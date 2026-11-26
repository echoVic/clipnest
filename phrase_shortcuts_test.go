package main

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestPhraseShortcutAssignmentPreservesBindingOnRegistrationFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), phraseShortcutFile)
	registered := map[int]bool{}
	shortcuts := phraseShortcutConfig{Version: 1}
	apply := func(id string, slot int) error {
		return shortcuts.assign(path, id, slot, func(next int) error {
			if next == 2 {
				return errors.New("in use")
			}
			registered[next] = true
			return nil
		}, func(old int) { delete(registered, old) })
	}
	if err := apply("phrase-id", 1); err != nil {
		t.Fatal(err)
	}
	for _, attempt := range []struct {
		id   string
		slot int
	}{{"another-phrase", 1}, {"phrase-id", 2}} {
		if err := apply(attempt.id, attempt.slot); err == nil {
			t.Fatal("overwrote an occupied slot")
		}
	}
	saved, err := loadPhraseShortcuts(path)
	if err != nil || saved.Slots != [3]string{"phrase-id", "", ""} || !registered[1] {
		t.Fatalf("binding changed: %+v, %v", saved, err)
	}
	if err := apply("phrase-id", 0); err != nil || registered[1] || shortcuts.Slots[0] != "" {
		t.Fatalf("binding was not released: %+v, %v", shortcuts, err)
	}
}
