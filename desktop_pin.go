package main

// hideOnBlur reports whether the panel hides when it loses the keyboard. A
// pinned panel stays, as does one busy pasting or guiding a permission.
func hideOnBlur(busy, permission, pinned bool) bool {
	return !busy && !permission && !pinned
}

type toggleAction int

const (
	toggleOpen toggleAction = iota
	toggleFocus
	toggleClose
)

// panelToggle decides what the summon shortcut and the tray button do. A
// pinned panel without the keyboard takes it back instead of hiding.
func panelToggle(visible, pinned, focused bool) toggleAction {
	switch {
	case !visible:
		return toggleOpen
	case pinned && !focused:
		return toggleFocus
	default:
		return toggleClose
	}
}

// trackedTarget is the app a pinned panel pastes into: the last app in front
// that is not Clipnest, which Target reports as PID 0.
func trackedTarget(current, front targetApp) targetApp {
	if front.PID == 0 {
		return current
	}
	return front
}
