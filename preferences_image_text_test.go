package main

import (
	"reflect"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestPreferencesImageTextSwitchesRecognition(t *testing.T) {
	v := settingsView()
	v.tab = 1
	v.state.Storage.ImageText = true
	changes := []bool{}
	v.actions.imageText = func(on bool) { changes = append(changes, on) }
	tt := ui.NewTester(v.draw, 520, 1400)
	if !tt.HasText("图片文字") || !tt.HasText("不联网") {
		t.Fatal("image text setting missing", tt.Texts())
	}
	if err := tt.Click("识别图片文字"); err != nil || !reflect.DeepEqual(changes, []bool{false}) {
		t.Fatal("switch did not turn recognition off", changes, err)
	}
	v.state.Storage.ImageText = false
	tt.Frame()
	tt.Click("识别图片文字")
	if !reflect.DeepEqual(changes, []bool{false, true}) {
		t.Fatal("switch did not turn recognition on", changes)
	}
	v.busy = true
	tt.Frame()
	tt.Click("识别图片文字")
	if len(changes) != 2 {
		t.Fatal("busy view dispatched a change")
	}
}

func TestPreferencesImageTextNeedsSQLiteOnMacOSAndWindows(t *testing.T) {
	for _, c := range []struct {
		platform, backend, note string
	}{
		{"darwin", "json", "迁移到 SQLite 后可识别图片文字"},
		{"windows", "json", "迁移到 SQLite 后可识别图片文字"},
		{"linux", "sqlite", "图片文字识别仅适用于 macOS 和 Windows"},
	} {
		v := settingsView()
		v.tab, v.state.Platform, v.state.Storage.Backend = 1, c.platform, c.backend
		v.actions.imageText = func(bool) { t.Fatal("unavailable setting dispatched a change") }
		tt := ui.NewTester(v.draw, 520, 1400)
		if !tt.HasText(c.note) {
			t.Fatal("missing note", c.note, tt.Texts())
		}
		if _, found := tt.Find("识别图片文字"); found {
			t.Fatal("unavailable setting shows a switch", c.platform, c.backend)
		}
	}
}

// Windows recognizes only the languages installed for it, and preferences
// say what is missing below the switch.
func TestPreferencesImageTextShowsTheWindowsLanguageNote(t *testing.T) {
	v := settingsView()
	v.tab, v.state.Platform, v.state.Storage.ImageText = 1, "windows", true
	v.state.ImageTextNote = "未安装中文文字识别，截图中的中文可能识别不出。"
	changes := []bool{}
	v.actions.imageText = func(on bool) { changes = append(changes, on) }
	tt := ui.NewTester(v.draw, 520, 1400)
	if !tt.HasText("未安装中文文字识别") {
		t.Fatal("language note missing", tt.Texts())
	}
	if err := tt.Click("识别图片文字"); err != nil || !reflect.DeepEqual(changes, []bool{false}) {
		t.Fatal("Windows switch did not turn recognition off", changes, err)
	}
	v.state.ImageTextNote = ""
	tt.Frame()
	if tt.HasText("未安装中文文字识别") {
		t.Fatal("stale language note")
	}
}
