//go:build darwin

package main

import (
	"reflect"
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
