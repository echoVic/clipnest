package main

import (
	"context"
	"errors"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
)

// hideOnBlur reports whether the panel hides when it loses the keyboard. A
// pinned panel stays, as does one busy pasting or guiding a permission.
func hideOnBlur(busy, permission, pinned bool) bool {
	return !busy && !permission && !pinned
}

type toggleAction int

const (
	toggleOpen toggleAction = iota
	toggleFocus
	toggleClose
)

// panelToggle decides what the summon shortcut and the tray button do. A
// pinned panel without the keyboard takes it back instead of hiding.
func panelToggle(visible, pinned, focused bool) toggleAction {
	switch {
	case !visible:
		return toggleOpen
	case pinned && !focused:
		return toggleFocus
	default:
		return toggleClose
	}
}

// trackedTarget is the app a pinned panel pastes into: the last app in front
// that is not Clipnest, which Target reports as PID 0.
func trackedTarget(current, front targetApp) targetApp {
	if front.PID == 0 {
		return current
	}
	return front
}

// historyChanged tells a showing panel, by session, that its snapshot is
// behind. The panel asks for Reload when nothing holds the old snapshot.
var historyChanged = mygo.NewEvent[uint64]("clipnest:history-changed")

func (d *Desktop) announceHistory() {
	if !d.quitting && d.win != nil && d.win.IsVisible() {
		_ = historyChanged.Broadcast(d.session)
	}
}

// PanelReload is a panel with a new snapshot and where the selected entry is
// in it, -1 when it is gone or none was given.
type PanelReload struct {
	State PanelState `json:"state"`
	Index int        `json:"index"`
}

// Reload swaps a showing panel's snapshot for a new one. The new snapshot is
// opened and read in the background while the old one keeps serving the
// panel; only the swap runs on main. It does not broadcast: the panel takes
// a broadcast of a new session as reopening and clears its search.
func (d *Desktop) Reload(session uint64, q, kind, id string) (r PanelReload, err error) {
	r.Index = -1
	ready := false
	mygo.RunOnMain(func() {
		ready = session == d.session && d.win != nil && d.win.IsVisible() && !d.busy && !d.quitting && d.view != nil
		if !ready {
			r.State = d.snapshot()
		}
	})
	if !ready {
		return r, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	v, loadErr := d.h.openView(ctx)
	var page HistoryPage
	if loadErr == nil {
		page, loadErr = v.Page("", "", 0, historyPageSize)
	}
	index := -1
	if loadErr == nil && id != "" {
		// A failed lookup only loses the selection; the panel falls back to
		// the old position.
		if found, err := v.Locate(q, kind, id); err == nil {
			index = found
		}
	}
	mygo.RunOnMain(func() {
		if loadErr != nil || session != d.session || !d.win.IsVisible() || d.busy || d.quitting {
			cancel()
			if v != nil {
				go v.Close()
			}
			if loadErr != nil {
				d.notice = "历史无法读取：" + loadErr.Error()
			}
			r.State = d.snapshot()
			return
		}
		d.closeView()
		d.session++
		d.view, d.cancelView = v, cancel
		d.entries, d.total, d.loading = page.Entries, page.Total, false
		if strings.HasPrefix(d.notice, "历史无法读取：") {
			d.notice = ""
		}
		r.State, r.Index = d.snapshot(), index
	})
	return r, nil
}

// SetPinned pins or unpins the showing panel. Hiding the panel unpins it.
func (d *Desktop) SetPinned(session uint64, pinned bool) (s PanelState, err error) {
	mygo.RunOnMain(func() {
		if session != d.session || d.win == nil || !d.win.IsVisible() || d.quitting {
			err = errors.New("面板已更新，请重新选择")
			return
		}
		d.pinned = pinned
		s = d.snapshot()
	})
	return
}

func (d *Desktop) SetEntryPinned(session uint64, q, kind, id string, pinned bool) (PanelReload, error) {
	var view *historyView
	var err error
	mygo.RunOnMain(func() { view, err = d.panelView(session) })
	if err == nil {
		err = view.Contains(id)
	}
	if err != nil {
		return PanelReload{}, err
	}
	if _, err := d.mutateHistory(session, func() error { return d.h.SetEntryPinned(id, pinned) }, false); err != nil {
		return PanelReload{}, err
	}
	return d.Reload(session, q, kind, id)
}

// trackTarget follows the app a pinned panel pastes into as the user
// switches apps, and shows its name in the panel when it changes.
func (d *Desktop) trackTarget() {
	if !d.pinned || d.win == nil || !d.win.IsVisible() {
		return
	}
	next := trackedTarget(d.target, d.n.Target())
	changed := next.Name != d.target.Name
	d.target = next
	if changed {
		_ = panelChanged.Broadcast(d.snapshot())
	}
}

// leavePanel gives the keyboard back to target: a pinned panel stays up,
// any other one hides.
func (d *Desktop) leavePanel(target targetApp) {
	if d.pinned {
		d.handOff()
	} else {
		d.hide()
	}
	d.n.Activate(target)
}

// handOff takes the keyboard from a pinned panel without hiding it. On macOS
// the panel takes the keys without activating Clipnest, so activating the
// app in front does not take them back; ordering the panel out does, and
// showing it again without focus keeps it on screen. Its fade is off
// meanwhile, or the panel would vanish for a quarter second. handingOff keeps
// that brief hide from ending the session. On Windows, Activate hands over
// the foreground and the panel stays above it.
func (d *Desktop) handOff() {
	if runtime.GOOS != "darwin" {
		return
	}
	defer d.n.suspendAnimation(d.win.NativeHandle())()
	d.handingOff = true
	d.win.Hide()
	d.win.ShowInactive()
	d.handingOff = false
}
