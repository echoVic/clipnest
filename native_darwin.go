//go:build darwin

package main

import (
	"errors"
	"github.com/ebitengine/purego"
	"os"
	"unsafe"
)

// Called exclusively on MyGo's main thread. No cgo or AppleScript process.
type nativeBridge struct {
	getClass      func(string) uintptr
	selector      func(string) uintptr
	msg           uintptr
	stringPointer func(uintptr, uintptr) *byte
	trusted       func() bool
	createKey     func(uintptr, uint16, bool) uintptr
	setFlags      func(uintptr, uint64)
	flags         func(uint32) uint64
	keyDown       func(uint32, uint16) bool
	post          func(uint32, uintptr)
	release       func(uintptr)
}
type targetApp struct {
	PID  int
	Name string
}

func openNative() (*nativeBridge, error) {
	n := &nativeBridge{}
	objc, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, err
	}
	if _, err = purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
		return nil, err
	}
	services, err := purego.Dlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, err
	}
	purego.RegisterLibFunc(&n.getClass, objc, "objc_getClass")
	purego.RegisterLibFunc(&n.selector, objc, "sel_registerName")
	n.msg, err = purego.Dlsym(objc, "objc_msgSend")
	if err != nil {
		return nil, err
	}
	purego.RegisterFunc(&n.stringPointer, n.msg)
	purego.RegisterLibFunc(&n.trusted, services, "AXIsProcessTrusted")
	purego.RegisterLibFunc(&n.createKey, services, "CGEventCreateKeyboardEvent")
	purego.RegisterLibFunc(&n.setFlags, services, "CGEventSetFlags")
	purego.RegisterLibFunc(&n.flags, services, "CGEventSourceFlagsState")
	purego.RegisterLibFunc(&n.keyDown, services, "CGEventSourceKeyState")
	purego.RegisterLibFunc(&n.post, services, "CGEventPost")
	purego.RegisterLibFunc(&n.release, services, "CFRelease")
	return n, nil
}
func (n *nativeBridge) send(o uintptr, s string, a ...uintptr) uintptr {
	args := []uintptr{o, n.selector(s)}
	args = append(args, a...)
	v, _, _ := purego.SyscallN(n.msg, args...)
	return v
}
func (n *nativeBridge) pool() func() {
	p := n.send(n.send(n.getClass("NSAutoreleasePool"), "alloc"), "init")
	return func() { n.send(p, "drain") }
}
func (n *nativeBridge) text(o uintptr) string {
	p := n.stringPointer(o, n.selector("UTF8String"))
	if p == nil {
		return ""
	}
	b := []byte{}
	for i := uintptr(0); i < 4096; i++ {
		c := *(*byte)(unsafe.Add(unsafe.Pointer(p), i))
		if c == 0 {
			break
		}
		b = append(b, c)
	}
	return string(b)
}
func (n *nativeBridge) Target() targetApp {
	defer n.pool()()
	w := n.send(n.getClass("NSWorkspace"), "sharedWorkspace")
	a := n.send(w, "frontmostApplication")
	pid := int(n.send(a, "processIdentifier"))
	if pid == os.Getpid() {
		return targetApp{}
	}
	return targetApp{pid, n.text(n.send(a, "localizedName"))}
}
func (n *nativeBridge) Activate(t targetApp) bool {
	defer n.pool()()
	if t.PID <= 0 {
		return false
	}
	a := n.send(n.getClass("NSRunningApplication"), "runningApplicationWithProcessIdentifier:", uintptr(t.PID))
	return a != 0 && n.send(a, "activateWithOptions:", 2) != 0
}
func (n *nativeBridge) Revision() int64 {
	defer n.pool()()
	return int64(n.send(n.send(n.getClass("NSPasteboard"), "generalPasteboard"), "changeCount"))
}
func (n *nativeBridge) Accessible() bool { return n.trusted() }
func (n *nativeBridge) ReadyToPaste(t targetApp) bool {
	return pasteInputReady(t.PID, n.Target().PID, n.flags(1), func(key uint16) bool { return n.keyDown(1, key) })
}
func pasteInputReady(targetPID, frontPID int, flags uint64, keyDown func(uint16) bool) bool {
	if frontPID != targetPID || flags&((1<<17)|(1<<18)|(1<<19)|(1<<20)) != 0 {
		return false
	}
	// Wait for V, Return and the number keys (including the numeric keypad).
	for _, key := range []uint16{9, 36, 18, 19, 20, 21, 23, 22, 26, 28, 25, 76, 83, 84, 85, 86, 87, 88, 89, 91, 92} {
		if keyDown(key) {
			return false
		}
	}
	return true
}
func (n *nativeBridge) Paste(t targetApp) error {
	if !n.Accessible() {
		return errors.New("请先在系统设置中允许 Clipnest 使用辅助功能")
	}
	if !n.ReadyToPaste(t) {
		return errors.New("原应用尚未获得焦点，内容已复制，可手动粘贴")
	}
	down, up := n.createKey(0, 9, true), n.createKey(0, 9, false) // ANSI V.
	if down == 0 || up == 0 {
		if down != 0 {
			n.release(down)
		}
		if up != 0 {
			n.release(up)
		}
		return errors.New("无法发送粘贴按键")
	}
	defer n.release(down)
	defer n.release(up)
	n.setFlags(down, 1<<20)
	n.setFlags(up, 1<<20)
	n.post(0, down)
	n.post(0, up)
	return nil
}
