package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnavailableMessage(t *testing.T) {
	m := unavailableMessage("/Volumes/Data/Clipnest", errors.New("找不到这个文件夹"))
	if want := []string{"重试", "重新选择…", "改回默认位置", "退出"}; strings.Join(m.Buttons, "|") != strings.Join(want, "|") {
		t.Fatal(m.Buttons)
	}
	if m.Buttons[unavailableRetry] != "重试" || m.Buttons[unavailableRepick] != "重新选择…" || m.Buttons[unavailableDefault] != "改回默认位置" || m.Buttons[unavailableQuit] != "退出" {
		t.Fatal("button indexes", m.Buttons)
	}
	if m.CancelButton != unavailableQuit {
		t.Fatal("Escape should quit, not change anything:", m.CancelButton)
	}
	if !strings.Contains(m.Detail, "/Volumes/Data/Clipnest") || !strings.Contains(m.Detail, "找不到这个文件夹") {
		t.Fatal(m.Detail)
	}
	// A broken record names no directory.
	if m := unavailableMessage("", errors.New("数据位置记录无法读取")); strings.Contains(m.Detail, "数据位置：") {
		t.Fatal(m.Detail)
	}
}

func TestRepickDataDir(t *testing.T) {
	root := t.TempDir()
	def := filepath.Join(root, "Default")
	never := func(string) bool { t.Fatal("asked to start over"); return false }

	s, err := openStorage(filepath.Join(root, "Found"))
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if recorded, err := repickDataDir(filepath.Join(root, "Found"), def, never); err != nil || !recorded {
		t.Fatal(recorded, err)
	}
	if loc, _ := readDataLocation(def); loc != (dataLocation{Dir: filepath.Join(root, "Found")}) {
		t.Fatal(loc)
	}

	blank := filepath.Join(root, "Blank")
	if err := os.Mkdir(blank, 0700); err != nil {
		t.Fatal(err)
	}
	asked := ""
	if recorded, err := repickDataDir(blank, def, func(dir string) bool { asked = dir; return false }); err != nil || recorded || asked != blank {
		t.Fatal(recorded, err, asked)
	}
	if found, _ := hasClipnestData(blank); found {
		t.Fatal("created data without asking")
	}
	if recorded, err := repickDataDir(blank, def, func(string) bool { return true }); err != nil || !recorded {
		t.Fatal(recorded, err)
	}
	if found, _ := hasClipnestData(blank); !found {
		t.Fatal("recorded a directory without data")
	}

	broken := filepath.Join(root, "Broken")
	writeFile(t, filepath.Join(broken, databaseName))
	if recorded, err := repickDataDir(broken, def, never); err == nil || recorded {
		t.Fatal("recorded data that won't open")
	}
	if loc, _ := readDataLocation(def); loc.Dir != blank {
		t.Fatal("record changed:", loc)
	}
}
