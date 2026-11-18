package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func dataDirView() *preferencesView {
	v := settingsView()
	v.tab = 2
	v.state.DataDir, v.state.DataDirDefault = "/Users/me/Library/Application Support/Clipnest", true
	return v
}

func TestPreferencesShowTheDataLocation(t *testing.T) {
	v := dataDirView()
	opened, picked := 0, 0
	v.actions.openDataDir = func() error { opened++; return nil }
	v.actions.pickDataDir = func() { picked++ }
	v.actions.restoreDataDir = func() { t.Fatal("restored the default") }
	tt := ui.NewTester(v.draw, 620, 580)
	if !tt.HasText("数据位置") || !tt.HasText(v.state.DataDir) {
		t.Fatal("location missing", tt.Texts())
	}
	if tt.HasText("恢复默认位置") {
		t.Fatal("offered the default while in it")
	}
	if err := tt.Click("打开文件夹"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("更改…"); err != nil {
		t.Fatal(err)
	}
	if opened != 1 || picked != 1 {
		t.Fatal(opened, picked)
	}
}

func TestPreferencesOfferTheDefaultLocationElsewhere(t *testing.T) {
	v := dataDirView()
	v.state.DataDir, v.state.DataDirDefault = "/Volumes/Data/Clipnest", false
	restored := 0
	v.actions.restoreDataDir = func() { restored++ }
	tt := ui.NewTester(v.draw, 620, 580)
	if err := tt.Click("恢复默认位置"); err != nil {
		t.Fatal(err)
	}
	if restored != 1 {
		t.Fatal(restored)
	}
}

func TestPreferencesDataLocationCantChange(t *testing.T) {
	for name, change := range map[string]func(*PreferencesState){
		"--data-dir": func(s *PreferencesState) { s.DataDirFixed, s.DataDirDefault = true, false },
		"JSON":       func(s *PreferencesState) { s.Storage.Backend, s.Storage.MigrationAvailable = "json", true },
	} {
		t.Run(name, func(t *testing.T) {
			v := dataDirView()
			change(&v.state)
			v.actions.pickDataDir = func() { t.Fatal("picked") }
			v.actions.restoreDataDir = func() { t.Fatal("restored") }
			tt := ui.NewTester(v.draw, 620, 580)
			tt.Click("更改…")
			tt.Click("恢复默认位置")
			want := "启动参数"
			if name == "JSON" {
				want = "请先迁移到 SQLite，再更改数据位置"
			}
			if !tt.HasText(want) && !strings.Contains(strings.Join(tt.Texts(), "\n"), want) {
				t.Fatal("no reason given", tt.Texts())
			}
		})
	}
}

func TestPreferencesAskBeforeMovingTheData(t *testing.T) {
	v := dataDirView()
	var moved []moveTarget
	v.actions.moveData = func(t moveTarget) { moved = append(moved, t) }
	closed := 0
	v.actions.close = func() { closed++ }
	target := moveTarget{dir: "/Volumes/Data/Clipnest"}
	v.askMove(target)
	tt := ui.NewTester(v.draw, 620, 580)
	if !tt.HasText("移动并重启") {
		t.Fatal("no confirmation", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	if len(moved) != 0 || closed != 0 || v.confirmMove {
		t.Fatal("Escape did not cancel", moved, closed)
	}
	v.askMove(target)
	tt.Frame()
	if err := tt.Click("移动并重启"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(moved, []moveTarget{target}) {
		t.Fatal(moved)
	}
}

func TestPreferencesShowWhatBecameOfAMove(t *testing.T) {
	v := dataDirView()
	v.state.DataDirNotice = "数据已移到「/Volumes/Data/Clipnest」"
	if tt := ui.NewTester(v.draw, 620, 580); !tt.HasText(v.state.DataDirNotice) {
		t.Fatal(tt.Texts())
	}
}

func TestMoveDialog(t *testing.T) {
	title, message, confirm := moveDialog(moveTarget{dir: "/Volumes/Data/Clipnest"})
	if !strings.Contains(title, "移到") || !strings.Contains(message, "/Volumes/Data/Clipnest") || !strings.Contains(message, "删除") || !strings.Contains(message, "不记录") || confirm != "移动并重启" {
		t.Fatal(title, message, confirm)
	}
	if strings.Contains(message, "同步") {
		t.Fatal("warned about a folder that isn't synced:", message)
	}
	title, message, confirm = moveDialog(moveTarget{dir: "/Volumes/Data/Clipnest", existing: true})
	if !strings.Contains(title, "改用") || !strings.Contains(message, "原样保留") || confirm != "改用这里的数据" {
		t.Fatal(title, message, confirm)
	}
	_, message, _ = moveDialog(moveTarget{dir: "/Users/me/Dropbox/Clipnest", synced: "Dropbox"})
	if !strings.Contains(message, "Dropbox") || !strings.Contains(message, "同步") {
		t.Fatal(message)
	}
}
