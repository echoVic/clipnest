package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// movedData moves a storage from old to target the way the preferences do,
// recording the move in def, and returns the paths.
func movedData(t *testing.T, old, def string) (target string) {
	t.Helper()
	s, err := openStorage(old)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.History.Add(entry(t, "搬家"))
	target = filepath.Join(t.TempDir(), "New")
	if err := s.MoveTo(target, func() error {
		return commitMove(def, dataLocation{Dir: target, Previous: old})
	}); err != nil {
		t.Fatal(err)
	}
	return target
}

func openChosen(t *testing.T, def string) openedData {
	t.Helper()
	d, err := openDataDir(chooseDataDir("", def), def)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.store.Close(); d.unlock() })
	return d
}

func clipnestFilesIn(t *testing.T, dir string) []string {
	t.Helper()
	var found []string
	for _, name := range append([]string{databaseName, databaseName + "-wal", databaseName + "-shm", ".writer.lock", "history.json", "phrases.json"}, movedSettings...) {
		if _, err := os.Lstat(filepath.Join(dir, name)); err == nil {
			found = append(found, name)
		}
	}
	return found
}

func TestOpenDataDirFinishesAMove(t *testing.T) {
	root := t.TempDir()
	def := filepath.Join(root, "Default")
	old := filepath.Join(root, "Old")
	writeFile(t, filepath.Join(old, "notes.txt"))
	target := movedData(t, old, def)
	d := openChosen(t, def)
	if d.dir != target || !strings.Contains(d.notice, target) {
		t.Fatal(d.dir, d.notice)
	}
	if loc, err := readDataLocation(def); err != nil || loc != (dataLocation{Dir: target}) {
		t.Fatal("not committed:", loc, err)
	}
	if found := clipnestFilesIn(t, old); len(found) != 0 {
		t.Fatal("old files left:", found)
	}
	if _, err := os.Stat(filepath.Join(old, "notes.txt")); err != nil {
		t.Fatal("deleted a file that isn't Clipnest's:", err)
	}
	if entries, _ := storedSnapshot(t, d.store.History); len(entries) != 1 {
		t.Fatal("history:", entries)
	}
}

func TestOpenDataDirRemovesTheEmptiedOldFolder(t *testing.T) {
	root := t.TempDir()
	def := filepath.Join(root, "Default")
	old := filepath.Join(root, "Old")
	movedData(t, old, def)
	openChosen(t, def)
	if _, err := os.Stat(old); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("old folder left:", err)
	}
}

func TestOpenDataDirKeepsTheDefaultFolder(t *testing.T) {
	root := t.TempDir()
	def := filepath.Join(root, "Default")
	writeFile(t, filepath.Join(def, "updater.json"))
	target := movedData(t, def, def)
	d := openChosen(t, def)
	if d.dir != target {
		t.Fatal(d.dir)
	}
	if found := clipnestFilesIn(t, def); len(found) != 0 {
		t.Fatal("old files left:", found)
	}
	for _, name := range []string{"updater.json", dataLocationName} {
		if _, err := os.Stat(filepath.Join(def, name)); err != nil {
			t.Fatal(name, err)
		}
	}
}

func TestOpenDataDirGoesBackWhenTheNewPlaceWontOpen(t *testing.T) {
	for _, old := range []string{"Old", "Default"} {
		t.Run(old, func(t *testing.T) {
			root := t.TempDir()
			def := filepath.Join(root, "Default")
			old := filepath.Join(root, old)
			target := movedData(t, old, def)
			if err := os.WriteFile(filepath.Join(target, databaseName), []byte("not a database"), 0600); err != nil {
				t.Fatal(err)
			}
			d := openChosen(t, def)
			if d.dir != old || !strings.Contains(d.notice, "改回") {
				t.Fatal(d.dir, d.notice)
			}
			loc, err := readDataLocation(def)
			if err != nil || loc.Previous != "" || (loc.Dir != old && !(loc.Dir == "" && old == def)) {
				t.Fatal("record:", loc, err)
			}
			if found := clipnestFilesIn(t, target); len(found) != 0 {
				t.Fatal("copy left:", found)
			}
			if entries, _ := storedSnapshot(t, d.store.History); len(entries) != 1 {
				t.Fatal("old data:", entries)
			}
		})
	}
}

