//go:build darwin

package main

import (
	"errors"

	"github.com/ebitengine/purego/objc"
)

func (n *nativeBridge) applicationBundleType() uintptr {
	name := n.makeString(n.getClass("NSString"), n.selector("stringWithUTF8String:"), "com.apple.application-bundle")
	return n.send(n.getClass("UTType"), "typeWithIdentifier:", name)
}

func (n *nativeBridge) newApplicationPanel() uintptr {
	panel := n.send(n.getClass("NSOpenPanel"), "openPanel")
	n.send(panel, "retain")
	text := func(value string) uintptr {
		return n.makeString(n.getClass("NSString"), n.selector("stringWithUTF8String:"), value)
	}
	n.send(panel, "setTitle:", text("选择要排除的应用"))
	n.send(panel, "setPrompt:", text("添加"))
	n.send(panel, "setCanChooseFiles:", 1)
	n.send(panel, "setCanChooseDirectories:", 0)
	n.send(panel, "setAllowsMultipleSelection:", 0)
	n.send(panel, "setTreatsFilePackagesAsDirectories:", 0)
	n.send(panel, "setCanCreateDirectories:", 0)
	n.send(panel, "setDirectoryURL:", n.send(n.getClass("NSURL"), "fileURLWithPath:", text("/Applications")))
	// Filename extension "app" resolves to application-file on macOS, which
	// excludes ordinary .app directories. Use the explicit bundle type.
	types := n.send(n.getClass("NSArray"), "arrayWithObject:", n.applicationBundleType())
	n.send(panel, "setAllowedContentTypes:", types)
	return panel
}

func (n *nativeBridge) openApplicationPicker(parent uintptr) (*applicationPicker, error) {
	defer n.pool()()
	if parent == 0 || n.applicationBundleType() == 0 {
		return nil, errors.New("无法打开应用选择器")
	}
	p := &applicationPicker{parent: parent, panel: n.newApplicationPanel(), result: make(chan string, 1)}
	block := objc.NewBlock(func(_ objc.Block, response int) {
		defer n.pool()()
		path := ""
		if response == 1 {
			path = n.text(n.send(n.send(p.panel, "URL"), "path"))
		}
		panel := p.complete(path)
		n.send(panel, "release")
	})
	n.send(p.panel, "beginSheetModalForWindow:completionHandler:", parent, uintptr(block))
	block.Release()
	return p, nil
}

func (n *nativeBridge) closeApplicationPicker(p *applicationPicker) {
	if p.panel == 0 {
		return
	}
	defer n.pool()()
	n.send(p.parent, "endSheet:returnCode:", p.panel, 0)
	// Quitting may stop the run loop before AppKit delivers the completion.
	p.resolve("")
}
