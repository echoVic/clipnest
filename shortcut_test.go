package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestShortcutBindingFromEventRequiresModifierAndFormatsPerPlatform(t *testing.T) {
	if _, ok := shortcutBindingFromEvent("darwin", ui.Modifiers(0), ui.KeyK, ui.KeyUnknown); ok {
		t.Fatal("a bare key must not become a global shortcut")
	}
	binding, ok := shortcutBindingFromEvent("darwin", ui.Cmd|ui.Shift, ui.KeyK, ui.KeyUnknown)
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
	if err := saveShortcutConfig(path, "Command+Shift+K"); err != nil {
		t.Fatal(err)
	}
	got, err := loadShortcutConfig(path)
	if err != nil || got != "Command+Shift+K" {
		t.Fatalf("shortcut config did not round trip: %q %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
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
	binding, ok := shortcutBindingFromEvent("darwin", ui.Cmd|ui.Shift, ui.KeyA, ui.KeyQ)
	if !ok || binding.Accelerator != "Command+Shift+Q" {
		t.Fatalf("wrong physical accelerator: %+v", binding)
	}
}
