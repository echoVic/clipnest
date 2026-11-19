package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func exportOf(t *testing.T, s *Storage, secrets bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "export.zip")
	if _, err := s.Export(path, exportOptions{Secrets: secrets, Shortcut: "Alt+V", App: "0.1.17", Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return path
}

// editZip copies the ZIP at path, passing each file through edit, which
// returns its new content, or false to leave the file out.
func editZip(t *testing.T, path string, edit func(name string, content []byte) ([]byte, bool)) string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var files [][2]string
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if content, keep := edit(f.Name, content); keep {
			files = append(files, [2]string{f.Name, string(content)})
		}
	}
	return testZip(t, files...)
}

// rewriteZip copies the ZIP at path with name's content replaced.
func rewriteZip(t *testing.T, path, name, data string) string {
	t.Helper()
	return editZip(t, path, func(n string, content []byte) ([]byte, bool) {
		if n == name {
			return []byte(data), true
		}
		return content, true
	})
}

// testZipWithout copies the ZIP at path without name.
func testZipWithout(t *testing.T, path, name string) string {
	t.Helper()
	return editZip(t, path, func(n string, content []byte) ([]byte, bool) { return content, n != name })
}

func withSettings(t *testing.T, path string, change func(*exportSettings)) string {
	t.Helper()
	f := openImport(t, path)
	s, err := f.settings()
	if err != nil || s == nil {
		t.Fatal(s, err)
	}
	change(s)
	data, _ := json.Marshal(s)
	return rewriteZip(t, path, settingsName, string(data))
}

func textEntry(t *testing.T, text, source string, created int64) Entry {
	t.Helper()
	e, err := newEntry(text, nil, source)
	if err != nil {
		t.Fatal(err)
	}
	e.Created = created
	return e
}

func historyTexts(t *testing.T, s *Storage) []string {
	t.Helper()
	entries, _ := storedSnapshot(t, s.History)
	var texts []string
	for _, e := range entries {
		texts = append(texts, e.Text)
	}
	return texts
}

func phraseList(t *testing.T, s *Storage) []string {
	t.Helper()
	texts, err := s.Phrases.texts()
	if err != nil {
		t.Fatal(err)
	}
	return texts
}

func savePhrases(t *testing.T, s *Storage, texts ...string) {
	t.Helper()
	for _, text := range texts {
		if _, err := s.Phrases.Save("", text); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPlanImportCounts(t *testing.T) {
	to := transferStorage(t)
	addEntries(t, to, textEntry(t, "乙", "这边", 1000))
	savePhrases(t, to, "二")
	privacy := to.Privacy.Settings()
	privacy.Secrets = secretsSkip
	to.Privacy.Save(privacy)

	from := transferStorage(t)
	addEntries(t, from, textEntry(t, "甲", "那边", 500), textEntry(t, "乙", "那边", 2000), imageEntry(t, 3), textEntry(t, "AWS="+testAWSKey, "那边", 3000))
	savePhrases(t, from, "一", "二", "三")
	path := exportOf(t, from, true)

	before := historyTexts(t, to)
	plan, err := to.PlanImport(path)
	if err != nil {
		t.Fatal(err)
	}
	want := ImportPlan{New: 2, Existing: 1, Skipped: 1, NewPhrases: 2, DuplicatePhrases: 1}
	got := plan
	got.Manifest, got.Settings, got.PrunedWithSettings = exportManifest{}, nil, 0
	if got != want || plan.Settings == nil || plan.Manifest.App != "0.1.17" {
		t.Fatalf("%+v", plan)
	}
	if !slices.Equal(historyTexts(t, to), before) || !slices.Equal(phraseList(t, to), []string{"二"}) {
		t.Fatal("the check wrote")
	}
}

func TestPlanImportFillsThePhrasesUp(t *testing.T) {
	to := transferStorage(t)
	for i := range maxPhrases - 1 {
		savePhrases(t, to, fmt.Sprint("已有 ", i))
	}
	from := transferStorage(t)
	savePhrases(t, from, "新的一", "新的二", "新的三")
	plan, err := to.PlanImport(exportOf(t, from, false))
	if err != nil || plan.NewPhrases != 1 || plan.PhrasesOver != 2 {
		t.Fatalf("%+v %v", plan, err)
	}
}

func TestPlanImportPredictsCleanup(t *testing.T) {
	to := transferStorage(t)
	now := time.Now().UnixMilli()
	addEntries(t, to, textEntry(t, "新一", "", now-1000), textEntry(t, "新二", "", now))
	if err := to.History.SetRetention(RetentionPolicy{MaxEntries: 2}); err != nil {
		t.Fatal(err)
	}
	from := transferStorage(t)
	addEntries(t, from, textEntry(t, "旧一", "", now-9000), textEntry(t, "旧二", "", now-8000), textEntry(t, "旧三", "", now-7000))
	plan, err := to.PlanImport(exportOf(t, from, false))
	if err != nil || plan.Pruned != 3 || plan.PrunedNew != 3 || plan.PrunedWithSettings != 0 {
		t.Fatalf("%+v %v", plan, err)
	}
}

func TestPlanImportReportsInvalidSettings(t *testing.T) {
	from := transferStorage(t)
	path := withSettings(t, exportOf(t, from, false), func(s *exportSettings) { s.Retention.KeepDays = -1 })
	plan, err := transferStorage(t).PlanImport(path)
	if err != nil || plan.Settings != nil || plan.SettingsProblem == "" {
		t.Fatalf("%+v %v", plan, err)
	}
}

func TestPlanImportNothingNew(t *testing.T) {
	s := transferStorage(t)
	addEntries(t, s, entry(t, "一样"))
	// Without settings.json there are no settings to offer either.
	path := testZipWithout(t, exportOf(t, s, false), settingsName)
	plan, err := s.PlanImport(path)
	if err != nil || !plan.nothingNew() || plan.Existing != 1 {
		t.Fatalf("%+v %v", plan, err)
	}
}
