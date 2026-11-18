//go:build windows

package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// winVKeys are the keys under HKCU that the switch reads and writes.
type winVKeys struct {
	advanced string // Explorer's, with DisabledHotkeys
	clipnest string // Clipnest's, with WinVHotkey
}

const winVValue = "WinVHotkey"

// The uninstaller restores Win+V without the data directory, which can be
// anywhere with --data-dir, so the state lives in the registry.
var systemWinVKeys = winVKeys{
	advanced: `Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`,
	clipnest: `Software\Clipnest`,
}

func systemWinV() winVSystem { return systemWinVKeys.system(restartExplorer) }

func (k winVKeys) system(restart func() error) winVSystem {
	return winVSystem{
		state: k.state, setState: k.setState,
		disabled: k.disabled, setDisabled: k.setDisabled,
		restartExplorer: restart,
		register:        func() error { return nil },
		unregister:      func() {},
	}
}

// disabled refuses a DisabledHotkeys that is not a string, rather than
// replacing what something else wrote there.
func (k winVKeys) disabled() (string, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, k.advanced, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer key.Close()
	list, _, err := key.GetStringValue("DisabledHotkeys")
	if errors.Is(err, registry.ErrNotExist) {
		return "", nil
	}
	return list, err
}

func (k winVKeys) setDisabled(list string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, k.advanced, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	if list == "" {
		if err := key.DeleteValue("DisabledHotkeys"); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	return key.SetStringValue("DisabledHotkeys", list)
}

func (k winVKeys) state() (winVState, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, k.clipnest, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return winVOff, nil
	}
	if err != nil {
		return winVOff, err
	}
	defer key.Close()
	v, _, err := key.GetIntegerValue(winVValue)
	if errors.Is(err, registry.ErrNotExist) {
		return winVOff, nil
	}
	if err != nil {
		return winVOff, err
	}
	if s := winVState(v); s == winVAdded || s == winVKept {
		return s, nil
	}
	return winVOff, nil
}

// setState of off deletes the value, and Clipnest's key once it is empty.
func (k winVKeys) setState(s winVState) error {
	if s != winVOff {
		key, _, err := registry.CreateKey(registry.CURRENT_USER, k.clipnest, registry.SET_VALUE)
		if err != nil {
			return err
		}
		defer key.Close()
		return key.SetDWordValue(winVValue, uint32(s))
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, k.clipnest, registry.SET_VALUE|registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := key.DeleteValue(winVValue); err != nil && !errors.Is(err, registry.ErrNotExist) {
		key.Close()
		return err
	}
	info, err := key.Stat()
	key.Close()
	if err == nil && info.SubKeyCount == 0 && info.ValueCount == 0 {
		_ = registry.DeleteKey(registry.CURRENT_USER, k.clipnest)
	}
	return nil
}

var (
	rstrtmgr                = windows.NewLazySystemDLL("rstrtmgr.dll")
	procRmStartSession      = rstrtmgr.NewProc("RmStartSession")
	procRmRegisterResources = rstrtmgr.NewProc("RmRegisterResources")
	procRmShutdown          = rstrtmgr.NewProc("RmShutdown")
	procRmRestart           = rstrtmgr.NewProc("RmRestart")
	procRmEndSession        = rstrtmgr.NewProc("RmEndSession")
	procGetShellWindow      = windows.NewLazySystemDLL("user32.dll").NewProc("GetShellWindow")
)

// rmUniqueProcess is RM_UNIQUE_PROCESS.
type rmUniqueProcess struct {
	pid   uint32
	start windows.Filetime
}

const (
	rmSessionKeyLen = 32 // CCH_RM_SESSION_KEY
	rmForceShutdown = 1
)

// shellProcess is this session's Explorer: the process of its desktop
// window. Explorers of other sessions are left alone.
func shellProcess() (rmUniqueProcess, bool) {
	hwnd, _, _ := procGetShellWindow.Call()
	if hwnd == 0 {
		return rmUniqueProcess{}, false
	}
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(windows.HWND(hwnd), &pid); err != nil || pid == 0 {
		return rmUniqueProcess{}, false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return rmUniqueProcess{}, false
	}
	defer windows.CloseHandle(h)
	var start, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &start, &exit, &kernel, &user); err != nil {
		return rmUniqueProcess{}, false
	}
	return rmUniqueProcess{pid, start}, true
}

func rmError(name string, r uintptr) error {
	return fmt.Errorf("%s: %w", name, syscall.Errno(r))
}

// restartExplorer restarts Explorer from a Clipnest process that has the
// user's environment as Windows builds it at sign-in. The new Explorer
// inherits that process's environment, and every app started from the shell
// inherits Explorer's; Clipnest's own can be older than Explorer's, which
// takes in changed variables as they change, or carry an installer's.
func restartExplorer() error {
	cmd, err := explorerRestarter()
	if err != nil {
		return err
	}
	return cmd.Run()
}

func explorerRestarter() (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	// CreateEnvironmentBlock leaves out USERNAME and USERDOMAIN without
	// duplicate and impersonate access, which the pseudo-token lacks.
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY|windows.TOKEN_DUPLICATE|windows.TOKEN_IMPERSONATE, &token); err != nil {
		return nil, err
	}
	defer token.Close()
	env, err := token.Environ(false)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, "--"+restartExplorerFlag)
	cmd.Env = env
	return cmd, nil
}

// restartExplorerHere restarts this session's Explorer with Restart Manager,
// as installers do, so that it reopens the folder windows it had. Explorer
// reads DisabledHotkeys only when it starts.
func restartExplorerHere() error {
	shell, ok := shellProcess()
	if !ok {
		// The Explorer that starts next reads the new letters.
		return nil
	}
	var session uint32
	var key [rmSessionKeyLen + 1]uint16
	if r, _, _ := procRmStartSession.Call(uintptr(unsafe.Pointer(&session)), 0, uintptr(unsafe.Pointer(&key[0]))); r != 0 {
		return rmError("RmStartSession", r)
	}
	defer procRmEndSession.Call(uintptr(session))
	if r, _, _ := procRmRegisterResources.Call(uintptr(session), 0, 0, 1, uintptr(unsafe.Pointer(&shell)), 0, 0); r != 0 {
		return rmError("RmRegisterResources", r)
	}
	if r, _, _ := procRmShutdown.Call(uintptr(session), rmForceShutdown, 0); r != 0 {
		return rmError("RmShutdown", r)
	}
	if r, _, _ := procRmRestart.Call(uintptr(session), 0, 0); r != 0 {
		log.Print("资源管理器未能由重启管理器重启：", rmError("RmRestart", r))
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if next, ok := shellProcess(); ok && next.pid != shell.pid {
			return nil
		}
	}
	// Neither Restart Manager nor Windows started it again.
	dir, err := windows.GetWindowsDirectory()
	if err != nil {
		return err
	}
	cmd := exec.Command(filepath.Join(dir, "explorer.exe"))
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
