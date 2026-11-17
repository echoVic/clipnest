package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
)

func trayIcon() []byte {
	im := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	c := color.NRGBA{104, 83, 220, 255}
	if runtime.GOOS == "darwin" {
		c = color.NRGBA{0, 0, 0, 255}
	}
	for y := 4; y < 28; y++ {
		for x := 5; x < 27; x++ {
			if (x < 8 || x >= 24 || y < 7 || y >= 25) || (y >= 12 && y <= 14 && x >= 11 && x < 21) || (y >= 18 && y <= 20 && x >= 11 && x < 18) {
				im.SetNRGBA(x, y, c)
			}
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, im)
	return b.Bytes()
}

func (d *Desktop) trayToolTip() string { return "Clipnest · " + d.shortcut + " 打开剪贴板历史" }

// trayMenu is built when it opens, so it reflects a running paste sequence.
func (d *Desktop) trayMenu() *mygo.Menu {
	items := []*mygo.MenuItem{}
	if n := d.sequenceRemaining(); n > 0 {
		items = append(items, &mygo.MenuItem{Label: fmt.Sprintf("停止顺序粘贴（剩 %d 条）", n), Click: func(*mygo.MenuItem, *mygo.Window) {
			if d.sequence != nil {
				d.sequence.stop("")
			}
		}}, mygo.Separator())
	}
	return mygo.NewMenu(append(items, []*mygo.MenuItem{
		{Label: "打开 Clipnest", Click: func(*mygo.MenuItem, *mygo.Window) { d.openPanel() }},
		{Label: "偏好设置…", Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { d.OpenPreferences() }},
		{Label: "暂停 / 恢复记录", Click: func(*mygo.MenuItem, *mygo.Window) {
			if d.busy {
				return
			}
			d.h.Pause(!d.h.IsPaused())
			d.lastRevision = d.n.Revision()
			if d.preferences != nil {
				d.preferences.refresh()
			}
			_ = panelChanged.Broadcast(d.snapshot())
		}},
		updater.MenuItem(),
		{Label: "问题反馈…", Click: func(*mygo.MenuItem, *mygo.Window) {
			_ = mygo.Shell.OpenExternal("https://github.com/echoVic/clipnest/issues/new")
		}},
		mygo.Separator(),
		{Label: "退出 Clipnest", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	}...))
}

func (d *Desktop) createTray() {
	var err error
	d.tray, err = mygo.NewTray(mygo.TrayOptions{Icon: trayIcon(), IconIsTemplate: runtime.GOOS == "darwin", ToolTip: d.trayToolTip()})
	if err != nil {
		d.notice = "菜单栏图标不可用，仍可使用全局快捷键"
		return
	}
	if runtime.GOOS == "linux" {
		d.tray.SetMenu(d.trayMenu())
		return
	}
	d.tray.OnClick(d.show)
	d.tray.OnRightClick(func() {
		d.hide()
		d.tray.SetMenu(d.trayMenu())
		defer d.tray.SetMenu(nil)
		d.tray.PopUpMenu()
	})
}
