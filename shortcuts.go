package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"
)

const shortcutConfigVersion = 1

type shortcutBinding struct {
	Accelerator string
	Label       string
}

type shortcutConfig struct {
	Version     int    `json:"version"`
	Accelerator string `json:"accelerator"`
}

func loadShortcutConfig(path string) (string, error) {
	var config shortcutConfig
	if err := decodePrivateJSON(path, &config); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	if config.Version != shortcutConfigVersion || strings.TrimSpace(config.Accelerator) == "" || len(config.Accelerator) > 128 {
		return "", fmt.Errorf("快捷键配置无效")
	}
	binding, err := parseShortcutBinding(runtime.GOOS, config.Accelerator)
	return binding.Accelerator, err
}

func saveShortcutConfig(path, accelerator string) error {
	if strings.TrimSpace(accelerator) == "" || len(accelerator) > 128 {
		return errors.New("快捷键配置无效")
	}
	binding, err := parseShortcutBinding(runtime.GOOS, accelerator)
	if err != nil {
		return err
	}
	return writePrivateJSON(path, shortcutConfig{Version: shortcutConfigVersion, Accelerator: binding.Accelerator})
}

func shortcutBindingFromEvent(goos string, mods ui.Modifiers, key ui.Key, physical any) (shortcutBinding, bool) {
	// Global shortcuts in MyGo use ANSI positions, unlike local UI keys
	// which follow the current keyboard layout.
	physicalKey := ui.KeyUnknown
	switch value := physical.(type) {
	case ui.Key:
		physicalKey = value
	case ui.KeyCode:
		if physicalName, found := shortcutPhysicalKeyName(value); found {
			for candidate, name := range shortcutKeyNames {
				if name == physicalName {
					physicalKey = candidate
					break
				}
			}
		}
	}
	name, ok := shortcutKeyName(physicalKey)
	if !ok {
		name, ok = shortcutKeyName(key)
	}
	if !ok || mods == 0 || mods & ^(ui.Super|ui.Ctrl|ui.Alt|ui.Shift) != 0 {
		return shortcutBinding{}, false
	}
	parts := make([]string, 0, 5)
	labels := make([]string, 0, 5)
	if mods&ui.Super != 0 {
		if goos == "darwin" {
			parts = append(parts, "Command")
			labels = append(labels, "⌘")
		} else {
			parts = append(parts, "Super")
			labels = append(labels, "Win")
		}
	}
	if mods&ui.Ctrl != 0 {
		parts = append(parts, "Ctrl")
		if goos == "darwin" {
			labels = append(labels, "⌃")
		} else {
			labels = append(labels, "Ctrl")
		}
	}
	if mods&ui.Alt != 0 {
		parts = append(parts, "Alt")
		if goos == "darwin" {
			labels = append(labels, "⌥")
		} else {
			labels = append(labels, "Alt")
		}
	}
	if mods&ui.Shift != 0 {
		parts = append(parts, "Shift")
		if goos == "darwin" {
			labels = append(labels, "⇧")
		} else {
			labels = append(labels, "Shift")
		}
	}
	parts = append(parts, name)
	labels = append(labels, name)
	return shortcutBinding{Accelerator: strings.Join(parts, "+"), Label: strings.Join(labels, " ")}, true
}

func shortcutKeyName(key ui.Key) (string, bool) {
	name, ok := shortcutKeyNames[key]
	return name, ok
}

var shortcutKeyNames = map[ui.Key]string{
	ui.Key0: "0", ui.Key1: "1", ui.Key2: "2", ui.Key3: "3", ui.Key4: "4", ui.Key5: "5", ui.Key6: "6", ui.Key7: "7", ui.Key8: "8", ui.Key9: "9",
	ui.KeyA: "A", ui.KeyB: "B", ui.KeyC: "C", ui.KeyD: "D", ui.KeyE: "E", ui.KeyF: "F", ui.KeyG: "G", ui.KeyH: "H", ui.KeyI: "I", ui.KeyJ: "J", ui.KeyK: "K", ui.KeyL: "L", ui.KeyM: "M", ui.KeyN: "N", ui.KeyO: "O", ui.KeyP: "P", ui.KeyQ: "Q", ui.KeyR: "R", ui.KeyS: "S", ui.KeyT: "T", ui.KeyU: "U", ui.KeyV: "V", ui.KeyW: "W", ui.KeyX: "X", ui.KeyY: "Y", ui.KeyZ: "Z",
	ui.KeyF1: "F1", ui.KeyF2: "F2", ui.KeyF3: "F3", ui.KeyF4: "F4", ui.KeyF5: "F5", ui.KeyF6: "F6", ui.KeyF7: "F7", ui.KeyF8: "F8", ui.KeyF9: "F9", ui.KeyF10: "F10", ui.KeyF11: "F11", ui.KeyF12: "F12",
	ui.KeyEnter: "Enter", ui.KeyTab: "Tab", ui.KeySpace: "Space", ui.KeyBackspace: "Backspace", ui.KeyDelete: "Delete", ui.KeyInsert: "Insert", ui.KeyEscape: "Escape", ui.KeyUp: "Up", ui.KeyDown: "Down", ui.KeyLeft: "Left", ui.KeyRight: "Right", ui.KeyHome: "Home", ui.KeyEnd: "End", ui.KeyPageUp: "PageUp", ui.KeyPageDown: "PageDown",
	ui.KeyMinus: "-", ui.KeyEqual: "=", ui.KeyComma: ",", ui.KeyPeriod: ".", ui.KeySlash: "/", ui.KeySemicolon: ";", ui.KeyQuote: "'", ui.KeyBracketLeft: "[", ui.KeyBracketRight: "]", ui.KeyBackslash: "\\", ui.KeyBackquote: "`",
}

