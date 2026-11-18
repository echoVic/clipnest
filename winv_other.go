//go:build !windows

package main

import "errors"

var errWinVUnsupported = errors.New("Win+V 设置仅适用于 Windows")

// systemWinV is always off outside Windows, so restoring changes nothing.
func systemWinV() winVSystem {
	fail := func() error { return errWinVUnsupported }
	return winVSystem{
		state:           func() (winVState, error) { return winVOff, nil },
		setState:        func(winVState) error { return errWinVUnsupported },
		disabled:        func() (string, error) { return "", errWinVUnsupported },
		setDisabled:     func(string) error { return errWinVUnsupported },
		restartExplorer: fail,
		register:        fail,
		unregister:      func() {},
	}
}
