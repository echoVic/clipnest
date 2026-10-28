package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Unique counterexamples: S1 trailing document accepted; S2 invalid record
// silently discarded; S3 writer continues after pre-commit failure; S4 caller
// mistakes post-rename uncertainty for rollback; S5 concurrent process owns
// the same directory; S6 stale writer generation resurrects a deletion.
func TestStorageRejectsTrailingDocuments(t *testing.T) {
	for _, suffix := range []string{" {}", " trailing", " null"} {
		t.Run(suffix, func(t *testing.T) {
			dir := t.TempDir()
			for name, body := range map[string]string{"history.json": `{"version":1,"entries":[]}`, "phrases.json": `{"version":1,"phrases":[]}`} {
				path := filepath.Join(dir, name)
				original := []byte(body + suffix)
				if err := os.WriteFile(path, original, 0600); err != nil {
					t.Fatal(err)
				}
				if name == "history.json" {
					if _, err := openHistory(path); err == nil {
						t.Fatal("trailing data accepted")
					}
				} else {
					if _, err := openPhrases(path).Save("", "must not overwrite"); err == nil {
						t.Fatal("trailing phrases accepted")
					}
				}
				after, err := os.ReadFile(path)
				if err != nil || !reflect.DeepEqual(original, after) {
					t.Fatal("original changed", err)
				}
			}
		})
	}
}

func TestStorageRejectsInvalidHistoryWithoutDiscard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	body := `{"version":1,"entries":[{"text":"valid"},{"text":""}]}`
	os.WriteFile(path, []byte(body), 0600)
	if _, err := openHistory(path); err == nil {
		t.Fatal("invalid item silently discarded")
	}
	got, _ := os.ReadFile(path)
	if string(got) != body {
		t.Fatal("original changed")
	}
}

func TestStorageWriteFailureBoundaries(t *testing.T) {
	fault := errors.New("injected durable write fault")
	for _, stage := range []string{"encode", "file-sync", "rename", "directory-sync"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "history.json")
			original := []byte(`{"version":1,"entries":[]}`)
			os.WriteFile(path, original, 0600)
			trace := []string{}
			ops := jsonWriteOps{
				func(f *os.File) error {
					trace = append(trace, "file-sync")
					if stage == "file-sync" {
						return fault
					}
					return f.Sync()
				},
				func(a, b string) error {
					trace = append(trace, "rename")
					if stage == "rename" {
						return fault
					}
					return os.Rename(a, b)
				},
				func(p string) error {
					trace = append(trace, "directory-sync")
					if stage == "directory-sync" {
						return fault
					}
					return syncDirectory(p)
				},
			}
			var value any = archive{1, true, []Entry{}}
			if stage == "encode" {
				value = make(chan int)
			}
			err := writePrivateJSONWith(path, value, ops)
			if err == nil {
				t.Fatal("fault hidden")
			}
			if stage != "encode" && !errors.Is(err, fault) {
				t.Fatal("fault identity lost", err)
			}
			got, _ := os.ReadFile(path)
			expectedTrace := map[string][]string{"encode": {}, "file-sync": {"file-sync"}, "rename": {"file-sync", "rename"}, "directory-sync": {"file-sync", "rename", "directory-sync"}}[stage]
			if !reflect.DeepEqual(trace, expectedTrace) {
				t.Fatal("continued after failure", trace)
			}
			if stage == "directory-sync" {
				if !writeReplaced(err) || reflect.DeepEqual(got, original) {
					t.Fatal("replacement uncertainty misreported")
				}
				h, e := openHistory(path)
				if e != nil {
					t.Fatal(e)
				}
				_, paused := h.Snapshot()
				if !paused {
					t.Fatal("new state not readable")
				}
			} else if writeReplaced(err) || !reflect.DeepEqual(got, original) {
				t.Fatal("precommit failure changed original")
			}
			files, _ := os.ReadDir(dir)
			if len(files) != 1 {
				t.Fatal("temporary files leaked", files)
			}
		})
	}
}

func TestDataLockAcrossProcesses(t *testing.T) {
	if dir := os.Getenv("CLIPNEST_LOCK_TEST_DIR"); dir != "" {
		unlock, err := acquireDataLock(dir)
		if err != nil {
			os.Exit(17)
		}
		unlock()
		os.Exit(0)
	}
	dir := t.TempDir()
	unlock, err := acquireDataLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	locked := true
	defer func() {
		if locked {
			unlock()
		}
	}()
	run := func() error {
		cmd := exec.Command(os.Args[0], "-test.run=^TestDataLockAcrossProcesses$")
		cmd.Env = append(os.Environ(), "CLIPNEST_LOCK_TEST_DIR="+dir)
		return cmd.Run()
	}
	var exit *exec.ExitError
	if err := run(); !errors.As(err, &exit) || exit.ExitCode() != 17 {
		t.Fatalf("second process acquired directory: %v", err)
	}
	unlock()
	locked = false
	if err := run(); err != nil {
		t.Fatal("lock not released", err)
	}
}

func TestHistoryFailedSaveRetriesCurrentGeneration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.json")
	h, _ := openHistory(path)
	a := entry(t, "kept initially")
	h.Add(a)
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}
	os.Remove(path)
	os.Mkdir(path, 0700)
	h.Add(entry(t, "not allowed to resurrect"))
	if err := h.Save(); err == nil {
		t.Fatal("rename failure hidden")
	}
	h.Clear()
	os.Remove(path)
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := openHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := got.Snapshot()
	if len(entries) != 0 {
		t.Fatal("failed snapshot resurrected", entries)
	}
	if files, _ := os.ReadDir(dir); len(files) != 1 {
		t.Fatal("temp files leaked", files)
	}
}

func TestPhrasePostRenameFailureLocksFurtherWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "phrases.json")
	p := openPhrases(path)
	first, err := p.Save("", "old")
	if err != nil {
		t.Fatal(err)
	}
	fault := errors.New("directory acknowledgement lost")
	writes := 0
	p.write = func(path string, v any) error {
		writes++
		return writePrivateJSONWith(path, v, jsonWriteOps{func(f *os.File) error { return f.Sync() }, os.Rename, func(string) error { return fault }})
	}
	_, err = p.Save(first.ID, "new")
	if !errors.Is(err, fault) || !writeReplaced(err) {
		t.Fatal("uncertain commit not surfaced", err)
	}
	reopened := openPhrases(path)
	got, err := reopened.Get(first.ID)
	if err != nil || got.Text != "new" {
		t.Fatal("disk didn't contain replacement", got, err)
	}
	if _, err := p.Save("", "duplicate retry"); err == nil || writes != 1 {
		t.Fatal("uncertain writer accepted more changes", writes, err)
	}
	if _, err := p.List(""); err == nil {
		t.Fatal("read-only state not reported")
	}
}

func TestArchiveBoundAndWhitespace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	os.WriteFile(path, []byte(`{"version":1,"entries":[]} `+strings.Repeat("\n", 5)), 0600)
	if _, err := openHistory(path); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	err = f.Truncate(maxArchiveBytes + 1)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openHistory(path); err == nil {
		t.Fatal("oversized archive accepted")
	}
}
