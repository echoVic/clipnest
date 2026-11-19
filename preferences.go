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
	ShortcutNotice  string           `json:"shortcutNotice"`
	Storage         StorageStatus    `json:"storage"`
	Platform        string           `json:"platform"`
	Version         string           `json:"version"`
	ExcludedApps    []ExcludedApp    `json:"excludedApps"`
	Privacy         PrivacySettings  `json:"privacy"`
	// OpenAtLogin is the system's state, which the user can also change in
	// System Settings or Task Manager.
	OpenAtLogin bool `json:"openAtLogin"`
	// ImageTextNote says which languages the system cannot recognize yet.
	ImageTextNote string `json:"imageTextNote,omitempty"`
	// WinV is whether Win+V opens Clipnest (Windows only), and WinVNotice
	// why it does not although it should.
	WinV       winVState `json:"winV"`
	WinVNotice string    `json:"winVNotice,omitempty"`
	// DataDir is the data directory in use. DataDirFixed means --data-dir
	// chose it, and DataDirNotice says what became of a move.
	DataDir        string `json:"dataDir"`
	DataDirDefault bool   `json:"dataDirDefault"`
	DataDirFixed   bool   `json:"dataDirFixed"`
	DataDirNotice  string `json:"dataDirNotice,omitempty"`
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

// Called by the native preferences worker: keep registration and Desktop
// fields on main, and the atomic settings write off the UI thread.
func (d *Desktop) setShortcut(accelerator string) error {
	if _, err := parseShortcutBinding(runtime.GOOS, accelerator); err != nil {
		return err
	}
	started := false
	mygo.RunOnMain(func() {
		if !d.busy && !d.quitting {
			d.busy, started = true, true
		}
	})
	if !started {
		return errors.New("正在处理其他操作，请稍后再试")
	}
	defer mygo.RunOnMain(func() { d.busy = false })
	return d.applyShortcut(accelerator)
}

// applyShortcut registers accelerator as the summon shortcut and saves it.
// The caller holds d.busy.
func (d *Desktop) applyShortcut(accelerator string) error {
	binding, err := parseShortcutBinding(runtime.GOOS, accelerator)
	if err != nil {
		return err
	}
	var previous string
	mygo.RunOnMain(func() { previous = d.shortcutAccelerator })
	committed, err := replaceShortcut(previous, binding.Accelerator, d.shortcutPath, d.shortcutPressed, shortcutOps{
		register: func(acc string, show func()) (err error) {
			mygo.RunOnMain(func() { err = mygo.GlobalShortcut.Register(acc, show) })
			return
		},
		unregister: func(acc string) { mygo.RunOnMain(func() { mygo.GlobalShortcut.Unregister(acc) }) },
		save:       saveShortcutConfig,
	})
	if !committed {
		return err
	}
	mygo.RunOnMain(func() {
		if d.notice == d.shortcutNotice {
			d.notice = ""
		}
		d.shortcut, d.shortcutAccelerator = binding.Label, binding.Accelerator
		d.shortcutNotice = ""
		if err != nil {
			d.shortcutNotice = err.Error()
		}
		// Win+V was the summon shortcut; the switch keeps it.
		if previous == winVAccelerator {
			d.registerWinV()
		}
		if d.win != nil && d.win.IsVisible() {
			_ = panelChanged.Broadcast(d.snapshot())
		}
	})
	return err
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
		ShortcutNotice: d.shortcutNotice,
		Platform:       runtime.GOOS, Version: applicationVersion(), ExcludedApps: d.ExcludedApps(),
		Privacy: d.Privacy(), OpenAtLogin: mygo.App.OpenAtLogin(), ImageTextNote: imageTextNote(),
		WinV: d.winVState(), WinVNotice: d.winVNotice,
		DataDir: d.data.dir, DataDirDefault: samePath(d.data.dir, d.data.defaultDir),
		DataDirFixed: d.data.fixed, DataDirNotice: d.data.notice,
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
		shortcut: func(accelerator string) {
			p.run(func() error { return d.setShortcut(accelerator) }, "快捷键已更新", false)
		},
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
		savePrivacy: func(s PrivacySettings) {
			p.run(func() error { _, err := d.SavePrivacy(s); return err }, "已保存，之后的复制按新设置处理", false)
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
		openAtLogin: func(on bool) {
			success := "已关闭登录时打开"
			if on {
				success = "下次登录时，Clipnest 会在后台打开"
			}
			p.run(func() error { return d.setOpenAtLogin(on) }, success, false)
		},
		winV: func(on bool) {
			success := "已关闭，Win+V 重新打开系统的剪贴板面板"
			switch {
			case on:
				success = "已开启，按 Win+V 打开 Clipnest"
			case v.state.WinV == winVKept:
				success = "已关闭"
			}
			p.run(func() error { return d.setWinV(on) }, success, false)
		},
		openDataDir:    d.openDataDir,
		pickDataDir:    p.pickDataDir,
		restoreDataDir: func() { go p.chooseDataDir(d.data.defaultDir) },
		moveData:       p.moveData,
		exportData:     p.exportData,
		importData:     p.importData,
		update:         d.CheckForUpdates,
		close: func() {
			if !v.busy {
				v.recordingShortcut = false
				p.win.Hide()
			}
		},
	}
	p.win = mygo.NewWindow(mygo.WindowOptions{
		Title: "Clipnest · 偏好设置", Width: 620, Height: 580,
		MinWidth: 520, MinHeight: 480, Hidden: true,
		TitleBarStyle: mygo.TitleBarHidden, Content: ui.View(v.draw),
	})
	// Menu accelerators run before HandleInput. The preferences are all
	// native controls, so let the recorder receive Cmd+Q/Cmd+W as keys.
	p.win.SetMenu(mygo.NewMenu(nil))
	p.win.OnFocus(p.refresh)
	p.win.OnBlur(func() {
		v.recordingShortcut = false
		p.win.Invalidate()
	})
	p.win.OnHide(func() { v.recordingShortcut = false })
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
	p.report("", func() (string, error) { return success, work() }, resetRetention)
}

// report runs work in a worker, showing progress meanwhile and then what
// work says it did. Work that returns "" and no error says nothing, as a
// canceled dialog.
func (p *preferencesWindow) report(progress string, work func() (string, error), resetRetention bool) {
	if p.view.busy || p.desktop.busy || p.desktop.quitting {
		return
	}
	p.view.busy, p.view.notice, p.view.failed = true, progress, false
	p.win.Invalidate()
	go func() {
		success, err := work()
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
