package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// transferStorage is an empty SQLite storage.
func transferStorage(t *testing.T) *Storage {
	t.Helper()
	s, err := openStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func addEntries(t *testing.T, s *Storage, entries ...Entry) {
	t.Helper()
	for _, e := range entries {
		if err := s.History.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.History.Save(); err != nil {
		t.Fatal(err)
	}
}

func TestSensitiveEntries(t *testing.T) {
	for _, c := range []struct {
		kind, text, secret string
		want               bool
	}{
		{"text", "普通文字", "", false},
		{"text", "AWS=" + testAWSKey, "", true}, // recorded while privacy was off
		{"text", "规则后来认不出了", "AWS 访问密钥", true},
		{"image", "", "", false},
	} {
		if got := sensitive(c.kind, c.text, c.secret); got != c.want {
			t.Errorf("sensitive(%q, %q, %q) = %v", c.kind, c.text, c.secret, got)
		}
	}
}

func TestValidImageName(t *testing.T) {
	id := strings.Repeat("a1", 32)
	for name, want := range map[string]bool{
		"images/" + id + ".png":                  true,
		"images/" + strings.ToUpper(id) + ".png": false,
		"images/" + id[:63] + ".png":             false,
		"images/../" + id[:61] + ".png":          false,
		id + ".png":                              false,
		"images/" + id + ".jpg":                  false,
	} {
		if validImageName(name) != want {
			t.Errorf("validImageName(%q) != %v", name, want)
		}
	}
}

func TestExportSnapshotDoesNotHoldUpWrites(t *testing.T) {
	s := transferStorage(t)
	addEntries(t, s, entry(t, "导出前"))
	panel, err := s.History.OpenView() // holds one of the storage's connections
	if err != nil {
		t.Fatal(err)
	}
	defer panel.Close()
	db, tx, err := s.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer tx.Rollback()
	late := entry(t, "导出时复制的")
	done := make(chan error, 1)
	go func() {
		s.History.Add(late)
		done <- s.History.Save()
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a copy waited for the snapshot")
	}
	var n int
	if err := tx.QueryRow("SELECT count(*) FROM history").Scan(&n); err != nil || n != 1 {
		t.Fatal("the snapshot moved", n, err)
	}
}

func TestExportSummary(t *testing.T) {
	s := transferStorage(t)
	masked := entry(t, "AWS_SECRET="+testAWSKey)
	masked.mask()
	addEntries(t, s, masked, entry(t, "token "+testGitHub), entry(t, "普通文字"), imageEntry(t, 10))
	if _, err := s.Phrases.Save("", "常用语"); err != nil {
		t.Fatal(err)
	}
	got, err := s.ExportSummary()
	if want := (ExportSummary{History: 4, Images: 1, Phrases: 1, Sensitive: 2}); err != nil || got != want {
		t.Fatal(got, err)
	}
}

// readExport reads every file of an export, by name.
func readExport(t *testing.T, path string) map[string][]byte {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	files := map[string][]byte{}
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[f.Name] = data
		if strings.HasPrefix(f.Name, imagePrefix) && f.Method != zip.Store {
			t.Errorf("%s is compressed again", f.Name)
		}
	}
	return files
}

func exportItems(t *testing.T, files map[string][]byte) []exportItem {
	t.Helper()
	var items []exportItem
	if err := json.Unmarshal(files[historyName], &items); err != nil {
		t.Fatal(err)
	}
	return items
}

func TestExportWritesTheFile(t *testing.T) {
	s := transferStorage(t)
	first, img, second := entry(t, "第一条"), imageEntry(t, 20), entry(t, "a<b & c")
	addEntries(t, s, first, img, second)
	for _, text := range []string{"甲", "乙"} {
		if _, err := s.Phrases.Save("", text); err != nil {
			t.Fatal(err)
		}
	}
	privacy := s.Privacy.Settings()
	privacy.ClearSync = false
	if err := s.Privacy.Save(privacy); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "Clipnest 导出.zip")
	now := time.UnixMilli(1763520000000)
	got, err := s.Export(path, exportOptions{Shortcut: "Alt+V", App: "0.1.17", Now: now})
	if want := (ExportSummary{History: 3, Images: 1, Phrases: 2}); err != nil || got != want {
		t.Fatal(got, err)
	}
	files := readExport(t, path)
	names := slices.Sorted(maps.Keys(files))
	want := []string{imageName(img.ID), historyName, manifestName, phrasesName, settingsName}
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Fatal(names)
	}
	if !bytes.Equal(files[imageName(img.ID)], img.Image) {
		t.Fatal("image changed")
	}
	if !bytes.Contains(files[historyName], []byte("a<b & c")) {
		t.Fatal("HTML escaped", string(files[historyName]))
	}
	// Newest first, as the panel lists them.
	items := exportItems(t, files)
	wantItems := []exportItem{
		{Text: second.Text, Source: second.Source, Created: second.Created},
		{Image: imageName(img.ID), Source: img.Source, Created: img.Created},
		{Text: first.Text, Source: first.Source, Created: first.Created},
	}
	if !reflect.DeepEqual(items, wantItems) {
		t.Fatal(items)
	}
	var manifest exportManifest
	var phrases exportPhrases
	var settings exportSettings
	for name, v := range map[string]any{manifestName: &manifest, phrasesName: &phrases, settingsName: &settings} {
		if err := json.Unmarshal(files[name], v); err != nil {
			t.Fatal(name, err)
		}
	}
	if want := (exportManifest{Format: exportFormat, Version: 1, App: "0.1.17", Platform: runtime.GOOS, Exported: now.UnixMilli(), History: 3, Images: 1, Phrases: 2}); manifest != want {
		t.Fatal(manifest)
	}
	if !slices.Equal(phrases.Phrases, []string{"甲", "乙"}) {
		t.Fatal(phrases)
	}
	wantSettings := exportSettings{Retention: RetentionPolicy{MaxMiB: 512}, ImageText: true, Privacy: privacy, Platform: runtime.GOOS, Shortcut: "Alt+V", ExcludedApps: []ExcludedApp{}}
	if !reflect.DeepEqual(settings, wantSettings) {
		t.Fatal(settings)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("permissions", info.Mode(), err)
		}
	}
	if _, err := os.Stat(path + ".part"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("left the part file", err)
	}
}

