package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestResolveTargetFolders(t *testing.T) {
	root := t.TempDir()
	def := filepath.Join(root, "Default")
	current := clipnestDataDir(t, filepath.Join(root, "Current"))

	withData := clipnestDataDir(t, filepath.Join(root, "WithData"))
	if got, err := resolveTarget(withData, current, def); err != nil || got.dir != withData || !got.existing {
		t.Fatal("a folder with data:", got, err)
	}

	empty := filepath.Join(root, "Empty")
	for _, name := range []string{".DS_Store", "desktop.ini", ".writer.lock", ".clipnest-move-123/" + databaseName} {
		writeFile(t, filepath.Join(empty, name))
	}
	if got, err := resolveTarget(empty, current, def); err != nil || got.dir != empty || got.existing {
		t.Fatal("an empty folder:", got, err)
	}

	busy := filepath.Join(root, "Busy")
	writeFile(t, filepath.Join(busy, "notes.txt"))
	if got, err := resolveTarget(busy, current, def); err != nil || got.dir != filepath.Join(busy, "Clipnest") || got.existing {
		t.Fatal("a folder with other files:", got, err)
	}
	clipnestDataDir(t, filepath.Join(busy, "Clipnest"))
	if got, err := resolveTarget(busy, current, def); err != nil || got.dir != filepath.Join(busy, "Clipnest") || !got.existing {
		t.Fatal("its Clipnest folder has data:", got, err)
	}

	// Another app's phrases.json, or JSON data from before SQLite, is not
	// data to switch to: Clipnest would open it as JSON storage that can't
	// move, and later delete files by such common names.
	others := filepath.Join(root, "Others")
	writeFile(t, filepath.Join(others, "phrases.json"))
	if got, err := resolveTarget(others, current, def); err != nil || got.dir != filepath.Join(others, "Clipnest") || got.existing {
		t.Fatal("a folder with another phrases.json:", got, err)
	}

	crowded := filepath.Join(root, "Crowded")
	writeFile(t, filepath.Join(crowded, "notes.txt"))
	writeFile(t, filepath.Join(crowded, "Clipnest", "photo.jpg"))
	if got, err := resolveTarget(crowded, current, def); err == nil {
		t.Fatal("its Clipnest folder has other files:", got)
	}
}

func TestResolveTargetDefaultDirectory(t *testing.T) {
	root := t.TempDir()
	def := filepath.Join(root, "Default")
	writeFile(t, filepath.Join(def, "updater.json"))
	writeFile(t, filepath.Join(def, dataLocationName))
	current := clipnestDataDir(t, filepath.Join(root, "Current"))
	if got, err := resolveTarget(def, current, def); err != nil || got.dir != def || got.existing {
		t.Fatal("the default directory:", got, err)
	}
	clipnestDataDir(t, def)
	if got, err := resolveTarget(def, current, def); err != nil || got.dir != def || !got.existing {
		t.Fatal("the default directory with data:", got, err)
	}
}

func TestResolveTargetRefusesTheCurrentDirectory(t *testing.T) {
	root := t.TempDir()
	def := filepath.Join(root, "Default")
	current := clipnestDataDir(t, filepath.Join(root, "Parent", "Clipnest"))
	if _, err := resolveTarget(current, current, def); err == nil {
		t.Fatal("moved onto itself")
	}
	inside := filepath.Join(current, "Inside")
	if err := os.Mkdir(inside, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveTarget(inside, current, def); err == nil {
		t.Fatal("moved into itself")
	}
	// The parent is not empty, so the move would go to its Clipnest folder,
	// the current one.
	if _, err := resolveTarget(filepath.Join(root, "Parent"), current, def); err == nil {
		t.Fatal("moved onto itself through its parent")
	}
	// Starting up without a usable directory, there is none to refuse.
	if got, err := resolveTarget(current, "", def); err != nil || !got.existing {
		t.Fatal(got, err)
	}
}

func TestResolveTargetRefusesReadOnlyFolders(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits don't make a folder read-only on Windows")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "Locked")
	if err := os.Mkdir(locked, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0700) })
	if got, err := resolveTarget(locked, clipnestDataDir(t, filepath.Join(root, "Current")), filepath.Join(root, "Default")); err == nil {
		t.Fatal("a read-only folder:", got)
	}
}

func TestResolveTargetNamesSyncedFolders(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cases := map[string]string{
		filepath.Join(home, "Dropbox", "Clipnest"): "Dropbox",
		filepath.Join(home, "Documents"):           "",
	}
	switch runtime.GOOS {
	case "darwin":
		cases[filepath.Join(home, "Library", "Mobile Documents", "com~apple~CloudDocs")] = "iCloud 云盘"
		cases[filepath.Join(home, "Library", "CloudStorage", "OneDrive-Personal", "Clipnest")] = "OneDrive"
		cases[filepath.Join(home, "Library", "CloudStorage", "GoogleDrive-someone@example.com")] = "GoogleDrive"
	case "windows":
		t.Setenv("OneDrive", filepath.Join(home, "OneDrive"))
		t.Setenv("OneDriveCommercial", filepath.Join(home, "OneDrive - Company"))
		cases[filepath.Join(home, "OneDrive", "Clipnest")] = "OneDrive"
		cases[filepath.Join(home, "OneDrive - Company")] = "OneDrive"
	}
	for dir, want := range cases {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if got := syncService(dir); got != want {
			t.Errorf("%s: %q, want %q", dir, got, want)
		}
	}
	got, err := resolveTarget(filepath.Join(home, "Dropbox", "Clipnest"), clipnestDataDir(t, filepath.Join(home, "Current")), filepath.Join(home, "Default"))
	if err != nil || got.synced != "Dropbox" {
		t.Fatal(got, err)
	}
}
