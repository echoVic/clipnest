//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	kernel32     = syscall.NewLazyDLL("kernel32.dll")
	lockFileEx   = kernel32.NewProc("LockFileEx")
	unlockFileEx = kernel32.NewProc("UnlockFileEx")
)

// errorLockViolation is ERROR_LOCK_VIOLATION: another handle holds the lock.
const errorLockViolation = syscall.Errno(33)

func acquireDataLock(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, ".writer.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	var overlapped syscall.Overlapped
	ok, _, err := lockFileEx.Call(f.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if ok == 0 {
		f.Close()
		if errors.Is(err, errorLockViolation) {
			err = errors.Join(errDataLocked, err)
		}
		return nil, fmt.Errorf("数据目录写入锁无法取得：%w", err)
	}
	// Windows may release the lock of a closed handle late, and the instance a
	// move restarts locks right away.
	return func() {
		var overlapped syscall.Overlapped
		_, _, _ = unlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
		_ = f.Close()
	}, nil
}
