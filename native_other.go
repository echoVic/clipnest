//go:build !darwin

package main

import "errors"

type nativeBridge struct{}
type targetApp struct {
	PID  int
	Name string
}

func openNative() (*nativeBridge, error)               { return &nativeBridge{}, nil }
func (*nativeBridge) Target() targetApp                { return targetApp{} }
func (*nativeBridge) Activate(targetApp) bool          { return false }
func (*nativeBridge) Revision() int64                  { return -1 }
func (*nativeBridge) ClipboardAccess() ClipboardAccess { return clipboardUnsupported }
func (*nativeBridge) Accessible() bool                 { return false }
func (*nativeBridge) ReadyToPaste(targetApp) bool      { return false }
func (*nativeBridge) Paste(targetApp) error {
	return errors.New("当前系统支持复制历史内容，请返回原应用手动粘贴")
}