func TestExportLeavesOutSensitiveEntriesUnlessAsked(t *testing.T) {
	s := transferStorage(t)
	masked := entry(t, "AWS_SECRET="+testAWSKey)
	masked.mask()
	addEntries(t, s, masked, entry(t, "token "+testGitHub), entry(t, "普通文字"))
	for _, secrets := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "export.zip")
		got, err := s.Export(path, exportOptions{Secrets: secrets, Now: time.Now()})
		history := 1
		if secrets {
			history = 3
		}
		if want := (ExportSummary{History: history, Sensitive: 2}); err != nil || got != want {
			t.Fatal(secrets, got, err)
		}
		files := readExport(t, path)
		if items := exportItems(t, files); len(items) != history {
			t.Fatal(secrets, items)
		}
		var manifest exportManifest
		json.Unmarshal(files[manifestName], &manifest)
		if manifest.Secrets != secrets {
			t.Fatal("manifest says", manifest.Secrets)
		}
	}
}

func TestExportGoesOnWhileRecording(t *testing.T) {
	s := transferStorage(t)
	addEntries(t, s, entry(t, "导出前"))
	path := filepath.Join(t.TempDir(), "export.zip")
	late := entry(t, "导出时复制的")
	_, err := s.Export(path, exportOptions{Now: time.Now(), read: func() error {
		done := make(chan error, 1)
		go func() {
			s.History.Add(late)
			done <- s.History.Save()
		}()
		select {
		case err := <-done:
			return err
		case <-time.After(5 * time.Second):
			return errors.New("a copy waited for the export")
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if items := exportItems(t, readExport(t, path)); len(items) != 1 || items[0].Text != "导出前" {
		t.Fatal(items)
	}
	if entries, _ := storedSnapshot(t, s.History); len(entries) != 2 {
		t.Fatal("the copy was lost", entries)
	}
}

func TestExportFailureLeavesNoFile(t *testing.T) {
	s := transferStorage(t)
	addEntries(t, s, entry(t, "内容"))
	path := filepath.Join(t.TempDir(), "export.zip")
	if err := os.WriteFile(path, []byte("上次的导出"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := s.Export(path, exportOptions{Now: time.Now(), read: func() error { return errors.New("磁盘已满") }})
	if err == nil || !strings.Contains(err.Error(), "磁盘已满") {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "上次的导出" {
		t.Fatal("replaced the old file", string(data))
	}
	if _, err := os.Stat(path + ".part"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("left the part file", err)
	}
}
