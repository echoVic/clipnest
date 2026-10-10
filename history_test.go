package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func entry(t *testing.T, text string) Entry {
	t.Helper()
	e, err := newEntry(text, nil, "测试应用")
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func TestHistoryDedupAndFrozenPanel(t *testing.T) {
	h, _ := openHistory(filepath.Join(t.TempDir(), "history.json"))
	a, b, c := entry(t, "alpha"), entry(t, "beta"), entry(t, "gamma")
	h.Add(a)
	h.Add(b)
	frozen, _ := h.Snapshot()
	d := Desktop{entries: frozen, session: 7}
	h.Add(c)
	h.Add(a)
	latest, _ := h.Snapshot()
	if len(latest) != 3 || latest[0].ID != a.ID {
		t.Fatal("duplicate must move to front")
	}
	if frozen[0].ID != b.ID || frozen[1].ID != a.ID {
		t.Fatal("open panel order changed")
	}
	if got, err := d.find(7, b.ID); err != nil || got.Text != "beta" {
		t.Fatal("snapshot content lost")
	}
	if _, err := d.find(6, b.ID); err == nil {
		t.Fatal("stale session accepted")
	}
	if _, err := d.find(7, c.ID); err == nil {
		t.Fatal("new content leaked into frozen panel")
	}
}
func TestPersistencePauseDeleteAndClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "history.json")
	h, _ := openHistory(path)
	a := entry(t, "keep exact whitespace\n  hello\t")
	h.Add(a)
	h.Pause(true)
	h.Add(entry(t, "should not be stored"))
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := openHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	got, paused := loaded.Snapshot()
	if !paused || len(got) != 1 || got[0].Text != a.Text {
		t.Fatal("persistence changed content or pause state")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("history is not private")
	}
	loaded.Remove(a.ID)
	if err := loaded.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, _ = openHistory(path)
	got, _ = loaded.Snapshot()
	if len(got) != 0 {
		t.Fatal("deleted content returned")
	}
	loaded.Pause(false)
	loaded.Add(a)
	loaded.Clear()
	loaded.Save()
	loaded, _ = openHistory(path)
	got, _ = loaded.Snapshot()
	if len(got) != 0 {
		t.Fatal("clear did not persist")
	}
}
func TestCapacityAndContentValidation(t *testing.T) {
	h, _ := openHistory(filepath.Join(t.TempDir(), "history.json"))
	for i := 0; i < maxItems+15; i++ {
		h.Add(entry(t, fmt.Sprintf("item %d", i)))
	}
	got, _ := h.Snapshot()
	if len(got) != maxItems || got[0].Text != "item 214" {
		t.Fatal("history limit/order failed")
	}
	if _, err := newEntry(string(bytes.Repeat([]byte("x"), maxText+1)), nil, ""); err == nil {
		t.Fatal("oversized text accepted")
	}
	if _, err := newEntry("", []byte("not a png"), ""); err == nil {
		t.Fatal("invalid image accepted")
	}
	var buf bytes.Buffer
	png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 64, 32)))
	e, err := newEntry("", buf.Bytes(), "image")
	if err != nil || e.Width != 64 || e.Height != 32 || e.Kind != "image" {
		t.Fatal("image failed")
	}
	if entry(t, "https://example.com/a").Kind != "link" {
		t.Fatal("link failed")
	}
	if entry(t, "hello\nhttps://example.com").Kind != "text" {
		t.Fatal("multiline misclassified")
	}
	if _, err := newEntry("  \n", nil, ""); err == nil {
		t.Fatal("empty accepted")
	}
}
func TestConcurrentSaveDoesNotRestoreDeletedEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	h, _ := openHistory(path)
	var wg sync.WaitGroup
	for n := 0; n < 3; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				e, _ := newEntry(fmt.Sprintf("%d-%d", n, i), nil, "")
				h.Add(e)
				if err := h.Save(); err != nil {
					t.Error(err)
				}
			}
		}(n)
	}
	wg.Wait()
	h.Clear()
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := openHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := got.Snapshot()
	if len(entries) != 0 {
		t.Fatal("out of order write resurrected entries")
	}
}
func TestCorruptArchiveIsNotOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	os.WriteFile(path, []byte("broken"), 0600)
	if _, err := openHistory(path); err == nil {
		t.Fatal("corrupt history accepted")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "broken" {
		t.Fatal("corrupt data overwritten")
	}
}

func TestEscapedTextRoundTripsWithinContentLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	h, _ := openHistory(path)
	for i := 0; i < 17; i++ {
		h.Add(entry(t, fmt.Sprintf("%d:%s", i, strings.Repeat("\x00", maxText-32))))
	}
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := openHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := loaded.Snapshot()
	if len(got) != 17 {
		t.Fatal("JSON escaping lost valid history")
	}
}
