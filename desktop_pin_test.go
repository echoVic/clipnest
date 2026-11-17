package main

import "testing"

func TestHideOnBlur(t *testing.T) {
	for _, c := range []struct{ busy, permission, pinned, want bool }{
		{false, false, false, true},
		{true, false, false, false},
		{false, true, false, false},
		{false, false, true, false},
	} {
		if got := hideOnBlur(c.busy, c.permission, c.pinned); got != c.want {
			t.Errorf("hideOnBlur(busy %v, permission %v, pinned %v) = %v", c.busy, c.permission, c.pinned, got)
		}
	}
}

func TestPanelToggle(t *testing.T) {
	for _, c := range []struct {
		visible, pinned, focused bool
		want                     toggleAction
	}{
		{false, false, false, toggleOpen},
		{false, true, false, toggleOpen},
		{true, false, true, toggleClose},
		{true, false, false, toggleClose},
		{true, true, true, toggleClose},
		{true, true, false, toggleFocus},
	} {
		if got := panelToggle(c.visible, c.pinned, c.focused); got != c.want {
			t.Errorf("panelToggle(visible %v, pinned %v, focused %v) = %v, want %v", c.visible, c.pinned, c.focused, got, c.want)
		}
	}
}

func TestTrackedTargetKeepsTheLastOtherApp(t *testing.T) {
	editor, browser := targetApp{PID: 10, Name: "Editor"}, targetApp{PID: 20, Name: "Browser"}
	if got := trackedTarget(editor, browser); got != browser {
		t.Errorf("switching apps kept %v", got)
	}
	// PID 0 means Clipnest itself, or nothing, is in front.
	if got := trackedTarget(editor, targetApp{}); got != editor {
		t.Errorf("Clipnest in front replaced the target with %v", got)
	}
	if got := trackedTarget(targetApp{}, targetApp{}); got != (targetApp{}) {
		t.Errorf("no target became %v", got)
	}
}
