package main

import (
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
)

// sequenceUnavailable says why a paste sequence cannot start. It needs the
// same keystroke injection as auto-paste.
func sequenceUnavailable(goos string, sequence *pasteSequence, accessible bool) error {
	if sequence == nil {
		return errors.New("当前系统不支持顺序粘贴")
	}
	if goos == "darwin" && !accessible {
		return errors.New("允许辅助功能后才能顺序粘贴")
	}
	return nil
}

// sequenceOwnWindow reports whether a press of the paste keys belongs to one
// of Clipnest's own windows. The panel hides when it loses focus, so while it
// shows it has the keys, even on macOS where it takes them without making
// Clipnest the front app.
func sequenceOwnWindow(panelVisible bool, front targetApp) bool {
	return panelVisible || front.PID == 0
}

func writeClipboard(e Entry) error {
	if e.Kind == "image" {
		return mygo.Clipboard.WriteImage(e.Image)
	}
	return mygo.Clipboard.Write(transfer.TextData(e.Text))
}

func (d *Desktop) newSequence() *pasteSequence {
	key := pasteAccelerator()
	if key == "" {
		return nil
	}
	return newPasteSequence(key, manualPasteHint(), sequenceOps{
		register:   mygo.GlobalShortcut.Register,
		unregister: mygo.GlobalShortcut.Unregister,
		write: func(e Entry) error {
			if err := writeClipboard(e); err != nil {
				return err
			}
			d.lastRevision, d.lastID = d.n.Revision(), e.ID
			return nil
		},
		self: func() bool {
			return sequenceOwnWindow(d.win != nil && d.win.IsVisible(), d.n.Target())
		},
		send: d.n.PasteHeld,
		after: func(delay time.Duration, fn func()) func() {
			timer := time.AfterFunc(delay, func() { mygo.RunOnMain(fn) })
			return func() { timer.Stop() }
		},
		changed: d.sequenceChanged,
	})
}

func (d *Desktop) sequenceRemaining() int {
	if d.sequence == nil {
		return 0
	}
	return d.sequence.remaining()
}

// StartSequence queues the selected entry and every entry above it in the
// list the panel shows, then hides the panel. Each ⌘V after that pastes the
// next one, starting with the selected entry, the oldest of them.
func (d *Desktop) StartSequence(session uint64, q, kind string, index int, id string) error {
	var v *historyView
	var err error
	mygo.RunOnMain(func() {
		if err = sequenceUnavailable(runtime.GOOS, d.sequence, d.n.Accessible()); err == nil {
			v, err = d.panelView(session)
		}
	})
	if err != nil {
		return err
	}
	items, err := sequenceItems(v, q, kind, index, id)
	if err != nil {
		return err
	}
	mygo.RunOnMain(func() {
		if session != d.session || !d.win.IsVisible() || d.quitting {
			err = errors.New("面板已更新，请重新选择")
			return
		}
		if d.busy {
			err = errors.New("正在处理其他操作，请稍后")
			return
		}
		if err = sequenceUnavailable(runtime.GOOS, d.sequence, d.n.Accessible()); err != nil {
			return
		}
		if err = d.sequence.start(items); err != nil {
			return
		}
		d.hide()
		d.n.Activate(d.target)
	})
	return err
}

func (d *Desktop) StopSequence() (s PanelState) {
	mygo.RunOnMain(func() {
		if d.sequence != nil {
			d.sequence.stop("")
		}
		s = d.snapshot()
	})
	return
}

// sequenceChanged shows the remaining count beside the menu bar icon and in
// the tray tip, and a reason when the sequence stopped early.
func (d *Desktop) sequenceChanged(remaining int, reason string) {
	if reason != "" {
		d.notice = reason
	}
	if d.tray != nil {
		title, tip := "", d.trayToolTip()
		if remaining > 0 {
			title, tip = fmt.Sprint(remaining), fmt.Sprintf("Clipnest · 顺序粘贴 · 剩 %d 条", remaining)
		} else if reason != "" {
			tip = "Clipnest · " + reason
		}
		d.tray.SetTitle(title)
		d.tray.SetToolTip(tip)
	}
	if d.win != nil && d.win.IsVisible() {
		_ = panelChanged.Broadcast(d.snapshot())
	}
}
