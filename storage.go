package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

const maxArchiveBytes = 256 << 20

// decodePrivateJSON consumes exactly one bounded document. Unknown fields are
// permitted for compatible v1 extensions, but trailing documents/garbage are
// not: an incomplete or unrecognized archive must never be silently rewritten.
func decodePrivateJSON(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > maxArchiveBytes {
		return errors.New("数据文件不是普通文件或超过读取上限")
	}
	decoder := json.NewDecoder(io.LimitReader(f, maxArchiveBytes+1))
	if err := decoder.Decode(v); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("数据文件包含多个 JSON 文档")
		}
		return fmt.Errorf("数据文件末尾无效：%w", err)
	}
	return nil
}

// A rename and its directory sync are different commit boundaries. Readers
// already observe new data after rename, even if the directory sync fails.
// This error prevents callers from reporting success or keeping stale memory.
type replacedWriteError struct{ err error }

func (e *replacedWriteError) Error() string {
	return "文件已更新，但持久化确认失败：" + e.err.Error()
}
func (e *replacedWriteError) Unwrap() error { return e.err }
func writeReplaced(err error) bool          { var e *replacedWriteError; return errors.As(err, &e) }

type jsonWriteOps struct {
	syncFile func(*os.File) error
	rename   func(string, string) error
	syncDir  func(string) error
}

func syncDirectory(path string) error {
	// Windows does not expose directory fsync through os.File; its guarantee
	// remains the existing file sync + rename, not POSIX directory durability.
	if runtime.GOOS == "windows" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// v1 stays v1: no implicit migration, sidecar checksum or recovery generation
// can bring deleted private clipboard content back. The directory lock used by
// main excludes cooperating application instances; it is not a security ACL.
func writePrivateJSON(path string, v any) error {
	return writePrivateJSONWith(path, v, jsonWriteOps{func(f *os.File) error { return f.Sync() }, os.Rename, syncDirectory})
}

func writePrivateJSONWith(path string, v any, ops jsonWriteOps) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		err = json.NewEncoder(f).Encode(v)
	}
	if err == nil {
		err = ops.syncFile(f)
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := ops.rename(name, path); err != nil {
		return err
	}
	if err := ops.syncDir(dir); err != nil {
		return &replacedWriteError{err}
	}
	return nil
}