func shortcutPhysicalKeyName(code ui.KeyCode) (string, bool) {
	physical := map[ui.KeyCode]string{
		ui.CodeKeyA: "A", ui.CodeKeyB: "B", ui.CodeKeyC: "C", ui.CodeKeyD: "D", ui.CodeKeyE: "E", ui.CodeKeyF: "F", ui.CodeKeyG: "G", ui.CodeKeyH: "H", ui.CodeKeyI: "I", ui.CodeKeyJ: "J", ui.CodeKeyK: "K", ui.CodeKeyL: "L", ui.CodeKeyM: "M", ui.CodeKeyN: "N", ui.CodeKeyO: "O", ui.CodeKeyP: "P", ui.CodeKeyQ: "Q", ui.CodeKeyR: "R", ui.CodeKeyS: "S", ui.CodeKeyT: "T", ui.CodeKeyU: "U", ui.CodeKeyV: "V", ui.CodeKeyW: "W", ui.CodeKeyX: "X", ui.CodeKeyY: "Y", ui.CodeKeyZ: "Z",
		ui.CodeDigit0: "0", ui.CodeDigit1: "1", ui.CodeDigit2: "2", ui.CodeDigit3: "3", ui.CodeDigit4: "4", ui.CodeDigit5: "5", ui.CodeDigit6: "6", ui.CodeDigit7: "7", ui.CodeDigit8: "8", ui.CodeDigit9: "9",
		ui.CodeF1: "F1", ui.CodeF2: "F2", ui.CodeF3: "F3", ui.CodeF4: "F4", ui.CodeF5: "F5", ui.CodeF6: "F6", ui.CodeF7: "F7", ui.CodeF8: "F8", ui.CodeF9: "F9", ui.CodeF10: "F10", ui.CodeF11: "F11", ui.CodeF12: "F12",
		ui.CodeEnter: "Enter", ui.CodeTab: "Tab", ui.CodeSpace: "Space", ui.CodeBackspace: "Backspace", ui.CodeDelete: "Delete", ui.CodeInsert: "Insert", ui.CodeEscape: "Escape", ui.CodeArrowUp: "Up", ui.CodeArrowDown: "Down", ui.CodeArrowLeft: "Left", ui.CodeArrowRight: "Right", ui.CodeHome: "Home", ui.CodeEnd: "End", ui.CodePageUp: "PageUp", ui.CodePageDown: "PageDown",
	}
	name, ok := physical[code]
	return name, ok
}

// Normalize the subset the recorder can produce, including the framework's
// default CmdOrCtrl alias, so reselecting an active key does not register twice.
func parseShortcutBinding(goos, accelerator string) (shortcutBinding, error) {
	if len(accelerator) > 128 {
		return shortcutBinding{}, errors.New("快捷键无效，请重新录入")
	}
	parts := strings.Split(accelerator, "+")
	var mods ui.Modifiers
	for _, part := range parts[:len(parts)-1] {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "command", "cmd", "super", "meta", "win":
			mods |= ui.Super
		case "control", "ctrl":
			mods |= ui.Ctrl
		case "alt", "option":
			mods |= ui.Alt
		case "shift":
			mods |= ui.Shift
		case "cmdorctrl", "commandorcontrol":
			if goos == "darwin" {
				mods |= ui.Super
			} else {
				mods |= ui.Ctrl
			}
		default:
			return shortcutBinding{}, errors.New("快捷键无效，请重新录入")
		}
	}
	name := strings.TrimSpace(parts[len(parts)-1])
	for key, candidate := range shortcutKeyNames {
		if strings.EqualFold(name, candidate) {
			if binding, ok := shortcutBindingFromEvent(goos, mods, key, ui.KeyUnknown); ok {
				return binding, nil
			}
		}
	}
	return shortcutBinding{}, errors.New("请使用修饰键加字母、数字、功能键或导航键")
}

type shortcutOps struct {
	register   func(string, func()) error
	unregister func(string)
	save       func(string, string) error
}

// Registration happens before persistence. A pre-rename failure can undo the
// new registration; after rename, memory must follow the file already on disk.
func replaceShortcut(previous, next, path string, show func(), ops shortcutOps) (bool, error) {
	if previous != next {
		if err := ops.register(next, show); err != nil {
			return false, fmt.Errorf("快捷键被占用或系统不支持，请换一个组合：%w", err)
		}
	}
	err := ops.save(path, next)
	if err != nil && !writeReplaced(err) {
		if previous != next {
			ops.unregister(next)
		}
		return false, fmt.Errorf("快捷键保存失败，已保留原设置：%w", err)
	}
	if previous != "" && previous != next {
		ops.unregister(previous)
	}
	if err != nil {
		return true, fmt.Errorf("快捷键已生效，但保存确认失败：%w", err)
	}
	return true, nil
}
