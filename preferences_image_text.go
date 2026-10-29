package main

import "github.com/egoist/mygo/ui"

func (v *preferencesView) imageText(c *ui.Context) {
	v.group(c, "图片文字", func() {
		switch {
		case v.state.Platform != "darwin":
			ui.Text(c, "图片文字识别仅适用于 macOS。").FontSize(12).TextColor(c.Theme().TextMuted)
		case v.state.Storage.Backend != "sqlite":
			ui.Text(c, "迁移到 SQLite 后可识别图片文字。").FontSize(12).TextColor(c.Theme().TextMuted)
		default:
			// The field's label names the switch and toggles it when clicked.
			v.row(c, "识别图片文字", "在本机识别截图中的文字，不联网，可搜索和复制。关闭后删除已识别的文字。", func() {
				on := v.state.Storage.ImageText
				if ui.Switch(c, &on).Disabled(v.actions.imageText == nil).Changed() {
					v.actions.imageText(on)
				}
			})
		}
	})
}
