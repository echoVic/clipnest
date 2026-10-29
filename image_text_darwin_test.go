//go:build darwin

package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func recognizeFixture(t *testing.T, name string) string {
	t.Helper()
	png, err := os.ReadFile("testdata/image-text/" + name)
	if err != nil {
		t.Fatal(err)
	}
	text, err := recognizeImageText(png)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

// Real Vision on screenshots rendered by testdata/image-text/make_fixtures.py.
func TestImageTextVisionRecognizesChineseEnglishAndCode(t *testing.T) {
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

func TestImageTextVisionJoinsLinesAcrossTiles(t *testing.T) {
	text := recognizeFixture(t, "wide.png")
	for _, want := range []string{"image_text.go", "sqlite_history.go", "preferences_view.go", "clipboard history stays on this Mac and becomes searchable offline", "本地识别"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
	if n := strings.Count(text, "searchable"); n != 1 {
		t.Errorf("cross-tile line repeated %d times:\n%s", n, text)
	}
}

func TestImageTextVisionKeepsLinesInOverlappingRowsOnce(t *testing.T) {
	text := recognizeFixture(t, "tall.png")
	want := []string{"第一段", "Both tiles above and below see this line", "第二段", "Another line seen by two tiles", "最后一行"}
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

// The browser demo embeds this fixture and shows the text Vision reads in it.
func TestImageTextVisionMatchesTheBrowserDemo(t *testing.T) {
	demo, err := os.ReadFile("web/demo-image.js")
	if err != nil {
		t.Fatal(err)
	}
	png, err := os.ReadFile("testdata/image-text/demo.png")
	if err != nil {
		t.Fatal(err)
	}
	text := recognizeFixture(t, "demo.png")
	quoted := strings.ReplaceAll(text, "\n", `\n`)
	if !strings.Contains(string(demo), "'"+base64.StdEncoding.EncodeToString(png)+"'") || !strings.Contains(string(demo), "demoImageText = '"+quoted+"'") {
		t.Fatalf("web/demo-image.js is out of date; Vision reads:\n%s", text)
	}
}

// The desktop worker recognizes stored screenshots with Vision and saves the
// text where search and the preview read it.
func TestImageTextDesktopWorkerRecognizesStoredScreenshots(t *testing.T) {
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
	if got := detail(t, s.History, e.ID); got.ImageTextState != imageTextReady || got.Title != "剪贴板记录已暂停，新的复制不会保存。" {
		t.Fatalf("detail after recognition: %q %q", got.ImageTextState, got.Title)
	}
}

func TestImageTextVisionRejectsUnreadableImages(t *testing.T) {
	if _, err := recognizeImageText([]byte("not a png")); err == nil {
		t.Fatal("unreadable image recognized")
	}
}

// TestImageTextEvaluation recognizes the corpus of
// testdata/image-text-eval/make_corpus.py with the shipped pipeline, then
// score.py there reports recall and timing. It needs the corpus path:
//
//	CLIPNEST_IMAGE_TEXT_EVAL=<corpus> go test -run '^TestImageTextEvaluation$' -count=1 -timeout 20m .
func TestImageTextEvaluation(t *testing.T) {
	corpus := os.Getenv("CLIPNEST_IMAGE_TEXT_EVAL")
	if corpus == "" {
		t.Skip("set CLIPNEST_IMAGE_TEXT_EVAL to the corpus directory")
	}
	sets, err := filepath.Glob(filepath.Join(corpus, "*", "truth.json"))
	if err != nil || len(sets) == 0 {
		t.Fatal("no truth.json under", corpus, err)
	}
	for _, truth := range sets {
		dir := filepath.Dir(truth)
		data, err := os.ReadFile(truth)
		if err != nil {
			t.Fatal(err)
		}
		var images []struct{ Name string }
		if err := json.Unmarshal(data, &images); err != nil {
			t.Fatal(err)
		}
		var out strings.Builder
		for _, image := range images {
			png, err := os.ReadFile(filepath.Join(dir, image.Name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			text, err := recognizeImageText(png)
			row := map[string]any{"name": image.Name, "text": imageTextResult(text), "ms": time.Since(start).Milliseconds()}
			if err != nil {
				row["error"] = err.Error()
			}
			line, _ := json.Marshal(row)
			out.Write(append(line, '\n'))
		}
		if err := os.WriteFile(filepath.Join(dir, "results.jsonl"), []byte(out.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: %d images", dir, len(images))
	}
}
