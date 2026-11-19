package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// movableStorage is a SQLite storage with history (text, an image and its
// recognized text), a phrase, a paused state and the JSON settings files.
func movableStorage(t *testing.T) (*Storage, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "Old")
	s, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.History.SetImageText(true); err != nil {
		t.Fatal(err)
	}
	s.History.Add(entry(t, "第一条"))
	s.History.Add(imageEntry(t, 40))
	s.History.Pause(true)
	if err := s.History.Save(); err != nil {
		t.Fatal(err)
	}
	if err := s.History.recognizePending(func([]byte) (string, error) { return "图里的字", nil }, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Phrases.Save("", "常用语"); err != nil {
		t.Fatal(err)
	}
	if err := s.Exclusions.Add(ExcludedApp{BundleID: "com.example.vault", Name: "Vault"}); err != nil {
		t.Fatal(err)
	}
	settings := s.Privacy.Settings()
	settings.ClearSync = !settings.ClearSync
	if err := s.Privacy.Save(settings); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shortcut.json"), []byte(`{"version":1,"accelerator":"Alt+V"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return s, dir
}

// tableCounts counts the rows of every table of the database in dir.
func tableCounts(t *testing.T, dir string) map[string]int {
	t.Helper()
	db, err := openSQLite(filepath.Join(dir, databaseName), false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.db.Close()
	counts, err := countTables(db.db)
	if err != nil {
		t.Fatal(err)
	}
	return counts
}

func noMoveLeftovers(t *testing.T, target string) {
	t.Helper()
	entries, err := os.ReadDir(target)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "stray.txt" {
			t.Errorf("left in the target: %s", e.Name())
		}
	}
}

// stillWorks checks the storage a failed move leaves in use.
func stillWorks(t *testing.T, s *Storage) {
	t.Helper()
	s.History.Pause(false)
	if err := s.History.Add(entry(t, "移动失败之后")); err != nil {
		t.Fatal(err)
	}
	if err := s.History.Save(); err != nil {
		t.Fatal("history no longer saves:", err)
	}
	if _, err := s.Phrases.Save("", "移动失败之后"); err != nil {
		t.Fatal("phrases no longer save:", err)
	}
}

func TestMoveToCopiesEverything(t *testing.T) {
	s, old := movableStorage(t)
	// Not saved yet: the move writes it first.
	s.History.Pause(false)
	pending := entry(t, "还没保存")
	s.History.Add(pending)
	target := filepath.Join(filepath.Dir(old), "New")
	commits := 0
	if err := s.MoveTo(target, func() error { commits++; return nil }); err != nil {
		t.Fatal(err)
	}
	if commits != 1 {
		t.Fatal("commits:", commits)
	}
	if err := s.Close(); err != nil {
		t.Fatal("closing a moved storage:", err)
	}
	if got, want := tableCounts(t, target), tableCounts(t, old); !reflect.DeepEqual(got, want) || got["history_ocr"] == 0 {
		t.Fatalf("rows: %v, want %v", got, want)
	}
	for _, name := range []string{"excluded-apps.json", "privacy.json", "shortcut.json"} {
		a, errA := os.ReadFile(filepath.Join(old, name))
		b, errB := os.ReadFile(filepath.Join(target, name))
		if errA != nil || errB != nil || !bytes.Equal(a, b) {
			t.Errorf("%s: %v %v", name, errA, errB)
		}
	}
	moved, err := openStorage(target)
	if err != nil {
		t.Fatal(err)
	}
	defer moved.Close()
	entries, _ := storedSnapshot(t, moved.History)
	if len(entries) != 3 || entries[0].ID != pending.ID {
		t.Fatal("history:", len(entries))
	}
	if phrases, err := moved.Phrases.List(""); err != nil || len(phrases) != 1 {
		t.Fatal("phrases:", phrases, err)
	}
	if !moved.Exclusions.Contains("com.example.vault") {
		t.Fatal("excluded apps lost")
	}
	if matches, _ := filepath.Glob(filepath.Join(target, moveStagingPrefix+"*")); len(matches) != 0 {
		t.Fatal("staging left behind:", matches)
	}
}

// The history holds passwords and keys, so the copy is private even in a
// folder others can read, like one made with the picker's New Folder.
func TestMoveToKeepsTheCopyPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions")
	}
	s, old := movableStorage(t)
	target := filepath.Join(filepath.Dir(old), "Shared")
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveTo(target, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	moved, err := openStorage(target)
	if err != nil {
		t.Fatal(err)
	}
	defer moved.Close()
	if err := moved.History.Add(entry(t, "移动之后")); err != nil {
		t.Fatal(err)
	}
	if err := moved.History.Save(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{databaseName, databaseName + "-wal", databaseName + "-shm", "privacy.json", "shortcut.json"} {
		info, err := os.Stat(filepath.Join(target, name))
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Errorf("%s: %v, want 0600", name, perm)
		}
	}
}

