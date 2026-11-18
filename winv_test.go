package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestHotkeyLettersKeepTheOtherLetters(t *testing.T) {
	for _, c := range []struct {
		list, added string
		changed     bool
	}{
		{"", "V", true},
		{"QE", "QEV", true},
		{"QvE", "QvE", false},
		{"V", "V", false},
	} {
		if got, changed := withHotkeyLetter(c.list, 'V'); got != c.added || changed != c.changed {
			t.Errorf("withHotkeyLetter(%q) = %q, %v", c.list, got, changed)
		}
	}
	for _, c := range []struct {
		list, removed string
		changed       bool
	}{
		{"V", "", true},
		{"QVE", "QE", true},
		{"QvEV", "QE", true},
		{"QE", "QE", false},
		{"", "", false},
	} {
		if got, changed := withoutHotkeyLetter(c.list, 'V'); got != c.removed || changed != c.changed {
			t.Errorf("withoutHotkeyLetter(%q) = %q, %v", c.list, got, changed)
		}
	}
}

// fakeWinV records the calls in order; the registry is two fields.
type fakeWinV struct {
	state       winVState
	disabled    string
	calls       []string
	held        bool // Explorer holds Win+V until it restarts with V disabled
	restartErr  error
	registerErr error
	writeErr    error
	disabledErr error
}

func (f *fakeWinV) system() winVSystem {
	return winVSystem{
		state: func() (winVState, error) { return f.state, nil },
		setState: func(s winVState) error {
			f.calls = append(f.calls, "state "+s.String())
			if f.writeErr != nil {
				return f.writeErr
			}
			f.state = s
			return nil
		},
		disabled: func() (string, error) { return f.disabled, nil },
		setDisabled: func(list string) error {
			f.calls = append(f.calls, "disabled "+list)
			if f.writeErr != nil {
				return f.writeErr
			}
			if f.disabledErr != nil {
				return f.disabledErr
			}
			f.disabled = list
			return nil
		},
		restartExplorer: func() error {
			f.calls = append(f.calls, "restart")
			if f.restartErr != nil {
				return f.restartErr
			}
			f.held = !strings.ContainsAny(f.disabled, "vV")
			return nil
		},
		register: func() error {
			f.calls = append(f.calls, "register")
			if f.held {
				return errors.New("hotkey already registered")
			}
			return f.registerErr
		},
		unregister: func() { f.calls = append(f.calls, "unregister") },
	}
}

func TestTurningOnWinVDisablesItInExplorerAndRegistersIt(t *testing.T) {
	f := &fakeWinV{disabled: "Q", held: true}
	if err := turnOnWinV(f.system()); err != nil {
		t.Fatal(err)
	}
	// The state is written first, so an uninstall after a crash still
	// removes the V.
	if want := []string{"state added", "disabled QV", "restart", "register"}; !reflect.DeepEqual(f.calls, want) {
		t.Fatal(f.calls)
	}
	if f.state != winVAdded || f.disabled != "QV" {
		t.Fatal(f.state, f.disabled)
	}
}

func TestTurningOnWinVKeepsAVTheUserDisabledThemselves(t *testing.T) {
	// Explorer started with V disabled, so Win+V is free without a restart.
	f := &fakeWinV{disabled: "V"}
	if err := turnOnWinV(f.system()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"state kept", "register"}; !reflect.DeepEqual(f.calls, want) {
		t.Fatal(f.calls)
	}
	// V was added to the list, but Explorer has not restarted since.
	f = &fakeWinV{disabled: "V", held: true}
	if err := turnOnWinV(f.system()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"state kept", "register", "restart", "register"}; !reflect.DeepEqual(f.calls, want) {
		t.Fatal(f.calls)
	}
}

func TestTurningOnWinVFailures(t *testing.T) {
	f := &fakeWinV{held: true, writeErr: errors.New("access denied")}
	if err := turnOnWinV(f.system()); err == nil || !strings.Contains(err.Error(), "access denied") {
		t.Fatal(err)
	}
	if f.state != winVOff || f.disabled != "" || strings.Contains(strings.Join(f.calls, ","), "restart") {
		t.Fatal("a failed write went on", f.calls)
	}
	f = &fakeWinV{held: true, disabledErr: errors.New("access denied")}
	if err := turnOnWinV(f.system()); err == nil {
		t.Fatal("a failed list write succeeded")
	}
	if f.state != winVOff || strings.Contains(strings.Join(f.calls, ","), "restart") {
		t.Fatal("a failed list write left the state on", f.state, f.calls)
	}
	f = &fakeWinV{held: true, restartErr: errors.New("no")}
	if err := turnOnWinV(f.system()); err == nil || !strings.Contains(err.Error(), "注销") {
		t.Fatal("a failed restart did not say how to finish", err)
	}
	if f.state != winVAdded || f.disabled != "V" {
		t.Fatal("a failed restart undid the setting", f.state, f.disabled)
	}
	f = &fakeWinV{held: true, registerErr: errors.New("taken")}
	if err := turnOnWinV(f.system()); err == nil || !strings.Contains(err.Error(), "Win+V") {
		t.Fatal(err)
	}
}

func TestTurningOffWinVRemovesOnlyTheVClipnestAdded(t *testing.T) {
	f := &fakeWinV{state: winVAdded, disabled: "QVE"}
	if err := turnOffWinV(f.system()); err != nil {
		t.Fatal(err)
	}
	// Explorer can take Win+V back only once Clipnest has let go of it.
	if want := []string{"unregister", "disabled QE", "restart", "state off"}; !reflect.DeepEqual(f.calls, want) {
		t.Fatal(f.calls)
	}
	f = &fakeWinV{state: winVKept, disabled: "V"}
	if err := turnOffWinV(f.system()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"unregister", "state off"}; !reflect.DeepEqual(f.calls, want) || f.disabled != "V" {
		t.Fatal(f.calls, f.disabled)
	}
	// The user removed the V themselves: Explorer already has Win+V or
	// gets it when it next starts.
	f = &fakeWinV{state: winVAdded, disabled: "Q"}
	if err := turnOffWinV(f.system()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"unregister", "state off"}; !reflect.DeepEqual(f.calls, want) {
		t.Fatal(f.calls)
	}
}

func TestTurningOffWinVWhenOffChangesNothing(t *testing.T) {
	f := &fakeWinV{disabled: "V"}
	if err := turnOffWinV(f.system()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"unregister"}; !reflect.DeepEqual(f.calls, want) || f.disabled != "V" {
		t.Fatal(f.calls)
	}
}

func TestTurningOffWinVStillTurnsOffWhenExplorerDoesNotRestart(t *testing.T) {
	f := &fakeWinV{state: winVAdded, disabled: "V", restartErr: errors.New("no")}
	if err := turnOffWinV(f.system()); err == nil || !strings.Contains(err.Error(), "注销") {
		t.Fatal(err)
	}
	if f.state != winVOff || f.disabled != "" {
		t.Fatal(f.state, f.disabled)
	}
}
