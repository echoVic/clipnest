package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestPreferencesOpenAtLoginSwitchShowsSystemState(t *testing.T) {
	v := settingsView()
	v.state.OpenAtLogin = true
	changes := []bool{}
	v.actions.openAtLogin = func(on bool) { changes = append(changes, on) }
	tt := ui.NewTester(v.draw, 620, 580)
	if !tt.HasText("登录时打开 Clipnest") {
		t.Fatal("open at login setting missing from the general page", tt.Texts())
	}
	if err := tt.Click("登录时打开 Clipnest"); err != nil || !reflect.DeepEqual(changes, []bool{false}) {
		t.Fatal("switch did not turn opening at login off", changes, err)
	}
	// The switch follows the system, not the click: it stays on until the
	// refreshed state says otherwise.
	tt.Frame()
	tt.Click("登录时打开 Clipnest")
	if !reflect.DeepEqual(changes, []bool{false, false}) {
		t.Fatal("switch did not follow the system state", changes)
	}
	v.state.OpenAtLogin = false
	tt.Frame()
	tt.Click("登录时打开 Clipnest")
	if !reflect.DeepEqual(changes, []bool{false, false, true}) {
		t.Fatal("switch did not turn opening at login on", changes)
	}
	v.busy = true
	tt.Frame()
	tt.Click("登录时打开 Clipnest")
	if len(changes) != 3 {
		t.Fatal("busy view dispatched a change")
	}
}

func TestOpenAtLoginErrorTellsWhereToLook(t *testing.T) {
	for goos, want := range map[string]string{
		"darwin":  "登录项",
		"windows": "启动",
		"linux":   "登录时打开",
	} {
		if err := loginItemError(goos); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatal(goos, err)
		}
	}
}
