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

func TestLocateFollowsSearchAndFilter(t *testing.T) {
	// Newest first: four, three (a link), two, one.
	v := sequenceView(t, "one", "two", "https://example.com/three", "four")
	page, err := v.Page("", "", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	one, three := page.Entries[3].ID, page.Entries[1].ID
	for _, c := range []struct {
		q, kind, id string
		want        int
	}{
		{"", "", one, 3},
		{"", "all", three, 1},
		{"", "text", one, 2},
		{"o", "text", one, 2},
		{"", "link", one, -1},
		{"", "", "missing", -1},
		{"", "", "", -1},
	} {
		if got, err := v.Locate(c.q, c.kind, c.id); err != nil || got != c.want {
			t.Errorf("Locate(%q, %q, %q) = %d, %v; want %d", c.q, c.kind, c.id, got, err, c.want)
		}
	}
}

func TestLocateStopsAtTheLimit(t *testing.T) {
	v := sequenceView(t, "one", "two", "three")
	page, _ := v.Page("", "", 0, 50)
	defer func(limit int) { locateLimit = limit }(locateLimit)
	locateLimit = 2
	if got, _ := v.Locate("", "", page.Entries[1].ID); got != 1 {
		t.Errorf("within the limit: %d", got)
	}
	if got, _ := v.Locate("", "", page.Entries[2].ID); got != -1 {
		t.Errorf("past the limit: %d", got)
	}
	if _, err := v.Locate("", "nonsense", page.Entries[0].ID); err == nil {
		t.Error("an invalid kind was accepted")
	}
}
