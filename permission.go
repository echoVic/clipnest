package main

import (
	"errors"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

var errPermissionCanceled = errors.New("已取消授权")

type permissionWindow struct {
	win    *mygo.Window
	parent *mygo.Window
	reply  chan string
}

func (p *permissionWindow) finish(decision string, focus bool) {
	if p.reply == nil {
		return
	}
	reply := p.reply
	p.reply = nil
	p.win.Destroy()
	if focus && p.parent.IsVisible() && !p.parent.IsDestroyed() {
		p.parent.Focus()
	}
	reply <- decision
}

// A native child window takes focus without ending the panel's frozen session.
// Its only result is a choice; presenting it never reads or writes clipboard data.
func (d *Desktop) permissionDialog(parent *mygo.Window, purpose permissionPurpose, access ClipboardAccess) <-chan string {
	p := &permissionWindow{parent: parent, reply: make(chan string, 1)}
	v := &permissionView{purpose: purpose, access: access, choose: func(choice string) { p.finish(choice, true) }}
	p.win = mygo.NewWindow(mygo.WindowOptions{
		Title: "Clipnest · 权限", Width: 520, Height: 440,
		TitleBarStyle: mygo.TitleBarHidden, DisableResize: true,
		Parent: parent, Modal: true, Hidden: true, Content: ui.View(v.draw),
	})
	p.win.OnClose(func(e *mygo.CloseEvent) {
		if p.reply != nil {
			e.PreventDefault()
			p.finish("cancel", false)
		}
	})
	d.permission = p
	reply := p.reply
	p.win.Show()
	p.win.Focus()
	return reply
}

func (d *Desktop) permissionActive() bool {
	return d.permission != nil && d.permission.reply != nil
}

func (d *Desktop) cancelPermission() {
	if d.permissionActive() {
		d.permission.finish("cancel", false)
	}
}

func (d *Desktop) PromptPastePermission(session uint64) (string, error) {
	var reply <-chan string
	var err error
	mygo.RunOnMain(func() {
		if d.quitting || d.busy || d.permissionActive() || d.win == nil || !d.win.IsVisible() || session != d.session {
			err = errors.New("面板已更新，请重新选择")
			return
		}
		reply = d.permissionDialog(d.win, permissionPaste, d.clipboardAccess)
	})
	if err != nil {
		return "", err
	}
	return <-reply, nil
}

func (d *Desktop) ConfirmClipboardPermission() (PanelState, error) {
	var reply <-chan string
	var err error
	mygo.RunOnMain(func() {
		if d.quitting || d.busy || d.permissionActive() || d.preferences == nil || !d.preferences.win.IsVisible() {
			err = errors.New("请打开偏好设置并等待当前操作完成")
			return
		}
		access := d.n.ClipboardAccess()
		reply = d.permissionDialog(d.preferences.win, permissionRecord, access)
	})
	if err != nil {
		return PanelState{}, err
	}
	switch <-reply {
	case "request":
		return d.RequestClipboardPermission()
	case "settings":
		if err := d.OpenClipboardPermission(); err != nil {
			return PanelState{}, err
		}
	}
	return PanelState{}, errPermissionCanceled
}