func TestOpenDataDirGoesBackWhenTheNewPlaceIsGone(t *testing.T) {
	root := t.TempDir()
	def := filepath.Join(root, "Default")
	old := filepath.Join(root, "Old")
	target := movedData(t, old, def)
	if err := os.RemoveAll(target); err != nil {
		t.Fatal(err)
	}
	if d := openChosen(t, def); d.dir != old {
		t.Fatal(d.dir)
	}
	if _, err := os.Stat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("recreated the missing directory:", err)
	}
}

func TestOpenDataDirReportsAnUnavailableDirectory(t *testing.T) {
	root := t.TempDir()
	def := filepath.Join(root, "Default")
	if err := writeDataLocation(def, dataLocation{Dir: filepath.Join(root, "Gone")}); err != nil {
		t.Fatal(err)
	}
	if _, err := openDataDir(chooseDataDir("", def), def); err == nil {
		t.Fatal("opened")
	}
}

func TestOpenDataDirReportsOldFilesItCouldNotDelete(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits don't make a folder read-only on Windows")
	}
	root := t.TempDir()
	def := filepath.Join(root, "Default")
	old := filepath.Join(root, "Old")
	target := movedData(t, old, def)
	if err := os.Chmod(old, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(old, 0700) })
	d := openChosen(t, def)
	if d.dir != target || !strings.Contains(d.notice, "手动删除") || !strings.Contains(d.notice, old) {
		t.Fatal(d.dir, d.notice)
	}
	if loc, _ := readDataLocation(def); loc != (dataLocation{Dir: target}) {
		t.Fatal("went back after committing:", loc)
	}
}

func TestPrepareDataDir(t *testing.T) {
	root := t.TempDir()
	blank := filepath.Join(root, "Blank")
	if err := os.Mkdir(blank, 0700); err != nil {
		t.Fatal(err)
	}
	if err := prepareDataDir(blank); err != nil {
		t.Fatal(err)
	}
	if found, _ := hasClipnestData(blank); !found {
		t.Fatal("no database created")
	}
	broken := filepath.Join(root, "Broken")
	writeFile(t, filepath.Join(broken, databaseName))
	if err := prepareDataDir(broken); err == nil {
		t.Fatal("a broken database passed")
	}
	unlock, err := acquireDataLock(blank)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := prepareDataDir(blank); err == nil {
		t.Fatal("opened a directory another instance uses")
	}
}

// Another instance using the new place, say one started with --data-dir,
// keeps its files.
func TestOpenDataDirLeavesALockedNewPlaceAlone(t *testing.T) {
	root := t.TempDir()
	def := filepath.Join(root, "Default")
	old := filepath.Join(root, "Old")
	target := movedData(t, old, def)
	unlock, err := acquireDataLock(target)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	defer func(wait time.Duration) { lockWait = wait }(lockWait)
	lockWait = 100 * time.Millisecond
	if d := openChosen(t, def); d.dir != old {
		t.Fatal(d.dir)
	}
	if found, _ := hasClipnestData(target); !found {
		t.Fatal("deleted the files of the instance using it")
	}
}

// readOnly makes dir read-only until the test ends.
func readOnly(t *testing.T, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("permission bits don't make a folder read-only on Windows")
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0700) })
}

// A launch never runs on a place whose move it can't commit: a later launch
// would still take it for an unused copy, and delete it on going back.
func TestOpenDataDirUndoesAMoveItCantCommit(t *testing.T) {
	root := t.TempDir()
	def := filepath.Join(root, "Default")
	old := filepath.Join(root, "Old")
	target := movedData(t, old, def)
	readOnly(t, def)
	d, err := openDataDir(chooseDataDir("", def), def)
	if err != nil {
		t.Fatal(err)
	}
	if d.dir != old || !strings.Contains(d.notice, "改回") {
		t.Fatal(d.dir, d.notice)
	}
	if found, _ := hasClipnestData(target); found {
		t.Fatal("left a copy a later launch would open")
	}
	if err := d.store.History.Add(entry(t, "撤销之后")); err != nil {
		t.Fatal(err)
	}
	d.store.Close()
	d.unlock()

	os.Chmod(def, 0700)
	d = openChosen(t, def)
	if d.dir != old {
		t.Fatal(d.dir)
	}
	if entries, _ := storedSnapshot(t, d.store.History); len(entries) != 2 {
		t.Fatal("history:", entries)
	}
	if loc, err := readDataLocation(def); err != nil || loc != (dataLocation{Dir: old}) {
		t.Fatal("record:", loc, err)
	}
}

