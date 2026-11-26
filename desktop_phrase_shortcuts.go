package main

import (
	"errors"
	"log"
	"path/filepath"
	"runtime"
	"slices"

	"github.com/egoist/mygo"
)

type PhraseShortcut struct {
	Slot     int    `json:"slot"`
	Label    string `json:"label"`
	PhraseID string `json:"phraseID"`
	Active   bool   `json:"active"`
}

type phrasePasteTarget struct {
	slot   int
	id     string
	target targetApp
}

func (d *Desktop) phraseShortcutPath() string {
	return filepath.Join(d.data.dir, phraseShortcutFile)
}

func (d *Desktop) startPhraseShortcuts() {
	if phraseAccelerator(runtime.GOOS, 1) == "" {
		return
	}
	config, err := loadPhraseShortcuts(d.phraseShortcutPath())
	if err != nil {
		log.Print("无法读取常用语快捷键：", err)
		return
	}
	d.phraseKeys = config
	for index, id := range config.Slots {
		if id == "" {
			continue
		}
		if _, err := d.p.Get(id); errors.Is(err, errPhraseGone) {
			d.phraseKeys.Slots[index] = ""
			continue
		} else if err != nil {
			continue
		}
		if err := d.registerPhraseShortcut(index + 1); err != nil {
			log.Print("无法注册常用语快捷键：", err)
		}
	}
}

func (d *Desktop) registerPhraseShortcut(slot int) error {
	accelerator := phraseAccelerator(runtime.GOOS, slot)
	if err := mygo.GlobalShortcut.Register(accelerator, func() { d.pasteBoundPhrase(slot) }); err != nil {
		return err
	}
	mygo.RunOnMain(func() { d.phraseKeysActive[slot-1] = true })
	return nil
}

func (d *Desktop) unregisterPhraseShortcut(slot int) {
	mygo.RunOnMain(func() {
		if d.phraseKeysActive[slot-1] {
			d.phraseKeysActive[slot-1] = false
			mygo.GlobalShortcut.Unregister(phraseAccelerator(runtime.GOOS, slot))
		}
	})
}

func (d *Desktop) PhraseShortcuts() (items []PhraseShortcut, err error) {
	items = []PhraseShortcut{}
	if phraseAccelerator(runtime.GOOS, 1) == "" {
		return
	}
	mygo.RunOnMain(func() {
		if d.phraseKeys.Version != 1 {
			err = errors.New("常用语快捷键配置无法读取")
			return
		}
		for index, id := range d.phraseKeys.Slots {
			binding, parseErr := parseShortcutBinding(runtime.GOOS, phraseAccelerator(runtime.GOOS, index+1))
			if parseErr != nil {
				err = parseErr
				return
			}
			items = append(items, PhraseShortcut{index + 1, binding.Label, id, d.phraseKeysActive[index]})
		}
	})
	return
}

func (d *Desktop) SetPhraseShortcut(id string, slot int) error {
	if phraseAccelerator(runtime.GOOS, 1) == "" {
		return errors.New("当前平台不支持常用语快捷键")
	}
	_, err := d.mutateHistory(0, func() error {
		if _, err := d.p.Get(id); err != nil {
			return err
		}
		var config phraseShortcutConfig
		var active [3]bool
		mygo.RunOnMain(func() { config, active = d.phraseKeys, d.phraseKeysActive })
		if config.Version == 1 && slot > 0 && slot <= len(config.Slots) && config.Slots[slot-1] == id && !active[slot-1] {
			return d.registerPhraseShortcut(slot)
		}
		err := config.assign(d.phraseShortcutPath(), id, slot, d.registerPhraseShortcut, d.unregisterPhraseShortcut)
		mygo.RunOnMain(func() { d.phraseKeys = config })
		return err
	}, false)
	return err
}

func (d *Desktop) deletePhraseAndShortcut(id string) error {
	_, err := d.mutateHistory(0, func() error {
		if err := d.p.Delete(id); err != nil {
			return err
		}
		var config phraseShortcutConfig
		mygo.RunOnMain(func() { config = d.phraseKeys })
		if index := slices.Index(config.Slots[:], id); index >= 0 {
			d.unregisterPhraseShortcut(index + 1)
			config.Slots[index] = ""
			mygo.RunOnMain(func() { d.phraseKeys = config })
			if err := writePrivateJSON(d.phraseShortcutPath(), config); err != nil {
				log.Print("无法保存常用语快捷键：", err)
			}
		}
		return nil
	}, false)
	return err
}

func (d *Desktop) pasteBoundPhrase(slot int) {
	id := d.phraseKeys.Slots[slot-1]
	if id == "" || d.busy || d.quitting || d.win.IsFocused() {
		return
	}
	target := d.n.Target()
	if target.PID == 0 {
		return
	}
	if !d.n.Accessible() {
		d.notice = "允许辅助功能后才能用全局快捷键粘贴常用语"
		if d.tray != nil {
			d.tray.SetToolTip("Clipnest · " + d.notice)
		}
		return
	}
	go func() {
		phrase, err := d.p.Get(id)
		if err == nil {
			var item Entry
			item, err = newEntry(phrase.Text, nil, "")
			if err == nil {
				_, err = d.deliverTo(item, false, 0, &phrasePasteTarget{slot, id, target})
			}
		}
		if err != nil {
			log.Print("常用语快捷键粘贴失败：", err)
		}
	}()
}
