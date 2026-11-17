package main

import (
	"errors"
	"log"
	"runtime"

	"github.com/egoist/mygo"
)

// setOpenAtLogin runs on a worker; MyGo moves the system call to main.
func (d *Desktop) setOpenAtLogin(on bool) error {
	if err := mygo.App.SetOpenAtLogin(on); err != nil {
		log.Print("登录时打开设置失败：", err)
		return loginItemError(runtime.GOOS)
	}
	return nil
}

// loginItemError says where the user can check the setting. On macOS a
// login item can also be waiting for approval in System Settings.
func loginItemError(goos string) error {
	switch goos {
	case "darwin":
		return errors.New("未能设置，请在「系统设置 → 通用 → 登录项」中检查 Clipnest")
	case "windows":
		return errors.New("未能设置，请在「任务管理器 → 启动应用」中检查 Clipnest")
	default:
		return errors.New("未能设置登录时打开")
	}
}
