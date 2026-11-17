package main

import (
	"strings"
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
	if !tt.HasText("外观") || !tt.HasText("检查更新…") || tt.HasText("保存并清理") || tt.HasText("修改唤起快捷键") {
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
	if err := tt.Click("快捷键"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("修改唤起快捷键") || !tt.HasText("存为常用语") || tt.HasText("外观") {
		t.Fatal("shortcuts are not on their own page", tt.Texts())
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
	if v.tab != shortcutsTab || !tt.HasText("唤起或收起面板") {
		t.Fatal("End did not open shortcuts")
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

func TestPreferencesShortcutRecordingAndCancel(t *testing.T) {
	v := settingsView()
	v.tab = shortcutsTab
	var saved string
	closed := 0
	v.actions.shortcut = func(acc string) { saved = acc }
	v.actions.close = func() { closed++ }
	tt := ui.NewTester(v.draw, 620, 580)
	if err := tt.Click("修改唤起快捷键"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("按下快捷键…") {
		t.Fatal("recording prompt missing", tt.Texts())
	}
	tt.Key(0, ui.KeyK)
	if saved != "" || !v.recordingShortcut {
		t.Fatal("bare key was saved")
	}
	// This view fixture models macOS even when the test runs on Windows.
	tt.Key(ui.Super|ui.Shift, ui.KeyK)
	if saved != "Command+Shift+K" || v.recordingShortcut {
		t.Fatal("shortcut not submitted", saved)
	}
	v.state.Shortcut = "⌘ ⇧ K"
	tt.Frame()
	if !tt.HasText("⌘ ⇧ K") {
		t.Fatal("updated shortcut missing")
	}
	tt.Click("修改唤起快捷键")
	tt.Key(0, ui.KeyEscape)
	if v.recordingShortcut || closed != 0 {
		t.Fatal("Escape closed settings instead of canceling")
	}
	tt.Key(0, ui.KeyEscape)
	if closed != 1 {
		t.Fatal("Escape no longer closes idle settings")
	}
}

func TestPreferencesShortcutCanRecordWindowCloseChordAndReset(t *testing.T) {
	v := settingsView()
	v.tab = shortcutsTab
	var saved string
	closed := 0
	v.actions.shortcut = func(acc string) { saved = acc }
	v.actions.close = func() { closed++ }
	tt := ui.NewTester(v.draw, 620, 580)
	tt.Click("修改唤起快捷键")
	tt.Key(ui.Super, ui.KeyW)
	if saved != "Command+W" || closed != 0 {
		t.Fatalf("saved=%q closed=%d", saved, closed)
	}
	tt.Click("恢复默认")
	if saved != shortcutChoicesFor("darwin")[0].accelerator {
		t.Fatal("reset did not select platform default", saved)
	}
}

func TestPreferencesShortcutRecordingEndsWhenFocusLeaves(t *testing.T) {
	v := settingsView()
	v.tab = shortcutsTab
	v.actions.shortcut = func(string) { t.Fatal("saved after recording lost focus") }
	tt := ui.NewTester(v.draw, 620, 580)
	tt.Click("修改唤起快捷键")
	tt.Click("隐私")
	tt.Key(ui.Cmd|ui.Shift, ui.KeyK)
	tt.Click("快捷键")
	if v.recordingShortcut || tt.HasText("按下快捷键…") {
		t.Fatal("recording survived tab change")
	}
}

func TestPreferencesShortcutRecordingExistingHotkey(t *testing.T) {
	v := settingsView()
	v.tab = shortcutsTab
	var saved string
	v.actions.shortcut = func(acc string) { saved = acc }
	tt := ui.NewTester(v.draw, 620, 580)
	tt.Click("修改唤起快捷键")
	v.submitShortcut("Command+Shift+V")
	tt.Frame()
	if saved != "Command+Shift+V" || v.recordingShortcut {
		t.Fatal("existing global shortcut did not finish recording", saved)
	}
}

func TestPreferencesShortcutsPageListsEveryPanelKey(t *testing.T) {
	groups := panelShortcuts()
	if len(groups) < 4 {
		t.Fatal("shortcut list did not load", groups)
	}
	for _, platform := range []string{"darwin", "windows", "linux"} {
		v := settingsView()
		v.state.Platform, v.tab = platform, shortcutsTab
		tt := ui.NewTester(v.draw, 620, 580)
		for _, group := range groups {
			if _, ok := tt.Find(group.Group); !ok {
				t.Fatalf("%s: group %s missing: %q", platform, group.Group, tt.Texts())
			}
			for _, item := range group.Items {
				if _, ok := tt.Find(item.Label); !ok {
					t.Fatalf("%s: %s missing", platform, item.Label)
				}
				if item.Note != "" && !tt.HasText(item.Note) {
					t.Fatalf("%s: note of %s missing", platform, item.Label)
				}
				for _, key := range item.keysFor(platform) {
					if _, ok := tt.Find(key); !ok {
						t.Fatalf("%s: key %s of %s missing", platform, key, item.Label)
					}
				}
			}
		}
	}
}

func TestPreferencesShortcutsFollowThePlatform(t *testing.T) {
	v := settingsView()
	v.tab = shortcutsTab
	tt := ui.NewTester(v.draw, 620, 580)
	for _, key := range []string{"⌘ ⇧ V", "⌘ S", "⌥ ↑", "⌘ ⌫", "1–9"} {
		if _, ok := tt.Find(key); !ok {
			t.Fatalf("macOS key %s missing", key)
		}
	}
	if _, ok := tt.Find("Delete"); ok {
		t.Fatal("macOS lists Delete, which is ⌘⌫ on Mac keyboards")
	}
	v.state.Platform, v.state.Shortcut = "windows", "Ctrl Alt V"
	tt.Frame()
	for _, key := range []string{"Ctrl Alt V", "Ctrl S", "Alt ↑", "Ctrl ↩", "Delete"} {
		if _, ok := tt.Find(key); !ok {
			t.Fatalf("Windows key %s missing: %q", key, tt.Texts())
		}
	}
	for _, text := range tt.Texts() {
		if strings.ContainsAny(text, "⌘⌥⇧") {
			t.Fatalf("Windows shows a macOS glyph: %q", text)
		}
	}
}
