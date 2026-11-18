package main

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// defaultDataDirName names the default data directory under the user config
// directory. Test builds link another name with
// MYGO_LDFLAGS="-X main.defaultDataDirName=Clipnest.verify", so that neither
// they nor the instance a move relaunches touch the real data.
var defaultDataDirName = "Clipnest"

// dataLocationName is the record, in the default directory, of a data
// directory chosen in the preferences. It can't live in the chosen
// directory, which it is how to find.
const dataLocationName = "data-location.json"

func defaultDataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, defaultDataDirName), nil
}

// dataLocation is where the data is. Previous is where a move came from,
// until the new place has opened and the move is committed: a launch that
// can't open Dir goes back to it.
type dataLocation struct {
	Dir      string `json:"dir"`
	Previous string `json:"previous,omitempty"`
}

// readDataLocation reports the zero location when there is no record.
func readDataLocation(defaultDir string) (dataLocation, error) {
	var loc dataLocation
	err := decodePrivateJSON(filepath.Join(defaultDir, dataLocationName), &loc)
	if errors.Is(err, fs.ErrNotExist) {
		return dataLocation{}, nil
	}
	if err != nil {
		return dataLocation{}, err
	}
	for _, dir := range []string{loc.Dir, loc.Previous} {
		if dir != "" && !filepath.IsAbs(dir) {
			return dataLocation{}, errors.New("记录的路径不是绝对路径")
		}
	}
	return loc, nil
}

// writeDataLocation removes the record for the default directory with no
// move to finish, which is what no record means.
func writeDataLocation(defaultDir string, loc dataLocation) error {
	path := filepath.Join(defaultDir, dataLocationName)
	if loc.Previous == "" && (loc.Dir == "" || samePath(loc.Dir, defaultDir)) {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return syncDirectory(defaultDir)
	}
	return writePrivateJSON(path, loc)
}

// dataDirChoice is where this launch keeps its data.
type dataDirChoice struct {
	dir      string
	previous string // where a move came from, until it is committed
	fixed    bool   // chosen with --data-dir, not in the preferences
	// problem is why dir can't be used. Clipnest then neither creates it nor
	// writes elsewhere, and asks.
	problem error
}

// chooseDataDir takes --data-dir, else the record, else the default
// directory. Only the default may be missing or empty: a recorded directory
// always holds data, so a missing one is a disk that isn't connected or a
// folder that moved, not a first launch.
func chooseDataDir(flagDir, defaultDir string) dataDirChoice {
	if flagDir != "" {
		if abs, err := filepath.Abs(flagDir); err == nil {
			flagDir = abs
		}
		return dataDirChoice{dir: flagDir, fixed: true}
	}
	loc, err := readDataLocation(defaultDir)
	if err != nil {
		return dataDirChoice{problem: fmt.Errorf("数据位置记录无法读取：%w", err)}
	}
	if loc.Dir == "" || samePath(loc.Dir, defaultDir) {
		return dataDirChoice{dir: defaultDir, previous: loc.Previous}
	}
	return dataDirChoice{dir: loc.Dir, previous: loc.Previous, problem: checkDataDir(loc.Dir)}
}

// checkDataDir says why dir can't be a recorded data directory, or nil.
func checkDataDir(dir string) error {
	info, err := os.Stat(dir)
	if err == nil && !info.IsDir() {
		return errors.New("这个路径不是文件夹")
	}
	found := false
	if err == nil {
		found, err = hasClipnestData(dir)
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return errors.New("找不到这个文件夹，外接磁盘可能没有连接，或文件夹已被移动")
	case errors.Is(err, fs.ErrPermission):
		if runtime.GOOS == "darwin" {
			return errors.New("没有权限访问这个文件夹，可以在「系统设置 → 隐私与安全性 → 文件与文件夹」中允许 Clipnest")
		}
		return errors.New("没有权限访问这个文件夹")
	case err != nil:
		return err
	case !found:
		return errors.New("这里找不到 Clipnest 的数据")
	}
	return nil
}

// hasClipnestData reports whether dir has a Clipnest database. JSON files
// from before SQLite don't count: names like phrases.json are common, and
// data that isn't in SQLite can't move. Every directory Clipnest records has
// a database.
func hasClipnestData(dir string) (bool, error) {
	_, err := os.Lstat(filepath.Join(dir, databaseName))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// pathKey compares paths the way the file system does by default: macOS and
// Windows ignore case.
func pathKey(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

func samePath(a, b string) bool { return pathKey(a) == pathKey(b) }

// within reports whether path is inside dir, and not dir itself.
func within(path, dir string) bool {
	rel, err := filepath.Rel(pathKey(dir), pathKey(path))
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// commitMove records a finished copy as the data directory.
func commitMove(defaultDir string, loc dataLocation) error {
	return keepPlaced(writeDataLocation(defaultDir, loc))
}

// keepPlaced lets a record stand that is in place but whose folder couldn't
// be synced: the restart reads it, and the launch commits it again. Undoing
// the move now would leave the record naming a copy.
func keepPlaced(err error) error {
	var placed *replacedWriteError
	if errors.As(err, &placed) {
		log.Print("位置记录已写入，但文件夹未能同步：", err)
		return nil
	}
	return err
}
