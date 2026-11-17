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
