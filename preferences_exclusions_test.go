package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestPreferencesExclusionsManageAppsAndPreserveDraft(t *testing.T) {
	v := settingsView()
	v.tab, v.maxMiB, v.retentionDirty = 1, 128, true
	v.state.ExcludedApps = []ExcludedApp{{"com.private", "Private App"}}
	added, removed := 0, ""
	v.actions.addExcludedApp = func() { added++ }
	v.actions.removeExcludedApp = func(id string) { removed = id }
	tt := ui.NewTester(v.draw, 520, 1200)
	if !tt.HasText("排除应用") || !tt.HasText("Private App") || !tt.HasText("com.private") {
		t.Fatal("exclusion list missing", tt.Texts())
	}
	if err := tt.Click("添加应用…"); err != nil || added != 1 {
		t.Fatal("add action missing", err)
	}
	if err := tt.Click("移除 Private App"); err != nil || removed != "com.private" {
		t.Fatal("remove action lost app identity", err)
	}
	v.setState(v.state)
	if !v.retentionDirty || v.maxMiB != 128 {
		t.Fatal("exclusion refresh lost retention draft")
	}
	v.busy = true
	tt.Frame()
	tt.Click("添加应用…")
	tt.Click("移除 Private App")
	if added != 1 {
		t.Fatal("busy view dispatched an edit")
	}
}

func TestPreferencesExclusionsUnavailableOnOtherPlatforms(t *testing.T) {
	v := settingsView()
	v.tab, v.state.Platform = 1, "linux"
	tt := ui.NewTester(v.draw, 520, 1200)
	if !tt.HasText("排除应用") || tt.HasText("添加应用…") {
		t.Fatal("unsupported system exposes application picker", tt.Texts())
	}
}
