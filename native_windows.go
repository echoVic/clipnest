//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

type targetApp struct {
	PID      int
	Name     string
	BundleID string
	hwnd     uintptr
	threadID uint32
}

type nativeBridge struct {
	user32   *syscall.LazyDLL
	kernel32 *syscall.LazyDLL
}

// Password managers and other apps that own sensitive copies publish this
// clipboard format; Windows' own clipboard history honors it too.
var concealedFormats = []string{"ExcludeClipboardContentFromMonitorProcessing"}

func openNative() (*nativeBridge, error) {
	return &nativeBridge{
		user32:   syscall.NewLazyDLL("user32.dll"),
		kernel32: syscall.NewLazyDLL("kernel32.dll"),
	}, nil
}

// Windows has no per-app clipboard consent, so reading never prompts.
func (*nativeBridge) ClipboardAccess() ClipboardAccess { return clipboardAllowed }

// SendInput needs no accessibility grant; pasting into elevated apps can
// still be blocked by UIPI, which ReadyToPaste's foreground check detects.
func (*nativeBridge) Accessible() bool { return true }

func (*nativeBridge) autoPaste() bool { return true }
func manualPasteHint() string         { return "Ctrl+V" }

// Revision wraps GetClipboardSequenceNumber, which increments on every
// clipboard ownership change. Enumerating formats is therefore only needed
// after an actual copy.
func (n *nativeBridge) Revision() int64 {
	v, _, _ := n.user32.NewProc("GetClipboardSequenceNumber").Call()
	return int64(v)
}

// Target names the foreground app's process, the source of the next paste.
// Our own panel and tray windows never become the target.
func (n *nativeBridge) Target() targetApp {
	hwnd, _, _ := n.user32.NewProc("GetForegroundWindow").Call()
	if hwnd == 0 {
		return targetApp{}
	}
	var pid uint32
	thread, _, _ := n.user32.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 || int(pid) == os.Getpid() {
		return targetApp{}
	}
	name := processName(n.kernel32, pid)
	if name == "" {
		return targetApp{}
	}
	return targetApp{PID: int(pid), Name: name, hwnd: hwnd, threadID: uint32(thread)}
}

