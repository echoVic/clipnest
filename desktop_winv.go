package main

import (
	"errors"
	"log"

	"github.com/egoist/mygo"
)

// registerWinV registers Win+V at launch when the switch is on. Explorer
// started with V disabled, so it is free unless the V was removed since.
func (d *Desktop) registerWinV() {
	state, err := systemWinV().state()
	if err != nil {
		log.Print("Win+V 设置读取失败：", err)
		return
	}
	if state == winVOff {
		return
	}
	if err := d.registerWinVHotkey(); err != nil {
		log.Print("Win+V 注册失败：", err)
		d.winVNotice = "Win+V 仍被系统占用。关闭这个开关再打开，Clipnest 会重启资源管理器重新设置"
	}
}

// registerWinVHotkey adds Win+V beside the summon shortcut, unless the
// summon shortcut is Win+V already.
func (d *Desktop) registerWinVHotkey() error {
	if d.winVRegistered || d.shortcutAccelerator == winVAccelerator {
		return nil
	}
	if err := mygo.GlobalShortcut.Register(winVAccelerator, d.winVPressed); err != nil {
		return err
	}
	d.winVRegistered = true
	return nil
}

func (d *Desktop) unregisterWinVHotkey() {
	if d.winVRegistered {
		mygo.GlobalShortcut.Unregister(winVAccelerator)
		d.winVRegistered = false
	}
}

// winVPressed toggles the panel. While a summon shortcut is being recorded,
// Win+V is not offered as one: it already opens the panel.
func (d *Desktop) winVPressed() {
	if d.preferences != nil && d.preferences.view.recordingShortcut && d.preferences.win.IsVisible() {
		return
	}
	d.show()
}

// setWinV runs on the preferences worker: the registry and Explorer's
// restart off main, the hotkey on it.
func (d *Desktop) setWinV(on bool) error {
	started := false
	mygo.RunOnMain(func() {
		if !d.busy && !d.quitting {
			d.busy, started = true, true
		}
	})
	if !started {
		return errors.New("正在处理其他操作，请稍后再试")
	}
	s := systemWinV()
	s.register = func() (err error) {
		mygo.RunOnMain(func() { err = d.registerWinVHotkey() })
		return
	}
	s.unregister = func() { mygo.RunOnMain(d.unregisterWinVHotkey) }
	var err error
	if on {
		err = turnOnWinV(s)
	} else {
		err = turnOffWinV(s)
	}
	mygo.RunOnMain(func() {
		d.busy = false
		d.winVNotice = ""
	})
	return err
}

func (d *Desktop) winVState() winVState {
	state, err := systemWinV().state()
	if err != nil {
		return winVOff
	}
	return state
}
