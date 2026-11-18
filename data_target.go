package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// moveStagingPrefix names the folder a move copies into before it puts the
// files in place.
const moveStagingPrefix = ".clipnest-move-"

// moveTarget is where the data would go.
type moveTarget struct {
	dir string
	// existing means dir holds Clipnest data already: Clipnest switches to
	// it and copies nothing.
	existing bool
	// synced names the sync service dir is in, or is empty.
	synced string
}

// resolveTarget turns the picked folder into the data directory. A folder
// with Clipnest data, an empty folder, or the default directory is used as
// it is; any other folder gets a Clipnest folder inside, so that the data of
// a whole disk picked doesn't spread over its root. current, which may be
// empty, is the directory in use.
func resolveTarget(picked, current, defaultDir string) (moveTarget, error) {
	dir, err := filepath.Abs(picked)
	if err != nil {
		return moveTarget{}, err
	}
	t := moveTarget{dir: dir}
	if !samePath(dir, defaultDir) {
		if t, err = folderTarget(dir); err != nil {
			return moveTarget{}, err
		}
	} else if t.existing, err = hasClipnestData(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return moveTarget{}, err
	}
	if current != "" && samePath(t.dir, current) {
		return moveTarget{}, errors.New("这已经是当前的数据位置")
	}
	if current != "" && within(t.dir, current) {
		return moveTarget{}, errors.New("不能选当前数据目录里面的文件夹")
	}
	if err := checkWritable(t.dir); err != nil {
		return moveTarget{}, err
	}
	t.synced = syncService(t.dir)
	return t, nil
}

func folderTarget(dir string) (moveTarget, error) {
	found, err := hasClipnestData(dir)
	if err != nil {
		return moveTarget{}, err
	}
	if found {
		return moveTarget{dir: dir, existing: true}, nil
	}
	empty, err := emptyForData(dir)
	if err != nil {
		return moveTarget{}, err
	}
	if empty {
		return moveTarget{dir: dir}, nil
	}
	sub := filepath.Join(dir, "Clipnest")
	if found, err := hasClipnestData(sub); err != nil {
		return moveTarget{}, err
	} else if found {
		return moveTarget{dir: sub, existing: true}, nil
	}
	empty, err = emptyForData(sub)
	if errors.Is(err, fs.ErrNotExist) || err == nil && empty {
		return moveTarget{dir: sub}, nil
	}
	if err != nil {
		return moveTarget{}, err
	}
	return moveTarget{}, fmt.Errorf("「%s」里已有其他文件，请选一个空文件夹", sub)
}

// emptyForData ignores what the system and Clipnest leave in a folder.
func emptyForData(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		switch name := e.Name(); {
		case name == ".DS_Store", strings.EqualFold(name, "desktop.ini"), name == ".writer.lock", strings.HasPrefix(name, moveStagingPrefix):
		default:
			return false, nil
		}
	}
	return true, nil
}

// checkWritable tries dir, or the nearest folder above it that exists,
// which a move creates it in.
func checkWritable(dir string) error {
	for {
		_, err := os.Stat(dir)
		if err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if !errors.Is(err, fs.ErrNotExist) || parent == dir {
			return fmt.Errorf("无法访问这个文件夹：%w", err)
		}
		dir = parent
	}
	f, err := os.CreateTemp(dir, ".clipnest-write-*")
	if err != nil {
		return fmt.Errorf("无法写入这个文件夹：%w", err)
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

// syncService names the sync service whose folder dir is in, by where the
// services keep their folders by default.
func syncService(dir string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	in := func(root string) bool { return samePath(dir, root) || within(dir, root) }
	switch runtime.GOOS {
	case "darwin":
		if in(filepath.Join(home, "Library", "Mobile Documents")) {
			return "iCloud 云盘"
		}
		// File Provider folders: Dropbox, OneDrive-Personal,
		// GoogleDrive-someone@example.com, …
		storage := filepath.Join(home, "Library", "CloudStorage")
		if rel, err := filepath.Rel(storage, dir); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			name, _, _ := strings.Cut(strings.Split(rel, string(filepath.Separator))[0], "-")
			return name
		}
	case "windows":
		for _, key := range []string{"OneDrive", "OneDriveConsumer", "OneDriveCommercial"} {
			if root := os.Getenv(key); root != "" && in(root) {
				return "OneDrive"
			}
		}
	}
	if in(filepath.Join(home, "Dropbox")) {
		return "Dropbox"
	}
	return ""
}
