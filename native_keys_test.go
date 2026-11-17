package main

import (
	"reflect"
	"testing"
)

func TestWindowsPasteKeysAddCtrlOnlyWhenReleased(t *testing.T) {
	held := []keyStroke{{vk: 0x56}, {vk: 0x56, up: true}}
	if got := windowsPasteKeys(true); !reflect.DeepEqual(got, held) {
		t.Fatal("Ctrl is held: send only V", got)
	}
	full := []keyStroke{{vk: 0x11}, {vk: 0x56}, {vk: 0x56, up: true}, {vk: 0x11, up: true}}
	if got := windowsPasteKeys(false); !reflect.DeepEqual(got, full) {
		t.Fatal("Ctrl is up: send Ctrl+V", got)
	}
}
