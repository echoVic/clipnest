package main

import (
	"context"
	"errors"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/transfer"
	"runtime"
	"time"
)

var panelChanged = mygo.NewEvent[PanelState]("clipnest:panel")

type PanelState struct {
	Session         uint64          `json:"session"`
	Entries         []Entry         `json:"entries"`
	Total           int             `json:"total"`
	Loading         bool            `json:"loading"`
	Paused          bool            `json:"paused"`
	Accessible      bool            `json:"accessible"`
	ClipboardAccess ClipboardAccess `json:"clipboardAccess"`
	Target          string          `json:"target"`
	Platform        string          `json:"platform"`
	Notice          string          `json:"notice"`
	Shortcut        string          `json:"shortcut"`
	Storage         StorageStatus   `json:"storage"`
	Version         string          `json:"version"`
}
type PasteResult struct {
	Copied  bool   `json:"copied"`
	Pasted  bool   `json:"pasted"`
	Message string `json:"message"`
}

// Desktop's fields are main-thread-only; persistence has its own locks.
type Desktop struct {
	h                     *History
	p                     *Phrases
	storage               *Storage
	n                     *nativeBridge
	win                   *mygo.Window
	tray                  *mygo.Tray
	entries               []Entry
	total                 int
	loading               bool
	view                  *historyView
	cancelView            context.CancelFunc
	session               uint64
	target                targetApp
	lastRevision          int64
	observedRevision      int64
	observedSource        targetApp
	sourceObserved        bool
	lastID                string
	clipboardAccess       ClipboardAccess
	preferences           *preferencesWindow
	appPicker             *applicationPicker
	permission            *permissionWindow
	appearanceInitialized bool
	notice                string
	shortcut              string
	busy                  bool
	quitting              bool
	stop                  chan struct{}
}

func (d *Desktop) snapshot() PanelState {
	paused := d.h.IsPaused()
	summaries := make([]Entry, 0, len(d.entries))
	for _, e := range d.entries {
		summaries = append(summaries, e.summary())
	}
	return PanelState{
		Session: d.session, Entries: summaries, Total: d.total, Loading: d.loading, Paused: paused, Accessible: d.n.Accessible(),
		ClipboardAccess: d.clipboardAccess,
		Target:          d.target.Name, Platform: runtime.GOOS, Notice: d.notice, Shortcut: d.shortcut,
		Storage: d.StorageStatus(), Version: applicationVersion(),
	}
}
func (d *Desktop) State() (s PanelState) { mygo.RunOnMain(func() { s = d.snapshot() }); return }

func (d *Desktop) StorageStatus() StorageStatus { return d.storage.Status() }

// The disk work runs off the AppKit thread. Busy keeps capture and delivery
// from changing the clipboard while the stores switch writers.
func (d *Desktop) MigrateStorage() (StorageStatus, error) {
	started := false
	mygo.RunOnMain(func() {
		if !d.busy && !d.quitting {
			d.busy = true
			started = true
		}
	})
	if !started {
		return d.StorageStatus(), errors.New("正在处理其他操作，请稍后再试")
	}
	defer mygo.RunOnMain(func() {
		d.busy = false
		if d.win.IsVisible() {
			_ = panelChanged.Broadcast(d.reload())
		}
	})
	return d.storage.Migrate()
}

func (d *Desktop) Dismiss() {
	mygo.RunOnMain(func() {
		if !d.busy {
			d.hide()
			d.n.Activate(d.target)
		}
	})
}
func (d *Desktop) panelView(session uint64) (*historyView, error) {
	if session != d.session {
		return nil, errors.New("面板已更新，请重新选择")
	}
	if d.view == nil {
		return nil, errors.New("历史正在加载，请稍后再试")
	}
	return d.view, nil
}
func (d *Desktop) Page(session, revision uint64, q, kind string, offset, limit int) (HistoryPage, error) {
	var v *historyView
	var err error
	mygo.RunOnMain(func() { v, err = d.panelView(session) })
	if err != nil {
		return HistoryPage{}, err
	}
	return v.SearchPage(revision, q, kind, offset, limit)
}

func (d *Desktop) Search(session, revision uint64) error {
	var v *historyView
	var err error
	mygo.RunOnMain(func() { v, err = d.panelView(session) })
	if err != nil {
		return err
	}
	_, err = v.searchContext(revision)
	return err
}
func (d *Desktop) Detail(session uint64, id string) (Entry, error) {
	var v *historyView
	var err error
	mygo.RunOnMain(func() { v, err = d.panelView(session) })
	if err != nil {
		return Entry{}, err
	}
	return v.Detail(id)
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
	mygo.RunOnMain(func() {
		if d.busy {
			err = errors.New("正在处理其他操作，请稍后再试")
			return
		}
		d.h.Pause(paused)
		d.lastRevision = d.n.Revision()
		s = d.snapshot()
	})
	if err != nil {
		return
	}
	err = d.h.Save()
	return
}
func (d *Desktop) Delete(session uint64, id string) (s PanelState, err error) {
	if _, err := d.Detail(session, id); err != nil {
		return s, err
	}
	return d.changeHistory(session, func() error {
		d.h.Remove(id)
		return d.h.Save()
	})
}
func (d *Desktop) Clear() (s PanelState, err error) {
	return d.changeHistory(0, func() error { d.h.Clear(); return d.h.Save() })
}

func (d *Desktop) SaveRetention(policy RetentionPolicy) (PanelState, error) {
	return d.changeHistory(0, func() error { return d.h.SetRetention(policy) })
}

func (d *Desktop) changeHistory(session uint64, change func() error) (s PanelState, err error) {
	started := false
	mygo.RunOnMain(func() {
		if d.busy || d.quitting {
			err = errors.New("正在处理其他操作，请稍后")
			return
		}
		if session != 0 && session != d.session {
			err = errors.New("面板已更新，请重新选择")
			return
		}
		d.busy, started = true, true
	})
	if !started {
		return
	}
	err = change()
	mygo.RunOnMain(func() {
		d.busy = false
		if err == nil && !d.quitting && d.win.IsVisible() {
			s = d.reload()
		} else {
			s = d.snapshot()
		}
	})
	return
}

func (d *Desktop) Choose(session uint64, id string, copyOnly bool) (PasteResult, error) {
	e, err := d.Detail(session, id)
	if err != nil {
		return PasteResult{}, err
	}
	return d.deliver(e, copyOnly, session)
}

// deliver copies e and, unless copyOnly, pastes it into the app the panel was
// opened from. Recording the clipboard revision keeps capture from re-adding it.
func (d *Desktop) deliver(e Entry, copyOnly bool, session uint64) (result PasteResult, err error) {
	var target targetApp
	started := false
	mygo.RunOnMain(func() {
		if session != d.session || !d.win.IsVisible() || d.quitting {
			err = errors.New("面板已更新，请重新选择")
			return
		}
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
		d.hide()
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
func (d *Desktop) InsertPhrase(session uint64, id string, copyOnly bool) (PasteResult, error) {
	p, err := d.p.Get(id)
	if err != nil {
		return PasteResult{}, err
	}
	e, err := newEntry(p.Text, nil, "")
	if err != nil {
		return PasteResult{}, err
	}
	return d.deliver(e, copyOnly, session)
}
