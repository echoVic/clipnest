package main

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// Explorer registers Win+V when it starts, unless V is in the letters of
// DisabledHotkeys. Clipnest adds the V, restarts Explorer and registers
// Win+V itself; turning the switch off or uninstalling takes the V out and
// restarts Explorer again, so that it takes Win+V back.
const winVAccelerator = "Super+V"

// winVState is what Clipnest remembers about the V, so that turning off
// removes only a V that Clipnest added.
type winVState int

const (
	winVOff winVState = iota
	// winVAdded: Clipnest added the V to DisabledHotkeys.
	winVAdded
	// winVKept: the V was already there, and stays when turning off.
	winVKept
)

func (s winVState) String() string {
	switch s {
	case winVAdded:
		return "added"
	case winVKept:
		return "kept"
	default:
		return "off"
	}
}

// winVSystem is what turning Win+V on and off changes: the remembered state,
// the DisabledHotkeys letters ("" deletes the value), Explorer, and the
// hotkey Clipnest registers.
type winVSystem struct {
	state           func() (winVState, error)
	setState        func(winVState) error
	disabled        func() (string, error)
	setDisabled     func(string) error
	restartExplorer func() error
	register        func() error
	unregister      func()
}

func winVSettingsError(err error) error {
	return fmt.Errorf("无法修改 Win+V 的系统设置：%w", err)
}

// turnOnWinV writes the state before the V, so that an uninstall after a
// crash in between still removes it, and restarts Explorer only when it
// holds Win+V.
func turnOnWinV(s winVSystem) error {
	state, err := s.state()
	if err != nil {
		return winVSettingsError(err)
	}
	list, err := s.disabled()
	if err != nil {
		return winVSettingsError(err)
	}
	next, added := withHotkeyLetter(list, 'V')
	want := state
	switch {
	case added:
		want = winVAdded
	case state == winVOff:
		want = winVKept
	}
	if want != state {
		if err := s.setState(want); err != nil {
			return winVSettingsError(err)
		}
	}
	if added {
		if err := s.setDisabled(next); err != nil {
			if want != state {
				_ = s.setState(state)
			}
			return winVSettingsError(err)
		}
	} else if s.register() == nil {
		return nil
	}
	if err := s.restartExplorer(); err != nil {
		return errors.New("已设置，但未能重启资源管理器；注销后重新登录即可生效")
	}
	if err := s.register(); err != nil {
		return errors.New("资源管理器仍占用 Win+V，请注销后重新登录再试")
	}
	return nil
}

// turnOffWinV lets go of Win+V before Explorer restarts to take it back. It
// restarts Explorer only when it removed the V.
func turnOffWinV(s winVSystem) error {
	s.unregister()
	state, err := s.state()
	if err != nil {
		return winVSettingsError(err)
	}
	if state == winVOff {
		return nil
	}
	var restartErr error
	if state == winVAdded {
		list, err := s.disabled()
		if err != nil {
			return winVSettingsError(err)
		}
		if next, removed := withoutHotkeyLetter(list, 'V'); removed {
			if err := s.setDisabled(next); err != nil {
				return winVSettingsError(err)
			}
			restartErr = s.restartExplorer()
		}
	}
	if err := s.setState(winVOff); err != nil {
		return winVSettingsError(err)
	}
	if restartErr != nil {
		return errors.New("已关闭，但未能重启资源管理器；注销后重新登录，系统的 Win+V 才会恢复")
	}
	return nil
}

// withHotkeyLetter adds letter to the DisabledHotkeys letters unless it is
// there in either case.
func withHotkeyLetter(list string, letter rune) (string, bool) {
	if strings.ContainsFunc(list, func(r rune) bool { return unicode.ToUpper(r) == unicode.ToUpper(letter) }) {
		return list, false
	}
	return list + string(letter), true
}

// withoutHotkeyLetter removes letter in either case, keeping the others.
func withoutHotkeyLetter(list string, letter rune) (string, bool) {
	next := strings.Map(func(r rune) rune {
		if unicode.ToUpper(r) == unicode.ToUpper(letter) {
			return -1
		}
		return r
	}, list)
	return next, next != list
}
