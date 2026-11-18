package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// openedData is the storage of this launch.
type openedData struct {
	store  *Storage
	unlock func()
	dir    string
	// notice says what became of a move, for the preferences.
	notice string
}

// errDataLocked is the data lock held by another instance.
var errDataLocked = errors.New("数据目录已被锁定")

// lockWait is how long a launch after a move waits for the data lock: the
// instance that moved the data restarts Clipnest before it lets go of the
// old place.
var lockWait = 5 * time.Second

// acquireDataLockWaiting waits up to lockWait for another instance to let go.
func acquireDataLockWaiting(dir string) (func(), error) {
	deadline := time.Now().Add(lockWait)
	for {
		unlock, err := acquireDataLock(dir)
		if err == nil || !errors.Is(err, errDataLocked) || time.Now().After(deadline) {
			return unlock, err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// openDataDir locks and opens choice.dir. After a move it commits the new
// place and then deletes the old files; when the new place won't open, it
// goes back to the old one, which still has everything. The error is for a
// directory that can't be used with no move to go back from.
//
// No launch runs on a moved copy before the move is committed, so until then
// the copy holds nothing the old place lacks, and going back may delete it.
func openDataDir(choice dataDirChoice, defaultDir string) (openedData, error) {
	if choice.previous == "" {
		if choice.problem != nil {
			return openedData{}, choice.problem
		}
		d, _, err := lockAndOpen(choice.dir, false)
		return d, err
	}
	err := choice.problem
	locked := false // the copy is not another instance's
	if err == nil {
		var d openedData
		if d, locked, err = lockAndOpen(choice.dir, true); err == nil {
			if cerr := writeDataLocation(defaultDir, dataLocation{Dir: choice.dir}); cerr != nil {
				d.store.Close()
				d.unlock()
				return goBack(choice, fmt.Errorf("位置记录无法更新：%w", cerr), true, defaultDir)
			}
			d.notice = finishMove(d.dir, choice.previous, defaultDir)
			return d, nil
		}
	}
	return goBack(choice, err, locked, defaultDir)
}

// goBack opens the place a move came from, deletes the copy unless another
// instance uses it, and records the old place again.
func goBack(choice dataDirChoice, cause error, deleteCopy bool, defaultDir string) (openedData, error) {
	previous := choice.previous
	failed := func(err error) (openedData, error) {
		return openedData{}, fmt.Errorf("新位置无法打开（%v），原位置「%s」也无法使用：%w", cause, previous, err)
	}
	if !samePath(previous, defaultDir) {
		if err := checkDataDir(previous); err != nil {
			return failed(err)
		}
	}
	d, _, err := lockAndOpen(previous, true)
	if err != nil {
		return failed(err)
	}
	copyGone := false
	if deleteCopy {
		copyGone = removeClipnestFiles(choice.dir, samePath(choice.dir, defaultDir)) == nil
	}
	d.notice = fmt.Sprintf("新位置「%s」无法打开，已改回原位置：%v", choice.dir, cause)
	if werr := writeDataLocation(defaultDir, dataLocation{Dir: previous}); werr != nil {
		// With the record unchanged, a later launch would take up a copy
		// still there and delete what this one writes to the old place.
		if !copyGone {
			d.store.Close()
			d.unlock()
			return openedData{}, fmt.Errorf("新位置无法打开，也未能改回原位置：%w", errors.Join(cause, werr))
		}
		d.notice += fmt.Sprintf("。位置记录未能更新：%v", werr)
	}
	return d, nil
}

// lockAndOpen reports whether it got the lock, past which a failure is the
// data's, not another instance's. With wait, it waits for an instance that
// is exiting.
func lockAndOpen(dir string, wait bool) (openedData, bool, error) {
	acquire := acquireDataLock
	if wait {
		acquire = acquireDataLockWaiting
	}
	unlock, err := acquire(dir)
	if err != nil {
		return openedData{}, false, err
	}
	store, err := openStorage(dir)
	if err != nil {
		unlock()
		return openedData{}, true, err
	}
	return openedData{store: store, unlock: unlock, dir: dir}, true, nil
}

// finishMove deletes the old files once the move is committed.
func finishMove(dir, previous, defaultDir string) string {
	if _, err := os.Stat(previous); err != nil {
		return fmt.Sprintf("数据已移到「%s」。原位置「%s」现在无法访问，那里的数据没有删除，可以之后手动删除（%v）", dir, previous, err)
	}
	if err := removeClipnestFiles(previous, samePath(previous, defaultDir)); err != nil {
		return fmt.Sprintf("数据已移到「%s」。旧位置的文件没能删除，可以手动删除：%s（%v）", dir, previous, err)
	}
	return fmt.Sprintf("数据已移到「%s」", dir)
}

// removeClipnestFiles deletes Clipnest's own files in dir and leaves anything
// else. Unless keepDir, it then deletes dir if that left it empty. The
// default directory is kept: it has the location record and the updater's
// files. It waits for the data lock, so that nothing still has the files
// open, and a missing dir has nothing to delete.
func removeClipnestFiles(dir string, keepDir bool) error {
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	unlock, err := acquireDataLockWaiting(dir)
	if err != nil {
		return err
	}
	names := []string{databaseName, databaseName + "-wal", databaseName + "-shm", databaseName + "-journal", "history.json", "phrases.json"}
	names = append(names, movedSettings...)
	var errs []error
	remove := func(name string) {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	for _, name := range names {
		remove(name)
	}
	// Windows can't delete a file that is open, and the lock holds it open.
	unlock()
	remove(".writer.lock")
	if err := errors.Join(errs...); err != nil {
		return err
	}
	if !keepDir {
		_ = os.Remove(dir) // only when empty
	}
	return syncDirectory(filepath.Dir(dir))
}

// prepareDataDir opens dir once before Clipnest records it: data that won't
// open is never recorded, and an empty folder gets its database, so that a
// recorded directory always has data.
func prepareDataDir(dir string) error {
	d, _, err := lockAndOpen(dir, false)
	if err != nil {
		return err
	}
	defer d.unlock()
	return d.store.Close()
}
