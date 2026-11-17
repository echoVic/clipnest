package main

import "testing"

func TestSequenceAvailability(t *testing.T) {
	s := newPasteSequence("Command+V", "⌘V", sequenceOps{})
	for _, c := range []struct {
		goos       string
		sequence   *pasteSequence
		accessible bool
		want       string
	}{
		{"darwin", s, true, ""},
		{"darwin", s, false, "允许辅助功能后才能顺序粘贴"},
		{"windows", s, false, ""}, // Windows needs no grant to send keys
		{"linux", nil, false, "当前系统不支持顺序粘贴"},
	} {
		err := sequenceUnavailable(c.goos, c.sequence, c.accessible)
		if got := ""; err != nil {
			got = err.Error()
			if got != c.want {
				t.Errorf("%s: %q, want %q", c.goos, got, c.want)
			}
		} else if c.want != "" {
			t.Errorf("%s: available, want %q", c.goos, c.want)
		}
	}
}

func TestSequenceOwnWindow(t *testing.T) {
	other := targetApp{PID: 42, Name: "TextEdit"}
	for _, c := range []struct {
		name         string
		panelVisible bool
		front        targetApp
		want         bool
	}{
		// The macOS panel takes keys without making Clipnest the front app.
		{"panel over another app", true, other, true},
		{"own app in front", false, targetApp{}, true},
		{"another app in front", false, other, false},
	} {
		if got := sequenceOwnWindow(c.panelVisible, c.front); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
