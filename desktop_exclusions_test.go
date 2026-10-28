package main

import (
	"errors"
	"testing"
	"time"
)

func TestExclusionsDroppedMainDispatchCannotAdmitEdits(t *testing.T) {
	ran := false
	err := runExclusionsMain(func(func()) {}, func() error { ran = true; return nil })
	if err == nil || ran {
		t.Fatal("dropped main dispatch admitted an edit")
	}
	injected := errors.New("edit refused")
	if err := runExclusionsMain(func(work func()) { work() }, func() error { return injected }); !errors.Is(err, injected) {
		t.Fatal("main operation error lost", err)
	}
	if err := runExclusionsMain(func(work func()) { work() }, func() error { ran = true; return nil }); err != nil || !ran {
		t.Fatal("accepted main operation failed", err)
	}
}

func TestApplicationPickerQuitAndCompletionResolveOnlyOnce(t *testing.T) {
	p := &applicationPicker{result: make(chan string, 1)}
	done := make(chan struct{})
	go func() {
		p.resolve("")
		p.resolve("/must/not/be/recorded.app")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("quit and sheet completion blocked each other")
	}
	if path := <-p.result; path != "" || len(p.result) != 0 {
		t.Fatal("cancellation allowed a late selection", path)
	}
}

func TestApplicationPickerCompletionRetiresHandleBeforeDelivery(t *testing.T) {
	p := &applicationPicker{panel: 7, result: make(chan string, 1)}
	released := p.complete("/chosen.app")
	if released != 7 || p.panel != 0 {
		t.Fatal("completed picker retained a native handle")
	}
	if path := <-p.result; path != "/chosen.app" {
		t.Fatal("lost selected application", path)
	}
	if released := p.complete(""); released != 0 || len(p.result) != 0 {
		t.Fatal("duplicate completion retired the same handle twice")
	}
}
