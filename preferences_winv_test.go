package main

import (
	"reflect"
	"testing"

	"github.com/egoist/mygo/ui"
)

func winVSettingsView() *preferencesView {
	v := settingsView()
	v.state.Platform, v.state.Shortcut = "windows", "Ctrl Alt V"
	v.tab = shortcutsTab
	return v
}

func TestPreferencesWinVSwitchIsOnlyOnWindows(t *testing.T) {
	v := settingsView()
	v.tab = shortcutsTab
	if tt := ui.NewTester(v.draw, 620, 580); tt.HasText("用 Win+V 打开 Clipnest") {
		t.Fatal("Win+V switch shown on macOS")
	}
	v = winVSettingsView()
	if tt := ui.NewTester(v.draw, 620, 580); !tt.HasText("用 Win+V 打开 Clipnest") {
		t.Fatal("Win+V switch missing on Windows", tt.Texts())
	}
}

func TestPreferencesWinVSwitchAsksBeforeRestartingExplorer(t *testing.T) {
	v := winVSettingsView()
	changes := []bool{}
	closed := 0
	v.actions.winV = func(on bool) { changes = append(changes, on) }
	v.actions.close = func() { closed++ }
	tt := ui.NewTester(v.draw, 620, 580)
	if err := tt.Click("用 Win+V 打开 Clipnest"); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 || !tt.HasText("重启资源管理器并开启") {
		t.Fatal("switch changed without asking", changes, tt.Texts())
	}
	// Escape cancels the dialog, not the window.
	tt.Key(0, ui.KeyEscape)
	if len(changes) != 0 || closed != 0 || v.confirmWinV {
		t.Fatal("Escape did not cancel", changes, closed)
	}
	tt.Click("用 Win+V 打开 Clipnest")
	if err := tt.Click("重启资源管理器并开启"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(changes, []bool{true}) {
		t.Fatal(changes)
	}
}

func TestPreferencesWinVSwitchTurnsOffWhatItTurnedOn(t *testing.T) {
	v := winVSettingsView()
	v.state.WinV = winVAdded
	changes := []bool{}
	v.actions.winV = func(on bool) { changes = append(changes, on) }
	tt := ui.NewTester(v.draw, 620, 580)
	tt.Click("用 Win+V 打开 Clipnest")
	if !tt.HasText("重启资源管理器并关闭") {
		t.Fatal("off dialog missing", tt.Texts())
	}
	tt.Click("取消")
	if len(changes) != 0 {
		t.Fatal("cancel changed the setting", changes)
	}
	tt.Click("用 Win+V 打开 Clipnest")
	tt.Click("重启资源管理器并关闭")
	if !reflect.DeepEqual(changes, []bool{false}) {
		t.Fatal(changes)
	}
	// Turning off a V the user had disabled restarts nothing.
	v.state.WinV = winVKept
	tt.Frame()
	tt.Click("用 Win+V 打开 Clipnest")
	if tt.HasText("重启资源管理器并关闭") || !tt.HasText("仍保持停用") {
		t.Fatal("kept V offered a restart", tt.Texts())
	}
}

func TestPreferencesWinVNoticeShowsWhyWinVDoesNotWork(t *testing.T) {
	v := winVSettingsView()
	v.state.WinV, v.state.WinVNotice = winVAdded, "资源管理器仍占用 Win+V"
	if tt := ui.NewTester(v.draw, 620, 580); !tt.HasText("资源管理器仍占用 Win+V") {
		t.Fatal("notice missing", tt.Texts())
	}
}
