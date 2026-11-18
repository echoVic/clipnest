package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// openedData is the storage of this launch.
type openedData struct {
	store  *Storage
	unlock func()
	dir    string
	// notice says what became of a move, for the preferences.
	notice string
}

// openDataDir locks and opens choice.dir. After a move it commits the new
// place and then deletes the old files; when the new place won't open, it
// goes back to the old one, which still has everything. The error is for a
// directory that can't be used with no move to go back from.
func openDataDir(choice dataDirChoice, defaultDir string) (openedData, error) {
	err := choice.problem
	copied := err == nil // the copy may be deleted, unless another instance uses it
	if err == nil {
		var d openedData
		if d, copied, err = lockAndOpen(choice.dir); err == nil {
			if choice.previous != "" {
				d.notice = finishMove(d.dir, choice.previous, defaultDir)
			}
			return d, nil
		}
	}
	if choice.previous == "" {
		return openedData{}, err
	}
	if werr := writeDataLocation(defaultDir, dataLocation{Dir: choice.previous}); werr != nil {
		return openedData{}, fmt.Errorf("新位置无法打开，也未能改回原位置：%w", errors.Join(err, werr))
	}
	if copied {
		_ = removeClipnestFiles(choice.dir, samePath(choice.dir, defaultDir))
	}
	d, _, perr := lockAndOpen(choice.previous)
	if perr != nil {
		return openedData{}, perr
	}
	d.notice = fmt.Sprintf("新位置「%s」无法打开，已改回原位置：%v", choice.dir, err)
	return d, nil
}

// lockAndOpen reports whether it got the lock, past which a failure is the
// data's, not another instance's.
func lockAndOpen(dir string) (openedData, bool, error) {
	unlock, err := acquireDataLock(dir)
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

func finishMove(dir, previous, defaultDir string) string {
	if err := writeDataLocation(defaultDir, dataLocation{Dir: dir}); err != nil {
		return fmt.Sprintf("数据已移到「%s」，但位置记录未能更新，旧位置的数据暂时保留：%v", dir, err)
	}
	if err := removeClipnestFiles(previous, samePath(previous, defaultDir)); err != nil {
		return fmt.Sprintf("数据已移到「%s」。旧位置的文件没能删除，可以手动删除：%s（%v）", dir, previous, err)
	}
	return fmt.Sprintf("数据已移到「%s」", dir)
}

// removeClipnestFiles deletes Clipnest's own files in dir and leaves anything
// else. Unless keepDir, it then deletes dir if that left it empty. The
// default directory is kept: it has the location record and the updater's
// files.
func removeClipnestFiles(dir string, keepDir bool) error {
	names := []string{databaseName, databaseName + "-wal", databaseName + "-shm", databaseName + "-journal", "history.json", "phrases.json"}
	names = append(names, movedSettings...)
	names = append(names, ".writer.lock")
	var errs []error
	for _, name := range names {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
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
	d, _, err := lockAndOpen(dir)
	if err != nil {
		return err
	}
	defer d.unlock()
	return d.store.Close()
}
