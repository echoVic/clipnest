package main

import (
	"encoding/json"
	"strings"
	"sync"
)

// The panel's keys are listed once, in web/shortcuts.json: the preferences
// window reads it from the embedded frontend, the browser demo fetches it, and
// web/shortcuts.test.mjs checks it against the key rules in web/keys.js.
type shortcutGroup struct {
	Group string         `json:"group"`
	Items []shortcutItem `json:"items"`
}

type shortcutItem struct {
	Label string   `json:"label"`
	Note  string   `json:"note"`
	Keys  []string `json:"keys"`
	Other []string `json:"other"`
}

var panelShortcuts = sync.OnceValue(func() []shortcutGroup {
	data, err := frontend.ReadFile("web/shortcuts.json")
	if err != nil {
		panic(err)
	}
	var groups []shortcutGroup
	if err := json.Unmarshal(data, &groups); err != nil {
		panic(err)
	}
	return groups
})

var ctrlGlyphs = strings.NewReplacer("⌘", " Ctrl ", "⌥", " Alt ", "⇧", " Shift ")

// keysFor matches shortcutKeys in web/shortcuts.js: macOS keeps the glyphs,
// other systems spell the modifiers out, and Other replaces the keys there.
func (item shortcutItem) keysFor(goos string) []string {
	if goos == "darwin" {
		return item.Keys
	}
	if item.Other != nil {
		return item.Other
	}
	keys := make([]string, len(item.Keys))
	for i, key := range item.Keys {
		keys[i] = strings.Join(strings.Fields(ctrlGlyphs.Replace(key)), " ")
	}
	return keys
}