func processName(kernel32 *syscall.LazyDLL, pid uint32) string {
	// PROCESS_QUERY_LIMITED_INFORMATION works without elevated rights.
	h, _, _ := kernel32.NewProc("OpenProcess").Call(0x1000, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer kernel32.NewProc("CloseHandle").Call(h)
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	r, _, _ := kernel32.NewProc("QueryFullProcessImageNameW").Call(
		h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return ""
	}
	return strings.TrimSuffix(filepath.Base(syscall.UTF16ToString(buf[:size])), ".exe")
}

// acquireForeground attaches the calling thread's input queue to the current
// foreground thread. Right after auto-paste hands the foreground to another
// app through the same AttachThreadInput trick, Windows denies the panel's
// SetForegroundWindow for a while; the shared input queue lets it regain it
// immediately, so the global shortcut can reopen the panel after pasting.
func (n *nativeBridge) acquireForeground() func() {
	fg, _, _ := n.user32.NewProc("GetForegroundWindow").Call()
	var fgPID uint32
	fgThread, _, _ := n.user32.NewProc("GetWindowThreadProcessId").Call(fg, uintptr(unsafe.Pointer(&fgPID)))
	curThread, _, _ := n.kernel32.NewProc("GetCurrentThreadId").Call()
	if fgThread == 0 || fgThread == curThread {
		return func() {}
	}
	attach := n.user32.NewProc("AttachThreadInput")
	attach.Call(curThread, fgThread, 1)
	return func() { attach.Call(curThread, fgThread, 0) }
}

// Activate brings the window the panel opened from back to the foreground.
// AttachThreadInput satisfies SetForegroundWindow's foreground lock.
func (n *nativeBridge) Activate(t targetApp) bool {
	if t.hwnd == 0 {
		return false
	}
	if iconic, _, _ := n.user32.NewProc("IsIconic").Call(t.hwnd); iconic != 0 {
		n.user32.NewProc("ShowWindow").Call(t.hwnd, 9) // SW_RESTORE
	}
	fg, _, _ := n.user32.NewProc("GetForegroundWindow").Call()
	var fgPID uint32
	fgThread, _, _ := n.user32.NewProc("GetWindowThreadProcessId").Call(fg, uintptr(unsafe.Pointer(&fgPID)))
	curThread, _, _ := n.kernel32.NewProc("GetCurrentThreadId").Call()
	attach := n.user32.NewProc("AttachThreadInput")
	targetThread := uintptr(t.threadID)
	if fgThread != targetThread {
		attach.Call(curThread, targetThread, 1)
		if fgThread != curThread {
			attach.Call(fgThread, targetThread, 1)
		}
	}
	n.user32.NewProc("BringWindowToTop").Call(t.hwnd)
	n.user32.NewProc("SetForegroundWindow").Call(t.hwnd)
	if fgThread != targetThread {
		attach.Call(curThread, targetThread, 0)
		if fgThread != curThread {
			attach.Call(fgThread, targetThread, 0)
		}
	}
	now, _, _ := n.user32.NewProc("GetForegroundWindow").Call()
	return now == t.hwnd
}

// Keys that must be released before the synthetic paste, mirroring the
// macOS guard: the opening chord's modifiers, and keys that choose a row.
var guardedPasteKeys = []uintptr{
	0x10, 0x11, 0x12, 0x5B, 0x5C, // Shift, Ctrl, Alt, LWin, RWin
	0x56,                                                       // V
	0x0D,                                                       // Return
	0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, // 0-9
	0x60, 0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69, // Numpad 0-9
}

func (n *nativeBridge) ReadyToPaste(t targetApp) bool {
	fg, _, _ := n.user32.NewProc("GetForegroundWindow").Call()
	if t.hwnd == 0 || fg != t.hwnd {
		return false
	}
	getAsync := n.user32.NewProc("GetAsyncKeyState")
	for _, vk := range guardedPasteKeys {
		if state, _, _ := getAsync.Call(vk); state&0x8000 != 0 {
			return false
		}
	}
	return true
}

// Windows INPUT / KEYBDINPUT for SendInput (amd64 and arm64 layout).
const (
	inputKeyboard  = 1
	keyeventfKeyUp = 0x0002
	vkControl      = 0x11
	vkV            = 0x56
)

type keyInput struct {
	vk        uint16
	scan      uint16
	flags     uint32
	time      uint32
	extraInfo uintptr
}

type inputRecord struct {
	inputType uint32
	_         uint32
	ki        keyInput
	_         [8]byte // pad the union to MOUSEINPUT's size
}

func (n *nativeBridge) Paste(t targetApp) error {
	if !n.ReadyToPaste(t) {
		return errors.New("原应用尚未获得焦点，内容已复制，可手动粘贴")
	}
	down := func(vk uint16) keyInput { return keyInput{vk: vk} }
	up := func(vk uint16) keyInput { return keyInput{vk: vk, flags: keyeventfKeyUp} }
	records := [4]inputRecord{
		{inputType: inputKeyboard, ki: down(vkControl)},
		{inputType: inputKeyboard, ki: down(vkV)},
		{inputType: inputKeyboard, ki: up(vkV)},
		{inputType: inputKeyboard, ki: up(vkControl)},
	}
	sent, _, _ := n.user32.NewProc("SendInput").Call(
		4, uintptr(unsafe.Pointer(&records[0])), unsafe.Sizeof(records[0]))
	if sent != 4 {
		return errors.New("无法发送粘贴按键，内容已复制，可手动粘贴")
	}
	return nil
}

func (*nativeBridge) Application(string) (ExcludedApp, error) {
	return ExcludedApp{}, errors.New("排除应用仅适用于 macOS")
}

func (*nativeBridge) openApplicationPicker(uintptr) (*applicationPicker, error) {
	return nil, errors.New("排除应用仅适用于 macOS")
}

func (*nativeBridge) closeApplicationPicker(*applicationPicker) {}
