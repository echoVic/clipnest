package main

import "github.com/egoist/mygo/ui"

type permissionPurpose uint8

const (
	permissionRecord permissionPurpose = iota
	permissionPaste
)

type permissionView struct {
	purpose permissionPurpose
	access  ClipboardAccess
	choose  func(string)
}

func (v *permissionView) draw(c *ui.Context) {
	theme := preferencesTheme(c)
	if c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Cmd, ui.KeyW) {
		v.choose("cancel")
	}
	title, heading := "剪贴板记录", "允许 Clipnest 记录复制内容"
	description := "文字与图片仅保存在这台电脑，标记为机密的内容不会记录。"
	primary, decision := "开始记录", "request"
	if v.purpose == permissionPaste {
		title, heading = "自动粘贴", "允许粘贴回原来的应用"
		description = "辅助功能用于发送粘贴按键，与剪贴板记录权限独立。"
		primary, decision = "打开系统设置…", "settings"
	} else if v.access == clipboardDenied || v.access == clipboardUnknown {
		heading = "剪贴板记录尚未获准"
		primary, decision = "记录权限设置…", "settings"
	}
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Height(max(36, c.TitleBar().Height)).Center().DragWindow().Children(func() {
			ui.Text(c, title).FontSize(16).Bold()
		})
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(18, 24).Gap(18).Children(func() {
				ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Row(c).Size(46, 46).Center().Radius(14).Background(theme.Surface).Children(func() {
						ui.Icon(c, preferenceTabs[1].icon).Size(26, 26).TextColor(theme.Accent)
					})
					ui.Text(c, heading).FontSize(18).Bold()
					ui.Text(c, description).FontSize(12).TextColor(theme.TextMuted)
				})
				settingsGroup(c, "权限说明", false, func() {
					if v.purpose == permissionPaste {
						ui.Text(c, "系统设置 → 隐私与安全性 → 辅助功能").FontWeight(500)
						ui.Text(c, "允许 Clipnest 后，重新选择内容即可自动粘贴。未授权时，可以「这次仅复制」，再手动按 ⌘V。").FontSize(12).TextColor(theme.TextMuted)
					} else {
						names := map[ClipboardAccess]string{
							clipboardDefault: "等待授权", clipboardAsk: "每次询问",
							clipboardDenied: "已拒绝", clipboardUnknown: "无法确认",
							clipboardAllowed: "已允许", clipboardUnsupported: "无需单独授权",
						}
						ui.Row(c).FillWidth().Children(func() {
							ui.Text(c, "当前系统权限").Grow(1)
							ui.Text(c, names[v.access]).TextColor(theme.TextMuted)
						})
						ui.Row(c).FillWidth().Height(1).Background(theme.Border)
						ui.Text(c, clipboardDescription(v.access)).FontSize(12).TextColor(theme.TextMuted)
						ui.Text(c, "一次读取成功不代表持续授权。自动记录需将系统中的「从其他 App 粘贴」设为「允许」。").FontSize(12).TextColor(theme.TextMuted)
					}
				})
			})
		})
		ui.Row(c).FillWidth().Height(1).Background(theme.Border)
		ui.Row(c).FillWidth().Padding(16, 24).Justify(ui.End).Gap(8).Children(func() {
			if ui.Button(c, "取消").AutoFocus().Clicked() {
				v.choose("cancel")
			}
			if v.purpose == permissionPaste && ui.Button(c, "这次仅复制").Clicked() {
				v.choose("copy")
			}
			if v.purpose == permissionRecord && v.access == clipboardAsk && ui.Button(c, "记录权限设置…").Clicked() {
				v.choose("settings")
			}
			if ui.PrimaryButton(c, primary).Clicked() {
				v.choose(decision)
			}
		})
	})
}
