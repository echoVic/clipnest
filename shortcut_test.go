package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestShortcutBindingFromEventRequiresModifierAndFormatsPerPlatform(t *testing.T) {
	if _, ok := shortcutBindingFromEvent("darwin", ui.Modifiers(0), ui.KeyK, ui.KeyUnknown); ok {
		t.Fatal("a bare key must not become a global shortcut")
	}
	binding, ok := shortcutBindingFromEvent("darwin", ui.Super|ui.Shift, ui.KeyK, ui.KeyUnknown)
	if !ok {
		t.Fatal("modified key should be accepted")
	}
	if binding.Accelerator != "Command+Shift+K" || binding.Label != "⌘ ⇧ K" {
		t.Fatalf("unexpected mac shortcut: %+v", binding)
	}
	binding, ok = shortcutBindingFromEvent("windows", ui.Ctrl|ui.Alt, ui.KeyF2, ui.KeyUnknown)
	if !ok || binding.Accelerator != "Ctrl+Alt+F2" || binding.Label != "Ctrl Alt F2" {
		t.Fatalf("unexpected windows shortcut: %+v", binding)
	}
}

func TestShortcutBindingUsesPhysicalKeyWhenLayoutHasNoNamedKey(t *testing.T) {
	binding, ok := shortcutBindingFromEvent("linux", ui.Ctrl, ui.KeyUnknown, ui.KeyCode(ui.CodeKeyS))
	if !ok || binding.Accelerator != "Ctrl+S" || binding.Label != "Ctrl S" {
		t.Fatalf("physical key was not normalized: %+v ok=%v", binding, ok)
	}
}

func TestShortcutConfigRoundTripAndMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shortcut.json")
	if got, err := loadShortcutConfig(path); err != nil || got != "" {
		t.Fatalf("missing config should use defaults: %q %v", got, err)
	}
	if err := saveShortcutConfig(path, "Ctrl+Shift+K"); err != nil {
		t.Fatal(err)
	}
	got, err := loadShortcutConfig(path)
	if err != nil || got != "Ctrl+Shift+K" {
		t.Fatalf("shortcut config did not round trip: %q %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows reports writable files as 0666; POSIX permission bits do not apply.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("shortcut config permissions = %v, want 0600", info.Mode().Perm())
	}
}

func TestShortcutConfigRejectsInvalidBindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shortcut.json")
	for _, accelerator := range []string{"", "K", "Ctrl", "Ctrl+Banana", "Ctrl+Alt+Shift+Command+F99"} {
		if err := saveShortcutConfig(path, accelerator); err == nil {
			t.Errorf("saved invalid shortcut %q", accelerator)
		}
	}
	if err := writePrivateJSON(path, shortcutConfig{Version: 1, Accelerator: "K"}); err != nil {
		t.Fatal(err)
	}
	if _, err := loadShortcutConfig(path); err == nil {
		t.Fatal("loaded shortcut without a modifier")
	}
}

func TestShortcutAliasesNormalizeToSameBinding(t *testing.T) {
	for _, accelerator := range []string{"Command+Shift+V", "CmdOrCtrl+Shift+V", "Shift+Super+v"} {
		binding, err := parseShortcutBinding("darwin", accelerator)
		if err != nil || binding.Accelerator != "Command+Shift+V" || binding.Label != "⌘ ⇧ V" {
			t.Fatalf("binding %q: %+v %v", accelerator, binding, err)
		}
	}
}

func TestShortcutReplacePreservesOldRegistrationOnFailure(t *testing.T) {
	for _, failure := range []string{"register", "save", "afterRename", "none"} {
		t.Run(failure, func(t *testing.T) {
			registered := map[string]bool{"Ctrl+Shift+V": true}
			ops := shortcutOps{
				register: func(acc string, _ func()) error {
					if failure == "register" {
						return errors.New("occupied")
					}
					registered[acc] = true
					return nil
				},
				unregister: func(acc string) { delete(registered, acc) },
				save: func(_, _ string) error {
					if failure == "save" {
						return errors.New("disk full")
					}
					if failure == "afterRename" {
						return &replacedWriteError{errors.New("directory sync failed")}
					}
					return nil
				},
			}
			committed, err := replaceShortcut("Ctrl+Shift+V", "Ctrl+Alt+K", "shortcut.json", func() {}, ops)
			wantCommit := failure == "none" || failure == "afterRename"
			if committed != wantCommit || (err == nil) != (failure == "none") {
				t.Fatalf("committed=%v err=%v", committed, err)
			}
			if registered["Ctrl+Shift+V"] == wantCommit || registered["Ctrl+Alt+K"] != wantCommit {
				t.Fatalf("wrong live registrations: %v", registered)
			}
		})
	}
}