// Going back to an old place that is gone creates nothing there and keeps
// the copy, which may be all that is left.
func TestOpenDataDirChecksTheOldPlaceBeforeGoingBack(t *testing.T) {
	root := t.TempDir()
	def := filepath.Join(root, "Default")
	old := filepath.Join(root, "Old")
	target := movedData(t, old, def)
	if err := os.WriteFile(filepath.Join(target, "privacy.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(old); err != nil {
		t.Fatal(err)
	}
	if _, err := openDataDir(chooseDataDir("", def), def); err == nil {
		t.Fatal("opened")
	}
	if _, err := os.Stat(old); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("created the old place again:", err)
	}
	if found, _ := hasClipnestData(target); !found {
		t.Fatal("deleted the copy")
	}
	if loc, _ := readDataLocation(def); loc != (dataLocation{Dir: target, Previous: old}) {
		t.Fatal("record changed:", loc)
	}
}

// An old place that can't be reached keeps its data, and the notice says so.
func TestOpenDataDirReportsAnOldPlaceItCantReach(t *testing.T) {
	root := t.TempDir()
	def := filepath.Join(root, "Default")
	old := filepath.Join(root, "Old")
	target := movedData(t, old, def)
	if err := os.Rename(old, old+"-away"); err != nil {
		t.Fatal(err)
	}
	d := openChosen(t, def)
	if d.dir != target || !strings.Contains(d.notice, "无法访问") || !strings.Contains(d.notice, old) {
		t.Fatal(d.dir, d.notice)
	}
	if _, err := os.Stat(old); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("created the old place again:", err)
	}
	if loc, _ := readDataLocation(def); loc != (dataLocation{Dir: target}) {
		t.Fatal("not committed:", loc)
	}
}

// holdLock locks dir the way the instance that moved the data does until it
// exits, and lets go after a while. stillThere reports whether the database
// in dir was still there when it let go.
func holdLock(t *testing.T, dir string) (stillThere func() bool) {
	t.Helper()
	unlock, err := acquireDataLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan bool, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		_, err := os.Stat(filepath.Join(dir, databaseName))
		unlock()
		done <- err == nil
	}()
	return func() bool { return <-done }
}

func TestOpenDataDirWaitsForTheInstanceThatMoved(t *testing.T) {
	root := t.TempDir()
	def := filepath.Join(root, "Default")
	old := filepath.Join(root, "Old")
	target := movedData(t, old, def)
	stillThere := holdLock(t, old)
	d := openChosen(t, def)
	if !stillThere() {
		t.Fatal("deleted the old files while the old instance held them")
	}
	if d.dir != target || strings.Contains(d.notice, "手动删除") {
		t.Fatal(d.dir, d.notice)
	}
	if found := clipnestFilesIn(t, old); len(found) != 0 {
		t.Fatal("old files left:", found)
	}
}

func TestOpenDataDirWaitsToGoBack(t *testing.T) {
	root := t.TempDir()
	def := filepath.Join(root, "Default")
	old := filepath.Join(root, "Old")
	target := movedData(t, old, def)
	if err := os.WriteFile(filepath.Join(target, databaseName), []byte("not a database"), 0600); err != nil {
		t.Fatal(err)
	}
	holdLock(t, old)
	if d := openChosen(t, def); d.dir != old {
		t.Fatal(d.dir)
	}
}

// A copy on a disk that isn't connected can't be deleted. Going back with the
// record unchanged would let the copy win once the disk is back, so Clipnest
// asks instead.
func TestOpenDataDirWontGoBackWithACopyItCantDelete(t *testing.T) {
	root := t.TempDir()
	def := filepath.Join(root, "Default")
	old := filepath.Join(root, "Old")
	target := movedData(t, old, def)
	if err := os.Rename(target, target+"-away"); err != nil {
		t.Fatal(err)
	}
	readOnly(t, def)
	if _, err := openDataDir(chooseDataDir("", def), def); err == nil {
		t.Fatal("went back with the record still naming the copy")
	}
}

// The copy stays until the old place has opened.
func TestOpenDataDirKeepsTheCopyWhenTheOldPlaceWontOpen(t *testing.T) {
	root := t.TempDir()
	def := filepath.Join(root, "Default")
	old := filepath.Join(root, "Old")
	target := movedData(t, old, def)
	if err := os.WriteFile(filepath.Join(target, "privacy.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "privacy.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := openDataDir(chooseDataDir("", def), def); err == nil {
		t.Fatal("opened")
	}
	if found, _ := hasClipnestData(target); !found {
		t.Fatal("deleted the copy")
	}
}
