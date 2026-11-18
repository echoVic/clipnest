//go:build darwin || windows

package main

import (
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

func TestImageTextRejectsUnreadableImages(t *testing.T) {
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
