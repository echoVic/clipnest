package main

// keyStroke is one Windows key event: a virtual-key code, pressed or released.
type keyStroke struct {
	vk uint16
	up bool
}

const (
	vkControl = 0x11
	vkV       = 0x56
)

// windowsPasteKeys are the events that paste on Windows. During a sequence
// the user is still holding Ctrl from the Ctrl+V the sequence caught, so only
// V is sent; pressing Ctrl again and releasing it would end that chord early.
func windowsPasteKeys(ctrlHeld bool) []keyStroke {
	if ctrlHeld {
		return []keyStroke{{vk: vkV}, {vk: vkV, up: true}}
	}
	return []keyStroke{{vk: vkControl}, {vk: vkV}, {vk: vkV, up: true}, {vk: vkControl, up: true}}
}