func TestShortcutReplaceSameBindingDoesNotReregister(t *testing.T) {
	writes := 0
	ops := shortcutOps{
		register:   func(string, func()) error { t.Fatal("reregistered active shortcut"); return nil },
		unregister: func(string) { t.Fatal("removed active shortcut") },
		save:       func(_, _ string) error { writes++; return nil },
	}
	committed, err := replaceShortcut("Ctrl+Alt+K", "Ctrl+Alt+K", "shortcut.json", func() {}, ops)
	if !committed || err != nil || writes != 1 {
		t.Fatalf("committed=%v writes=%d err=%v", committed, writes, err)
	}
}

func TestShortcutBindingUsesPhysicalPositionForGlobalRegistration(t *testing.T) {
	// MyGo's desktop hotkeys use ANSI key positions; a French-layout A at
	// the Q position must register Q, the key the person actually pressed.
	binding, ok := shortcutBindingFromEvent("darwin", ui.Super|ui.Shift, ui.KeyA, ui.KeyQ)
	if !ok || binding.Accelerator != "Command+Shift+Q" {
		t.Fatalf("wrong physical accelerator: %+v", binding)
	}
}

// A summon shortcut is taken from every app, so the editing, typing and
// window keys people press all day are refused.
func TestCommonShortcutsAreRefused(t *testing.T) {
	for _, c := range []struct {
		goos             string
		refused, allowed []string
	}{
		{"darwin",
			[]string{"Command+V", "Command+Q", "Command+Tab", "Command+Space", "Command+,", "Command+Left", "Ctrl+Space", "Ctrl+A", "Ctrl+V", "Ctrl+F4", "Shift+K", "Shift+Enter", "Alt+Left", "Alt+Backspace", "Alt+Tab", "Alt+F4", "Command+Shift+Left", "Alt+Shift+Right", "Shift+Ctrl+Tab"},
			[]string{"CmdOrCtrl+Shift+V", "Alt+Shift+V", "Alt+Space", "Alt+V", "Command+F5", "Shift+F2", "Command+Alt+Left", "Ctrl+Alt+Shift+Left", "Command+Ctrl+Space"}},
		{"windows",
			[]string{"Ctrl+V", "Ctrl+C", "Ctrl+Space", "Ctrl+Right", "Ctrl+F4", "Alt+F4", "Alt+Tab", "Alt+Enter", "Shift+Delete", "Shift+Insert", "Ctrl+Shift+Left", "Ctrl+Shift+End"},
			[]string{"CmdOrCtrl+Alt+V", "CmdOrCtrl+Shift+V", winVAccelerator, "Alt+Space", "Alt+V", "Ctrl+F12", "Ctrl+Alt+Left", "Super+Shift+K"}},
		{"linux",
			[]string{"Ctrl+V", "Shift+A", "Alt+Tab"},
			[]string{"Ctrl+Shift+V", "Alt+Shift+V", "Super+K"}},
	} {
		for _, accelerator := range c.refused {
			_, err := parseShortcutBinding(c.goos, accelerator)
			if !errors.As(err, new(commonShortcutError)) {
				t.Errorf("%s: %s was not refused as common: %v", c.goos, accelerator, err)
			}
		}
		for _, accelerator := range c.allowed {
			if _, err := parseShortcutBinding(c.goos, accelerator); err != nil {
				t.Errorf("%s: %s: %v", c.goos, accelerator, err)
			}
		}
		for _, choice := range shortcutChoicesFor(c.goos) {
			if _, err := parseShortcutBinding(c.goos, choice.accelerator); err != nil {
				t.Errorf("%s: default %s: %v", c.goos, choice.accelerator, err)
			}
		}
	}
	_, err := parseShortcutBinding("windows", "Ctrl+V")
	if err == nil || err.Error() != "Ctrl V 是常用快捷键，设成唤起快捷键后它在所有应用里都会失效，请再加一个修饰键" {
		t.Fatal(err)
	}
}

// Older versions let a common combination be saved. It no longer
// registers, and the notice says why instead of calling the file unreadable.
func TestShortcutConfigReplacesASavedCommonCombination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shortcut.json")
	if err := saveShortcutConfig(path, "Ctrl+V"); err == nil {
		t.Fatal("saved Ctrl+V")
	}
	if err := writePrivateJSON(path, shortcutConfig{Version: 1, Accelerator: "Ctrl+V"}); err != nil {
		t.Fatal(err)
	}
	got, err := loadShortcutConfig(path)
	if got != "" || err == nil {
		t.Fatalf("loaded %q %v", got, err)
	}
	label := "Ctrl V"
	if runtime.GOOS == "darwin" {
		label = "⌃ V"
	}
	if notice := shortcutConfigNotice(err); notice != "已保存的快捷键 "+label+" 会挡住常用操作，已改用默认快捷键；可在「偏好设置 → 快捷键」中修改" {
		t.Fatal(notice)
	}
	if notice := shortcutConfigNotice(errors.New("快捷键配置无效")); notice != "快捷键配置无法读取，已尝试默认快捷键；可在「偏好设置 → 快捷键」中重新录入" {
		t.Fatal(notice)
	}
}
