package main

import (
	"errors"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/transfer"
	"runtime"
	"slices"
	"strings"
	"time"
)

var panelChanged = mygo.NewEvent[PanelState]("clipnest:panel")

type PanelState struct {
	Session    uint64  `json:"session"`
	Entries    []Entry `json:"entries"`
	Paused     bool    `json:"paused"`
	Accessible bool    `json:"accessible"`
	Target     string  `json:"target"`
	Platform   string  `json:"platform"`
	Notice     string  `json:"notice"`
	Shortcut   string  `json:"shortcut"`
}
type PasteResult struct {
	Copied  bool   `json:"copied"`
	Pasted  bool   `json:"pasted"`
	Message string `json:"message"`
}

// Desktop's fields are main-thread-only; persistence has its own locks.
type Desktop struct {
	h            *History
	p            *Phrases
	n            *nativeBridge
	win          *mygo.Window
	tray         *mygo.Tray
	entries      []Entry
	session      uint64
	target       targetApp
	lastRevision int64
	lastID       string
	notice       string
	shortcut     string
	busy         bool
	quitting     bool
	stop         chan struct{}
}

func (d *Desktop) snapshot() PanelState {
	_, paused := d.h.Snapshot()
	summaries := make([]Entry, 0, len(d.entries))
	for _, e := range d.entries {
		summaries = append(summaries, e.summary())
	}
	return PanelState{d.session, summaries, paused, d.n.Accessible(), d.target.Name, runtime.GOOS, d.notice, d.shortcut}
}
func (d *Desktop) State() (s PanelState) { mygo.RunOnMain(func() { s = d.snapshot() }); return }
func (d *Desktop) show() {
	if d.busy {
		return
	}
	if d.win.IsVisible() {
		d.win.Hide()
		d.n.Activate(d.target)
		return
	}
	d.target = d.n.Target()
	d.capture()
	d.entries, _ = d.h.Snapshot()
	d.session++
	a := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint()).WorkArea
	// Fit the screen under the pointer, including scaled or smaller displays.
	d.win.SetSize(min(860, a.Width), min(570, a.Height))
	width, height := d.win.Size()
	d.win.SetPosition(a.X+max(0, (a.Width-width)/2), a.Y+max(0, (a.Height-height)/3))
	_ = panelChanged.Broadcast(d.snapshot())
	d.win.Show()
	d.win.Focus()
}
func (d *Desktop) Dismiss() {
	mygo.RunOnMain(func() {
		if !d.busy {
			d.win.Hide()
			d.n.Activate(d.target)
		}
	})
}
func (d *Desktop) Query(session uint64, q, kind string) (out []Entry, err error) {
	mygo.RunOnMain(func() {
		out = []Entry{}
		if session != d.session {
			err = errors.New("面板已更新，请重新选择")
			return
		}
		q = strings.ToLower(strings.TrimSpace(q))
		for _, e := range d.entries {
			if kind != "all" && kind != "" && e.Kind != kind {
				continue
			}
			if q != "" && !strings.Contains(strings.ToLower(e.Text+" "+e.Title+" "+e.Source), q) {
				continue
			}
			out = append(out, e.summary())
		}
	})
	return
}
func (d *Desktop) find(session uint64, id string) (Entry, error) {
	if session != d.session {
		return Entry{}, errors.New("面板已更新，请重新选择")
	}
	for _, e := range d.entries {
		if e.ID == id {
			return e, nil
		}
	}
	return Entry{}, errors.New("这条内容已移除")
}
func (d *Desktop) Detail(session uint64, id string) (e Entry, err error) {
	mygo.RunOnMain(func() { e, err = d.find(session, id) })
	return
}
func (d *Desktop) Permission() (ok bool) { mygo.RunOnMain(func() { ok = d.n.Accessible() }); return }
func (d *Desktop) OpenPermission() error {
	if runtime.GOOS != "darwin" {
		return errors.New("自动粘贴目前支持 macOS")
	}
	return mygo.Shell.OpenExternal("x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility")
}

// CheckForUpdates opens the update window, which says when Clipnest is up to
// date or offers the new version.
func (d *Desktop) CheckForUpdates() { updater.CheckForUpdates() }
func (d *Desktop) Pause(paused bool) (s PanelState, err error) {
	mygo.RunOnMain(func() { d.h.Pause(paused); d.lastRevision = d.n.Revision(); s = d.snapshot() })
	err = d.h.Save()
	return
}
func (d *Desktop) Delete(session uint64, id string) (s PanelState, err error) {
	mygo.RunOnMain(func() {
		if d.busy {
			err = errors.New("正在粘贴，请稍后")
			return
		}
		if _, err = d.find(session, id); err != nil {
			return
		}
		d.h.Remove(id)
		d.entries = slices.DeleteFunc(d.entries, func(e Entry) bool { return e.ID == id })
		s = d.snapshot()
	})
	if err == nil {
		err = d.h.Save()
	}
	return
}
func (d *Desktop) Clear() (s PanelState, err error) {
	mygo.RunOnMain(func() {
		if d.busy {
			err = errors.New("正在粘贴，请稍后")
			return
		}
		d.h.Clear()
		d.entries = []Entry{}
		d.session++
		s = d.snapshot()
	})
	if err == nil {
		err = d.h.Save()
	}
	return
}

func (d *Desktop) Choose(session uint64, id string, copyOnly bool) (PasteResult, error) {
	var e Entry
	var err error
	mygo.RunOnMain(func() { e, err = d.find(session, id) })
	if err != nil {
		return PasteResult{}, err
	}
	return d.deliver(e, copyOnly)
}

