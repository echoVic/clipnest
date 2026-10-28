package main

import (
	"context"
	"log"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
)

const (
	panelWidth  = 860
	panelHeight = 570
	panelRadius = 20
)

func (d *Desktop) ready() {
	var err error
	d.n, err = openNative()
	if err != nil {
		log.Fatal("无法初始化系统剪贴板：", err)
	}
	d.win = mygo.NewWindow(mygo.WindowOptions{
		Title: "Clipnest · 剪贴板历史", URL: "/",
		Width: panelWidth, Height: panelHeight,
		Panel: true, Hidden: true, Frameless: true, DisableResize: true,
		Transparent: true, BackgroundColor: "#00000000", CornerRadius: panelRadius,
	})
	d.win.SetMenu(mygo.NewMenu(nil))
	d.win.OnHide(d.hidden)
	d.win.OnBlur(func() {
		if !d.busy {
			d.hide()
		}
	})
	d.win.OnClose(func(e *mygo.CloseEvent) {
		if !d.quitting {
			e.PreventDefault()
			d.hide()
		}
	})
	d.registerShortcuts()
	d.createTray()
	d.win.OnReadyToShow(d.openPanel)
	go d.monitor()
}

// Cancel pending opens and reads before releasing a hidden panel's snapshot.
func (d *Desktop) closeView() {
	if d.cancelView != nil {
		d.cancelView()
		d.cancelView = nil
	}
	if d.view != nil {
		v := d.view
		d.view = nil
		go v.Close()
	}
}

// Both app-initiated and framework-initiated hiding end the session. Native
// callbacks can reenter Hide; invalidation must happen once.
func (d *Desktop) hidden() {
	if d.cancelView == nil && d.view == nil && !d.loading {
		return
	}
	d.closeView()
	d.session++
	d.entries, d.total, d.loading = []Entry{}, 0, false
}

func (d *Desktop) hide() {
	d.hidden()
	d.win.Hide()
}

// Native window work stays on main; flushing and opening the read transaction
// run in a worker. The generation prevents a slow old open replacing a newer
// panel, target app or selection.
func (d *Desktop) reload() PanelState {
	d.closeView()
	d.session++
	session := d.session
	d.entries, d.total, d.loading = []Entry{}, 0, true
	ctx, cancel := context.WithCancel(context.Background())
	d.cancelView = cancel
	go func() {
		v, err := d.h.openView(ctx)
		var page HistoryPage
		if err == nil {
			page, err = v.Page("", "", 0, historyPageSize)
		}
		mygo.RunOnMain(func() {
			if d.quitting || session != d.session || !d.win.IsVisible() {
				cancel()
				if v != nil {
					go v.Close()
				}
				return
			}
			d.loading = false
			if err != nil {
				cancel()
				if v != nil {
					go v.Close()
				}
				d.notice = "历史无法读取：" + err.Error()
			} else {
				d.view, d.entries, d.total = v, page.Entries, page.Total
				if strings.HasPrefix(d.notice, "历史无法读取：") {
					d.notice = ""
				}
			}
			_ = panelChanged.Broadcast(d.snapshot())
		})
	}()
	return d.snapshot()
}

func panelBounds(area mygo.Rectangle) mygo.Rectangle {
	width, height := min(panelWidth, area.Width), min(panelHeight, area.Height)
	return mygo.Rectangle{
		X:     area.X + max(0, (area.Width-width)/2),
		Y:     area.Y + max(0, (area.Height-height)/3),
		Width: width, Height: height,
	}
}

// Reopening the app or a second instance presents the existing panel. Only
// the shortcut and tray button toggle it, preserving the selected target.
func (d *Desktop) openPanel() {
	if d.busy || d.quitting || d.win == nil || d.win.IsDestroyed() {
		return
	}
	if d.win.IsVisible() {
		d.win.Focus()
		return
	}
	d.target = d.n.Target()
	d.capture()
	area := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint()).WorkArea
	bounds := panelBounds(area)
	// Keep the frame rebuild before Show: moving a hidden WKWebView between
	// displays previously left stale compositing tiles.
	d.win.SetSize(bounds.Width, bounds.Height)
	d.win.SetPosition(bounds.X, bounds.Y)
	d.win.Show()
	d.win.Focus()
	_ = panelChanged.Broadcast(d.reload())
}

func (d *Desktop) show() {
	if d.busy || d.quitting || d.win == nil {
		return
	}
	if d.win.IsVisible() {
		d.hide()
		d.n.Activate(d.target)
		return
	}
	d.openPanel()
}

func (d *Desktop) registerShortcuts() {
	if err := mygo.GlobalShortcut.Register("CmdOrCtrl+Shift+V", d.show); err != nil {
		d.shortcut = "⌥ ⇧ V"
		if err := mygo.GlobalShortcut.Register("Alt+Shift+V", d.show); err != nil {
			d.shortcut = "菜单栏打开"
			d.notice = "快捷键已被占用，请从菜单栏打开"
		} else {
			d.notice = "⌘⇧V 已被占用，已改用 ⌥⇧V"
		}
	}
	if runtime.GOOS != "darwin" && d.shortcut == "⌘ ⇧ V" {
		d.shortcut = "Ctrl Shift V"
	}
	if runtime.GOOS != "darwin" && d.shortcut == "⌥ ⇧ V" {
		d.shortcut = "Alt Shift V"
	}
}
