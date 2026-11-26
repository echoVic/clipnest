package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
)

const phraseShortcutFile = "phrase-shortcuts.json"

type phraseShortcutConfig struct {
	Version int       `json:"version"`
	Slots   [3]string `json:"slots"`
}

func loadPhraseShortcuts(path string) (phraseShortcutConfig, error) {
	config := phraseShortcutConfig{Version: 1}
	if err := decodePrivateJSON(path, &config); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return config, nil
		}
		return phraseShortcutConfig{}, err
	}
	if config.Version != 1 {
		return phraseShortcutConfig{}, errors.New("不支持此常用语快捷键配置")
	}
	for index, id := range config.Slots {
		if id != "" && slices.Contains(config.Slots[:index], id) {
			return phraseShortcutConfig{}, errors.New("常用语绑定了多个快捷键")
		}
	}
	return config, nil
}

func (config *phraseShortcutConfig) assign(path, id string, slot int, register func(int) error, unregister func(int)) error {
	if config.Version != 1 || id == "" || slot < 0 || slot > len(config.Slots) {
		return errors.New("无效的常用语快捷键")
	}
	previous := slices.Index(config.Slots[:], id) + 1
	if slot == previous {
		return nil
	}
	next := *config
	if previous != 0 {
		next.Slots[previous-1] = ""
	}
	if slot != 0 {
		if next.Slots[slot-1] != "" {
			return errors.New("该快捷键已绑定另一条常用语")
		}
		if err := register(slot); err != nil {
			return fmt.Errorf("该快捷键无法注册，请试试其他组合：%w", err)
		}
		next.Slots[slot-1] = id
	}
	err := writePrivateJSON(path, next)
	if err != nil && !writeReplaced(err) {
		if slot != 0 {
			unregister(slot)
		}
		return err
	}
	if previous != 0 {
		unregister(previous)
	}
	*config = next
	return err
}

func phraseAccelerator(platform string, slot int) string {
	prefix := ""
	switch platform {
	case "darwin":
		prefix = "Command+Ctrl+"
	case "windows":
		prefix = "Ctrl+Alt+Shift+"
	default:
		return ""
	}
	return fmt.Sprintf("%s%d", prefix, slot)
}