// deliver copies e and, unless copyOnly, pastes it into the app the panel was
// opened from. Recording the clipboard revision keeps capture from re-adding it.
func (d *Desktop) deliver(e Entry, copyOnly bool) (result PasteResult, err error) {
	var target targetApp
	started := false
	mygo.RunOnMain(func() {
		if d.busy {
			err = errors.New("正在粘贴，请稍后")
			return
		}
		if !copyOnly && runtime.GOOS == "darwin" && !d.n.Accessible() {
			err = errors.New("允许辅助功能后即可自动粘贴，也可以使用「仅复制」")
			return
		}
		if e.Kind == "image" {
			err = mygo.Clipboard.WriteImage(e.Image)
		} else {
			err = mygo.Clipboard.Write(transfer.TextData(e.Text))
		}
		if err != nil {
			return
		}
		d.lastRevision = d.n.Revision()
		d.lastID = e.ID
		result.Copied = true
		if copyOnly || runtime.GOOS != "darwin" || d.target.PID == 0 {
			result.Message = "已复制，返回目标应用即可粘贴"
			return
		}
		d.busy = true
		started = true
		target = d.target
		d.win.Hide()
		d.n.Activate(target)
	})
	if err != nil || !started {
		return
	}
	// Release keys from the opening chord / number selection before injecting.
	// A foreground check immediately before posting avoids pasting into another app.
	deadline := time.Now().Add(900 * time.Millisecond)
	for time.Now().Before(deadline) {
		time.Sleep(30 * time.Millisecond)
		ready := false
		mygo.RunOnMain(func() {
			if d.quitting {
				return
			}
			ready = d.n.ReadyToPaste(target)
			if ready {
				err = d.n.Paste(target)
				result.Pasted = err == nil
			}
		})
		if ready {
			break
		}
	}
	mygo.RunOnMain(func() {
		d.busy = false
		if !result.Pasted {
			d.notice = "内容已复制，未能自动粘贴。请返回原应用按 ⌘V。"
			if d.tray != nil {
				d.tray.SetToolTip("Clipnest · " + d.notice)
			}
		} else {
			d.notice = ""
		}
	})
	if result.Pasted {
		result.Message = "已粘贴"
	} else {
		result.Message = "内容已复制，请在原应用手动粘贴"
	}
	return
}

func (d *Desktop) Phrases(q string) ([]Phrase, error)         { return d.p.List(q) }
func (d *Desktop) PhraseDetail(id string) (Phrase, error)     { return d.p.Get(id) }
func (d *Desktop) SavePhrase(id, text string) (Phrase, error) { return d.p.Save(id, text) }
func (d *Desktop) DeletePhrase(id string) error               { return d.p.Delete(id) }
func (d *Desktop) MovePhrase(id string, delta int) error      { return d.p.Move(id, delta) }

// InsertPhrase pastes a phrase the way Choose pastes an entry. newEntry yields
// the ID capture would compute, so the write never lands in history.
func (d *Desktop) InsertPhrase(id string, copyOnly bool) (PasteResult, error) {
	p, err := d.p.Get(id)
	if err != nil {
		return PasteResult{}, err
	}
	e, err := newEntry(p.Text, nil, "")
	if err != nil {
		return PasteResult{}, err
	}
	return d.deliver(e, copyOnly)
}

func (d *Desktop) capture() {
	rev := d.n.Revision()
	if rev >= 0 && rev == d.lastRevision {
		return
	}
	_, paused := d.h.Snapshot()
	if paused {
		d.lastRevision = rev
		return
	}
	formats := mygo.Clipboard.AvailableFormats()
	for _, f := range formats {
		if f == "org.nspasteboard.ConcealedType" || f == "org.nspasteboard.TransientType" || f == "org.nspasteboard.AutoGeneratedType" {
			d.lastRevision = rev
			return
		}
	}
	var e Entry
	var err error
	portable := mygo.Clipboard.Formats()
	if slices.Contains(portable, transfer.FileList) {
		d.lastRevision = rev
		return
	}
	if slices.Contains(portable, transfer.Text) {
		data, readErr := mygo.Clipboard.ReadFormat(transfer.Text)
		if readErr != nil {
			return
		}
		e, err = newEntry(string(data), nil, d.n.Target().Name)
	} else if slices.Contains(portable, transfer.PNG) || slices.Contains(formats, "public.tiff") {
		e, err = newEntry("", mygo.Clipboard.ReadImage(), d.n.Target().Name)
	} else {
		d.lastRevision = rev
		return
	}
	if rev >= 0 && d.n.Revision() != rev {
		return
	}
	d.lastRevision = rev
	if err != nil {
		return
	}
	if rev < 0 && e.ID == d.lastID {
		return
	}
	d.lastID = e.ID
	d.h.Add(e)
}
func (d *Desktop) monitor() {
	t := time.NewTicker(300 * time.Millisecond)
	defer t.Stop()
	save := time.NewTicker(2 * time.Second)
	defer save.Stop()
	for {
		select {
		case <-d.stop:
			return
		case <-t.C:
			mygo.RunOnMain(func() {
				if !d.quitting && !d.busy {
					d.capture()
				}
			})
		case <-save.C:
			if err := d.h.Save(); err != nil {
				mygo.RunOnMain(func() { d.notice = "历史暂时无法保存，请检查磁盘空间与目录权限" })
			}
		}
	}
}
