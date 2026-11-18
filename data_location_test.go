package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// clipnestDataDir makes dir hold Clipnest data, as a moved-to directory does.
func clipnestDataDir(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, databaseName), nil, 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestChooseDataDirOrder(t *testing.T) {
	root := t.TempDir()
	def := filepath.Join(root, "Clipnest")
	if got := chooseDataDir("", def); got.dir != def || got.fixed || got.previous != "" || got.problem != nil {
		t.Fatalf("no record: %+v", got)
	}
	moved := clipnestDataDir(t, filepath.Join(root, "Moved"))
	if err := writeDataLocation(def, dataLocation{Dir: moved, Previous: def}); err != nil {
		t.Fatal(err)
	}
	if got := chooseDataDir("", def); got.dir != moved || got.previous != def || got.fixed || got.problem != nil {
		t.Fatalf("record: %+v", got)
	}
	flag := filepath.Join(root, "Flag")
	if got := chooseDataDir(flag, def); got.dir != flag || !got.fixed || got.previous != "" || got.problem != nil {
		t.Fatalf("--data-dir: %+v", got)
	}
}

func TestChooseDataDirUnavailable(t *testing.T) {
	root := t.TempDir()
	def := filepath.Join(root, "Clipnest")
	missing := filepath.Join(root, "Unplugged", "Clipnest")
	if err := writeDataLocation(def, dataLocation{Dir: missing}); err != nil {
		t.Fatal(err)
	}
	if got := chooseDataDir("", def); got.dir != missing || got.problem == nil {
		t.Fatalf("missing directory: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(root, "Unplugged")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("choosing created the missing directory:", err)
	}
	empty := filepath.Join(root, "Empty")
	if err := os.Mkdir(empty, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeDataLocation(def, dataLocation{Dir: empty}); err != nil {
		t.Fatal(err)
	}
	if got := chooseDataDir("", def); got.problem == nil {
		t.Fatalf("a directory without data: %+v", got)
	}
	if err := os.WriteFile(filepath.Join(def, dataLocationName), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := chooseDataDir("", def); got.problem == nil {
		t.Fatalf("broken record: %+v", got)
	}
}

func TestDefaultDataDirIsAlwaysAvailable(t *testing.T) {
	root := t.TempDir()
	def := filepath.Join(root, "Clipnest")
	// Missing, and without data: the first launch creates it.
	if got := chooseDataDir("", def); got.dir != def || got.problem != nil {
		t.Fatalf("%+v", got)
	}
	if err := writeDataLocation(def, dataLocation{Dir: def, Previous: filepath.Join(root, "Old")}); err != nil {
		t.Fatal(err)
	}
	if got := chooseDataDir("", def); got.dir != def || got.problem != nil || got.previous == "" {
		t.Fatalf("record of the default: %+v", got)
	}
}

func TestWritingTheDefaultLocationRemovesTheRecord(t *testing.T) {
	root := t.TempDir()
	def := filepath.Join(root, "Clipnest")
	other := clipnestDataDir(t, filepath.Join(root, "Other"))
	if err := writeDataLocation(def, dataLocation{Dir: other}); err != nil {
		t.Fatal(err)
	}
	if loc, err := readDataLocation(def); err != nil || loc.Dir != other {
		t.Fatal(loc, err)
	}
	if err := writeDataLocation(def, dataLocation{Dir: def}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(def, dataLocationName)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("record left behind:", err)
	}
	if loc, err := readDataLocation(def); err != nil || loc != (dataLocation{}) {
		t.Fatal(loc, err)
	}
}

func TestDataLocationRejectsRelativePaths(t *testing.T) {
	def := filepath.Join(t.TempDir(), "Clipnest")
	if err := os.MkdirAll(def, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(def, dataLocationName), []byte(`{"dir":"relative/Clipnest"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := chooseDataDir("", def); got.problem == nil {
		t.Fatalf("%+v", got)
	}
}
