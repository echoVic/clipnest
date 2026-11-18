//go:build windows

package main

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// windowsOCRLanguages returns the languages Windows recognizes. Without any,
// as on a server without the language features, the test is skipped.
func windowsOCRLanguages(t *testing.T) []string {
	t.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	leave, err := enterMTA()
	if err != nil {
		t.Fatal(err)
	}
	defer leave()
	o, err := loadOCR()
	if h := (hresultError{}); errors.As(err, &h) && h.code == 0x80040154 { // REGDB_E_CLASSNOTREG
		t.Skip("this Windows has no Windows.Media.Ocr:", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	tags, err := o.installed()
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) == 0 {
		t.Skip("Windows has no text recognition languages")
	}
	t.Log("recognition languages:", tags)
	return tags
}

// windowsChineseOCR skips the test when Windows cannot recognize Chinese.
func windowsChineseOCR(t *testing.T) {
	t.Helper()
	if zh, _ := ocrLanguages(windowsOCRLanguages(t)); zh == "" {
		t.Skip("Windows has no Chinese text recognition")
	}
}

// Real Windows.Media.Ocr on screenshots rendered by testdata/image-text/make_fixtures.py.
func TestImageTextWindowsRecognizesChineseEnglishAndCode(t *testing.T) {
	windowsChineseOCR(t)
	text := recognizeFixture(t, "mixed.png")
	for _, want := range []string{"剪贴板记录已暂停", "新的复制不会保存", "Recognized text in screenshots", "tx.Commit()", "err != nil", `fmt.Errorf("save: %w", err)`} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
	if lines := strings.Split(text, "\n"); len(lines) != 4 || !strings.HasPrefix(lines[0], "剪贴板") {
		t.Errorf("lines out of order:\n%s", text)
	}
}

func TestImageTextWindowsJoinsLinesAcrossTiles(t *testing.T) {
	windowsOCRLanguages(t)
	text := recognizeFixture(t, "wide.png")
	for _, want := range []string{"image_text.go", "sqlite_history.go", "preferences_view.go", "clipboard history stays on this Mac and becomes searchable offline"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
	if n := strings.Count(text, "searchable"); n != 1 {
		t.Errorf("cross-tile line repeated %d times:\n%s", n, text)
	}
}

func TestImageTextWindowsKeepsLinesInOverlappingRowsOnce(t *testing.T) {
	windowsChineseOCR(t)
	text := recognizeFixture(t, "tall.png")
	want := []string{"第一段", "Both tiles above and below see this line", "第二段", "Another line seen by two tiles", "end of screenshot"}
	at := 0
	for _, w := range want {
		if strings.Count(text, w) != 1 {
			t.Fatalf("%q not found exactly once in\n%s", w, text)
		}
		i := strings.Index(text, w)
		if i < at {
			t.Fatalf("%q out of order in\n%s", w, text)
		}
		at = i
	}
}

// Preferences ask for the note on the main thread, which MyGo puts in a
// single-threaded apartment; the recognizers are read from another thread.
func TestImageTextWindowsNoteReadsLanguagesFromASingleThreadedApartment(t *testing.T) {
	tags := windowsOCRLanguages(t)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ole32 := syscall.NewLazyDLL("ole32.dll")
	const coinitApartmentThreaded = 2
	if r, _, _ := ole32.NewProc("CoInitializeEx").Call(0, coinitApartmentThreaded); failed(uint32(r)) {
		t.Fatalf("CoInitializeEx failed: 0x%08X", uint32(r))
	}
	defer ole32.NewProc("CoUninitialize").Call()
	if got, want := imageTextNote(), imageTextLanguageNote(tags); got != want {
		t.Fatalf("note %q, want %q", got, want)
	}
}

// The desktop worker recognizes stored screenshots with Windows and saves the
// text where search and the preview read it.
func TestImageTextWindowsDesktopWorkerRecognizesStoredScreenshots(t *testing.T) {
	windowsChineseOCR(t)
	png, err := os.ReadFile("testdata/image-text/mixed.png")
	if err != nil {
		t.Fatal(err)
	}
	e, err := newEntry("", png, "截图")
	if err != nil {
		t.Fatal(err)
	}
	s := imageTextStorage(t, e)
	d := &Desktop{h: s.History, stop: make(chan struct{})}
	done := make(chan struct{})
	go func() { defer close(done); d.runImageText() }()
	defer func() { close(d.stop); <-done }()
	for deadline := time.Now().Add(30 * time.Second); searchTotal(t, s.History, "tx.Commit()") == 0; time.Sleep(100 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("worker did not recognize the stored screenshot")
		}
	}
	if got := detail(t, s.History, e.ID); got.ImageTextState != imageTextReady || !strings.HasPrefix(got.Title, "剪贴板记录已暂停") {
		t.Fatalf("detail after recognition: %q %q", got.ImageTextState, got.Title)
	}
}
