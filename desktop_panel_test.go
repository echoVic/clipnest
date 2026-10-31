package main

import (
	"context"
	"strings"
	"testing"

	"github.com/egoist/mygo"
)

func TestHiddenPanelInvalidatesSessionOnlyOnce(t *testing.T) {
	s, err := openStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e := entry(t, "frozen")
	s.History.Add(e)
	ctx, cancel := context.WithCancel(context.Background())
	v, err := s.History.openView(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d := Desktop{h: s.History, view: v, cancelView: cancel, entries: []Entry{e.summary()}, session: 7, total: 1}
	d.hidden()
	if d.session != 8 || d.view != nil || d.cancelView != nil || len(d.entries) != 0 || d.total != 0 || d.loading {
		t.Fatal("hidden panel retained session data")
	}
	if _, err := v.Page("", "", 0, 50); err == nil {
		t.Fatal("hidden reader still accepted requests")
	}
	d.hidden()
	if d.session != 8 {
		t.Fatal("duplicate hide shifted session")
	}
}

func TestHiddenPanelCancelsPendingOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	d := Desktop{cancelView: cancel, loading: true, session: 9}
	d.hidden()
	if ctx.Err() == nil || d.loading || d.session != 10 {
		t.Fatal("pending open not canceled")
	}
}

func TestPanelPlacementFitsPointerDisplay(t *testing.T) {
	for _, area := range []mygo.Rectangle{
		{X: 0, Y: 0, Width: 1440, Height: 900},
		{X: -1280, Y: -200, Width: 1280, Height: 700},
		{X: 1920, Y: 0, Width: 600, Height: 400},
	} {
		got := panelBounds(area)
		if got.Width != min(panelWidth, area.Width) || got.Height != min(panelHeight, area.Height) ||
			got.X < area.X || got.Y < area.Y || got.X+got.Width > area.X+area.Width || got.Y+got.Height > area.Y+area.Height {
			t.Fatalf("panel outside display: %+v %+v", area, got)
		}
		if got.Y != area.Y+(area.Height-got.Height)/3 {
			t.Fatal("opening placement changed")
		}
	}
}

func TestShortcutChoicesAvoidIMEReservedChordsOnWindows(t *testing.T) {
	win := shortcutChoicesFor("windows")
	if len(win) < 2 || win[0].accelerator != "CmdOrCtrl+Alt+V" {
		t.Fatalf("windows primary shortcut must be Ctrl+Alt+V: %+v", win)
	}
	for _, c := range win {
		if strings.Contains(c.accelerator, "Alt+Shift") {
			t.Fatalf("Alt+Shift is claimed by the input-language switch on Windows: %q", c.accelerator)
		}
	}
	if c := shortcutChoicesFor("darwin"); c[0].accelerator != "CmdOrCtrl+Shift+V" {
		t.Fatalf("macOS shortcut changed: %+v", c)
	}
}
