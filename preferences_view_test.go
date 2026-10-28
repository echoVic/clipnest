package main

import (
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func settingsView() *preferencesView {
	v := &preferencesView{}
	v.setState(PreferencesState{
		Appearance: mygo.ThemeSystem, Platform: "darwin", ClipboardAccess: clipboardAsk,
		Shortcut: "⌘ ⇧ V", Version: "0.1.5",
		Storage: StorageStatus{Backend: "sqlite", Retention: RetentionPolicy{512, 0}},
	})
	return v
}

func TestPreferencesPagesSeparateRelatedSettings(t *testing.T) {
	v := settingsView()
	tt := ui.NewTester(v.draw, 620, 580)
	if !tt.HasText("外观") || !tt.HasText("检查更新…") || tt.HasText("保存并清理") {
		t.Fatal("general page mixes unrelated settings", tt.Texts())
	}
	if err := tt.Click("隐私"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("剪贴板记录") || !tt.HasText("自动粘贴") || !tt.HasText("开始记录") || tt.HasText("外观") {
		t.Fatal("privacy settings are not grouped", tt.Texts())
	}
	if err := tt.Click("存储"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("历史保留策略") || !tt.HasText("保存并清理") || tt.HasText("开始记录") {
		t.Fatal("storage settings are not grouped", tt.Texts())
	}
}

func TestPreferencesSwitchingTabsRetainsUnsavedRetention(t *testing.T) {
	v := settingsView()
	v.tab = 2
	tt := ui.NewTester(v.draw, 620, 580)
	if err := tt.Click("历史内容容量(MiB)"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("128")
	if v.maxMiB != 128 || !v.retentionDirty {
		t.Fatal("retention edit lost", v.maxMiB)
	}
	tt.Click("通用")
	v.setState(PreferencesState{Appearance: mygo.ThemeDark, Storage: StorageStatus{Backend: "sqlite", Retention: RetentionPolicy{512, 0}}})
	tt.Frame()
	tt.Click("存储")
	if v.maxMiB != 128 || !v.retentionDirty {
		t.Fatal("refresh or tab change overwrote the draft")
	}
}

func TestPreferencesClearRequiresConfirmationAndEnterCancels(t *testing.T) {
	v := settingsView()
	v.tab = 2
	cleared := 0
	v.actions.clear = func() { cleared++ }
	tt := ui.NewTester(v.draw, 620, 580)
	if err := tt.Click("清空…"); err != nil {
		t.Fatal(err)
	}
	if cleared != 0 || !tt.HasText("清空所有复制记录？") {
		t.Fatal("clear did not ask for confirmation")
	}
	tt.Key(0, ui.KeyEnter)
	if cleared != 0 || v.confirmClear {
		t.Fatal("Enter performed a destructive action")
	}
	tt.Click("清空…")
	tt.Click("确认清空")
	if cleared != 1 {
		t.Fatal("confirmed clear was not dispatched")
	}
}

func TestPreferencesDeniedPermissionHasNoReadAction(t *testing.T) {
	v := settingsView()
	v.tab = 1
	v.state.ClipboardAccess = clipboardDenied
	tt := ui.NewTester(v.draw, 620, 580)
	if tt.HasText("开始记录") || !tt.HasText("记录已被拒绝") {
		t.Fatal("denied permission exposed a read action", tt.Texts())
	}
}

func TestPreferencesKeyboardTabsAndClose(t *testing.T) {
	v := settingsView()
	closed := 0
	v.actions.close = func() { closed++ }
	tt := ui.NewTester(v.draw, 620, 580)
	if err := tt.Click("隐私"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyRight)
	if v.tab != 2 || !tt.HasText("历史保留策略") {
		t.Fatal("arrow key did not open storage")
	}
	tt.Key(0, ui.KeyEnd)
	if v.tab != 2 || !tt.HasText("历史保留策略") {
		t.Fatal("End did not open storage")
	}
	tt.Key(0, ui.KeyHome)
	if v.tab != 0 {
		t.Fatal("Home did not open general")
	}
	tt.Key(ui.Cmd, ui.KeyW)
	if closed != 1 {
		t.Fatal("Cmd+W did not close settings")
	}
}

func TestPreferencesRetainsVersionAndUpdateAction(t *testing.T) {
	v := settingsView()
	checked := 0
	v.actions.update = func() { checked++ }
	tt := ui.NewTester(v.draw, 620, 580)
	if !tt.HasText("Clipnest 0.1.5") {
		t.Fatal("settings omitted the application version")
	}
	if err := tt.Click("检查更新…"); err != nil {
		t.Fatal(err)
	}
	if checked != 1 {
		t.Fatal("settings lost the update action")
	}
}
