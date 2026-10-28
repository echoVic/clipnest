package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestRecordPermissionDialogRequiresAnExplicitReadChoice(t *testing.T) {
	for _, access := range []ClipboardAccess{clipboardDefault, clipboardAsk} {
		var choice string
		v := permissionView{purpose: permissionRecord, access: access, choose: func(result string) { choice = result }}
		tt := ui.NewTester(v.draw, 520, 440)
		if choice != "" || !tt.HasText("开始记录") || !tt.HasText("一次读取成功不代表持续授权") {
			t.Fatal("presentation requested access or omitted the one-time consent explanation")
		}
		tt.Key(0, ui.KeyEnter)
		if choice != "cancel" {
			t.Fatal("Enter requested clipboard access", choice)
		}
		choice = ""
		if err := tt.Click("开始记录"); err != nil {
			t.Fatal(err)
		}
		if choice != "request" {
			t.Fatal("explicit recording action was not dispatched", choice)
		}
	}
}

func TestDeniedRecordPermissionDialogOnlyOffersSystemSettings(t *testing.T) {
	var choice string
	v := permissionView{purpose: permissionRecord, access: clipboardDenied, choose: func(result string) { choice = result }}
	tt := ui.NewTester(v.draw, 520, 440)
	if tt.HasText("开始记录") || !tt.HasText("已拒绝") {
		t.Fatal("denied permission exposed a read action")
	}
	if err := tt.Click("记录权限设置…"); err != nil {
		t.Fatal(err)
	}
	if choice != "settings" {
		t.Fatal("settings action was not dispatched")
	}
}

func TestPastePermissionDialogOffersCopyAndEscapeCancels(t *testing.T) {
	var choice string
	v := permissionView{purpose: permissionPaste, choose: func(result string) { choice = result }}
	tt := ui.NewTester(v.draw, 520, 440)
	if !tt.HasText("辅助功能") || tt.HasText("开始记录") {
		t.Fatal("paste permission was mixed with recording consent")
	}
	tt.Key(0, ui.KeyEscape)
	if choice != "cancel" {
		t.Fatal("Escape did not cancel")
	}
	if err := tt.Click("这次仅复制"); err != nil {
		t.Fatal(err)
	}
	if choice != "copy" {
		t.Fatal("copy choice was not dispatched")
	}
}