func TestMoveToFailuresLeaveNoTrace(t *testing.T) {
	failure := errors.New("injected")
	cases := map[string]struct {
		ops    func(*moveOps)
		commit func() error
	}{
		"copy": {ops: func(o *moveOps) { o.staged = func(string) error { return failure } }},
		"verify": {ops: func(o *moveOps) {
			o.staged = func(stage string) error {
				return os.WriteFile(filepath.Join(stage, "privacy.json"), []byte("{}"), 0600)
			}
		}},
		"verify database": {ops: func(o *moveOps) {
			o.staged = func(stage string) error {
				db, err := openSQLite(filepath.Join(stage, databaseName), false)
				if err != nil {
					return err
				}
				defer db.db.Close()
				_, err = db.db.Exec("DELETE FROM phrases")
				return err
			}
		}},
		"rename": {ops: func(o *moveOps) {
			renames := 0
			o.rename = func(from, to string) error {
				if renames++; renames == 2 {
					return failure
				}
				return os.Rename(from, to)
			}
		}},
		"commit": {commit: func() error { return failure }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s, old := movableStorage(t)
			target := filepath.Join(filepath.Dir(old), "New")
			ops := defaultMoveOps()
			if c.ops != nil {
				c.ops(&ops)
			}
			commit := c.commit
			if commit == nil {
				commit = func() error { return nil }
			}
			if err := s.moveTo(target, commit, ops); err == nil {
				t.Fatal("moved")
			}
			noMoveLeftovers(t, target)
			stillWorks(t, s)
		})
	}
}

func TestMoveToKeepsFilesAlreadyInTheTarget(t *testing.T) {
	s, old := movableStorage(t)
	target := filepath.Join(filepath.Dir(old), "New")
	writeFile(t, filepath.Join(target, "stray.txt"))
	writeFile(t, filepath.Join(target, "privacy.json"))
	if err := s.MoveTo(target, func() error { t.Fatal("committed"); return nil }); err == nil {
		t.Fatal("replaced privacy.json")
	}
	if b, err := os.ReadFile(filepath.Join(target, "privacy.json")); err != nil || string(b) != "x" {
		t.Fatal("privacy.json changed:", string(b), err)
	}
	os.Remove(filepath.Join(target, "privacy.json"))
	noMoveLeftovers(t, target)
	stillWorks(t, s)
}

func TestMoveToRefusesALockedTarget(t *testing.T) {
	s, old := movableStorage(t)
	target := filepath.Join(filepath.Dir(old), "New")
	unlock, err := acquireDataLock(target)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := s.MoveTo(target, func() error { return nil }); err == nil {
		t.Fatal("moved into a directory another instance uses")
	}
	if found, _ := hasClipnestData(target); found {
		t.Fatal("copied into it")
	}
	stillWorks(t, s)
}

func TestMoveToNeedsSQLite(t *testing.T) {
	dir := t.TempDir()
	seedLegacyStorage(t, dir)
	s, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.MoveTo(filepath.Join(t.TempDir(), "New"), func() error { return nil }); err == nil || !strings.Contains(err.Error(), "SQLite") {
		t.Fatal(err)
	}
}

// The clipboard monitor reads the paused state on the main thread, so a
// long copy must not hold the history lock.
func TestMoveToLeavesTheHistoryLockFree(t *testing.T) {
	s, old := movableStorage(t)
	copying, release := make(chan struct{}), make(chan struct{})
	ops := defaultMoveOps()
	ops.staged = func(string) error { close(copying); <-release; return nil }
	done := make(chan error, 1)
	go func() { done <- s.moveTo(filepath.Join(filepath.Dir(old), "New"), func() error { return nil }, ops) }()
	<-copying
	read := make(chan bool, 1)
	go func() { read <- s.History.IsPaused() }()
	select {
	case <-read:
	case <-time.After(2 * time.Second):
		t.Error("reading the paused state waited for the copy")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// A copy left after a failed commit isn't recorded, but picking its folder
// later would offer it as data, so the error says where it is.
func TestMoveToReportsACopyItCouldNotRemove(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits don't make a folder read-only on Windows")
	}
	s, old := movableStorage(t)
	target := filepath.Join(filepath.Dir(old), "New")
	err := s.MoveTo(target, func() error {
		readOnly(t, target)
		return errors.New("injected")
	})
	if err == nil || !strings.Contains(err.Error(), "手动删除") || !strings.Contains(err.Error(), target) {
		t.Fatal(err)
	}
	stillWorks(t, s)
}

// A record whose folder couldn't be synced is in place: the restart reads it.
func TestCommitMoveKeepsARecordInPlace(t *testing.T) {
	if err := keepPlaced(&replacedWriteError{errors.New("sync")}); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("disk full")
	if err := keepPlaced(failure); err != failure {
		t.Fatal(err)
	}
	def := t.TempDir()
	loc := dataLocation{Dir: filepath.Join(def, "New"), Previous: def}
	if err := commitMove(def, loc); err != nil {
		t.Fatal(err)
	}
	if got, _ := readDataLocation(def); got != loc {
		t.Fatal(got)
	}
}
