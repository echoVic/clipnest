//go:build darwin || linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Lock the directory, rather than individual JSON files that are replaced by
// rename. The kernel releases the advisory lock even after an unclean exit.
func acquireDataLock(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, ".writer.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("数据目录正被另一个 Clipnest 实例使用，已停止启动：%w", err)
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
