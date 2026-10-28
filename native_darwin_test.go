//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// Guards the actual native preflight without touching the user's clipboard,
// permissions or frontmost app. The pure decision is shared by production.
func TestNativePasteInputGuard(t *testing.T) {
	for _, flag := range []uint64{1 << 17, 1 << 18, 1 << 19, 1 << 20} {
		if pasteInputReady(7, 7, flag, func(uint16) bool { return false }) {
			t.Fatal("modifier was ignored", flag)
		}
	}
	if pasteInputReady(7, 8, 0, func(uint16) bool { return false }) {
		t.Fatal("different app accepted")
	}
	for _, held := range []uint16{9, 36, 18, 19, 20, 21, 23, 22, 26, 28, 25, 76, 83, 84, 85, 86, 87, 88, 89, 91, 92} {
		if pasteInputReady(7, 7, 0, func(key uint16) bool { return key == held }) {
			t.Fatal("selection key was ignored", held)
		}
	}
	keys := []uint16{}
	if !pasteInputReady(7, 7, 0, func(key uint16) bool { keys = append(keys, key); return false }) {
		t.Fatal("ready target refused")
	}
	if !reflect.DeepEqual(keys, []uint16{9, 36, 18, 19, 20, 21, 23, 22, 26, 28, 25, 76, 83, 84, 85, 86, 87, 88, 89, 91, 92}) {
		t.Fatal("guard key coverage changed", keys)
	}
}

func TestNativePastePermissionStopsBeforePosting(t *testing.T) {
	n := &nativeBridge{trusted: func() bool { return false }, post: func(uint32, uintptr) { t.Fatal("posted without permission") }}
	if err := n.Paste(targetApp{PID: 7}); err == nil {
		t.Fatal("permission error hidden")
	}
}

// Exercise the real Objective-C bridge using policy metadata only. This never
// reads/writes clipboard contents or changes the app's system permissions.
func TestNativeClipboardAccessPolicy(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	n, err := openNative()
	if err != nil {
		t.Fatal(err)
	}
	access := n.ClipboardAccess()
	switch access {
	case clipboardUnsupported, clipboardDefault, clipboardAsk, clipboardAllowed, clipboardDenied:
		t.Log("native clipboard access:", access)
	default:
		t.Fatalf("unexpected native clipboard policy: %q", access)
	}
}

func TestNativeSelectedApplicationMetadataWithoutLaunching(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	n, err := openNative()
	if err != nil {
		t.Fatal(err)
	}
	app, err := n.Application("/System/Library/CoreServices/Finder.app")
	if err != nil || app.BundleID != "com.apple.finder" || app.Name == "" {
		t.Fatal("Finder bundle metadata unavailable", app, err)
	}
	dir := t.TempDir()
	contents := filepath.Join(dir, "Invalid.app", "Contents")
	if err := os.MkdirAll(contents, 0700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleName</key><string>Invalid</string></dict></plist>`), 0600)
	for _, path := range []string{filepath.Dir(contents), dir, "/no/such/App.app"} {
		if _, err := n.Application(path); err == nil {
			t.Fatal("accepted invalid application bundle", path)
		}
	}
}

func TestNativeApplicationPickerAcceptsApplicationBundles(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	n, err := openNative()
	if err != nil {
		t.Fatal(err)
	}
	defer n.pool()()
	uti := n.applicationBundleType()
	if got := n.text(n.send(uti, "identifier")); got != "com.apple.application-bundle" {
		t.Fatal("picker rejects macOS .app bundles:", got)
	}
}

func TestNativeSelectedApplicationRejectsNonStringIdentifier(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	n, err := openNative()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "Numeric.app")
	if err := os.MkdirAll(filepath.Join(path, "Contents"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "Contents", "Info.plist"), []byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><integer>7</integer><key>CFBundleName</key><string>Numeric</string></dict></plist>`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := n.Application(path); err == nil {
		t.Fatal("accepted a non-string bundle identifier")
	}
}

func TestNativeCompletedApplicationPickerCannotBeCanceledAgain(t *testing.T) {
	// No native functions are installed: a completed handle must return before
	// creating an autorelease pool or sending an Objective-C message.
	n := &nativeBridge{}
	p := &applicationPicker{result: make(chan string, 1)}
	p.resolve("/chosen.app")
	n.closeApplicationPicker(p)
	if path := <-p.result; path != "/chosen.app" {
		t.Fatal("late quit changed a completed selection", path)
	}
}
