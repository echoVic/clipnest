package main

import (
	"errors"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

var appearanceChanged = mygo.NewEvent[mygo.ThemeSource]("clipnest:appearance")

type PreferencesState struct {
	Appearance      mygo.ThemeSource `json:"appearance"`
	Paused          bool             `json:"paused"`
	Accessible      bool             `json:"accessible"`
	ClipboardAccess ClipboardAccess  `json:"clipboardAccess"`
	Shortcut        string           `json:"shortcut"`
	Storage         StorageStatus    `json:"storage"`
	Platform        string           `json:"platform"`
	Version         string           `json:"version"`
	ExcludedApps    []ExcludedApp    `json:"excludedApps"`
}

type preferencesWindow struct {
	desktop *Desktop
	win     *mygo.Window
	view    *preferencesView
}

func validAppearance(source mygo.ThemeSource) bool {
	return source == mygo.ThemeSystem || source == mygo.ThemeLight || source == mygo.ThemeDark
}

// The existing webview preference is restored once. A native choice made
// before the webview finishes loading must take precedence over that restore.
func (d *Desktop) RestoreAppearance(source mygo.ThemeSource) (current mygo.ThemeSource, err error) {
	if !validAppearance(source) {
		return "", errors.New("无效的外观设置")
	}
	mygo.RunOnMain(func() {
		if !d.appearanceInitialized {
			d.setAppearance(source)
		}
		current = mygo.Theme.Source()
	})
	return
}

func (d *Desktop) SetAppearance(source mygo.ThemeSource) error {
	if !validAppearance(source) {
		return errors.New("无效的外观设置")
	}
	mygo.RunOnMain(func() { d.setAppearance(source) })
	return nil
}

func (d *Desktop) setAppearance(source mygo.ThemeSource) {
	d.appearanceInitialized = true
	mygo.Theme.SetSource(source)
	_ = appearanceChanged.Broadcast(source)
	if d.preferences != nil {
		d.preferences.refresh()
	}
}

func (d *Desktop) preferencesState() PreferencesState {
	d.clipboardAccess = d.n.ClipboardAccess()
	return PreferencesState{
		Appearance: mygo.Theme.Source(), Paused: d.h.IsPaused(), Accessible: d.n.Accessible(),
		ClipboardAccess: d.clipboardAccess, Shortcut: d.shortcut, Storage: d.StorageStatus(),
		Platform: runtime.GOOS, Version: applicationVersion(), ExcludedApps: d.ExcludedApps(),
	}
}

func (d *Desktop) OpenPreferences() {
	mygo.RunOnMain(func() {
		if d.busy || d.quitting {
			return
		}
		d.cancelPermission()
		if d.win != nil && d.win.IsVisible() {
			d.hide()
		}
		if d.preferences == nil {
			d.preferences = newPreferencesWindow(d)
		}
		p := d.preferences
		p.refresh()
		p.win.Show()
		p.win.Focus()
	})
}

func newPreferencesWindow(d *Desktop) *preferencesWindow {
	p := &preferencesWindow{desktop: d, view: &preferencesView{}}
	v := p.view
	v.actions = preferencesActions{
		appearance: func(source mygo.ThemeSource) {
			if err := d.SetAppearance(source); err != nil {
				v.notice, v.failed = err.Error(), true
			}
		},
		pause: func(paused bool) {
			p.run(func() error { _, err := d.Pause(paused); return err }, "记录状态已更新", false)
		},
		request: func() {
			p.run(func() error { _, err := d.ConfirmClipboardPermission(); return err }, "读取已完成；持续记录需将系统权限设为「允许」", false)
		},
		clipboardSettings: d.OpenClipboardPermission,
		accessibility:     d.OpenPermission,
		addExcludedApp: func() {
			p.run(func() error { _, err := d.AddExcludedApp(); return err }, "", false)
		},
		removeExcludedApp: func(id string) {
			p.run(func() error { _, err := d.RemoveExcludedApp(id); return err }, "已移除，下一次复制重新判断", false)
		},
		imageText: func(on bool) {
			success := "已关闭，识别的文字已删除"
			if on {
				success = "已开启，图片会在后台逐张识别"
			}
			p.run(func() error { _, err := d.SetImageText(on); return err }, success, false)
		},
		migrate: func() {
			p.run(func() error { _, err := d.MigrateStorage(); return err }, "本地存储已更新", false)
		},
		saveRetention: func(policy RetentionPolicy) {
			p.run(func() error { _, err := d.SaveRetention(policy); return err }, "已保存并清理历史，常用语不受影响", true)
		},
		clear: func() {
			p.run(func() error { _, err := d.Clear(); return err }, "复制历史已清空，常用语已保留", false)
		},
		update: d.CheckForUpdates,
		close: func() {
			if !v.busy {
				p.win.Hide()
			}
		},
	}
	p.win = mygo.NewWindow(mygo.WindowOptions{
		Title: "Clipnest · 偏好设置", Width: 620, Height: 580,
		MinWidth: 520, MinHeight: 480, Hidden: true,
		TitleBarStyle: mygo.TitleBarHidden, Content: ui.View(v.draw),
	})
	p.win.OnFocus(p.refresh)
	p.win.OnClose(func(e *mygo.CloseEvent) {
		if !d.quitting {
			e.PreventDefault()
			if !v.busy {
				p.win.Hide()
			}
		}
	})
	p.refresh()
	return p
}

// All view state changes are on main. Only persistence work runs in a worker.
func (p *preferencesWindow) run(work func() error, success string, resetRetention bool) {
	if p.view.busy || p.desktop.busy || p.desktop.quitting {
		return
	}
	p.view.busy, p.view.notice = true, ""
	p.win.Invalidate()
	go func() {
		err := work()
		p.win.Update(func() {
			p.view.busy = false
			p.view.failed = err != nil && !errors.Is(err, errPermissionCanceled)
			if errors.Is(err, errPermissionCanceled) {
				p.view.notice = ""
			} else if err != nil {
				p.view.notice = err.Error()
			} else {
				p.view.notice = success
				if resetRetention {
					p.view.retentionDirty = false
				}
			}
			p.refresh()
		})
	}()
}

func (p *preferencesWindow) refresh() {
	p.view.setState(p.desktop.preferencesState())
	if p.win != nil {
		p.win.Invalidate()
	}
}
