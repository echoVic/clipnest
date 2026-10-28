package main

import "github.com/egoist/mygo/ui"

func (v *preferencesView) exclusions(c *ui.Context) {
	v.group(c, "排除应用", func() {
		if v.state.Platform != "darwin" {
			ui.Text(c, "按应用排除记录仅适用于 macOS。").FontSize(12).TextColor(c.Theme().TextMuted)
			return
		}
		ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "跳过这些应用的新复制，已有历史与常用语保留。").Grow(1).MinWidth(0).FontSize(12).TextColor(c.Theme().TextMuted)
			if ui.Button(c, "添加应用…").Disabled(v.actions.addExcludedApp == nil).Clicked() {
				v.actions.addExcludedApp()
			}
		})
		if len(v.state.ExcludedApps) == 0 {
			ui.Text(c, "尚未排除任何应用").FontSize(12).TextColor(c.Theme().TextMuted)
		}
		for _, app := range v.state.ExcludedApps {
			ui.Row(c).FillWidth().Height(1).Background(c.Theme().Border)
			ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
					ui.Text(c, app.Name).FontWeight(500)
					ui.Text(c, app.BundleID).FontSize(11).TextColor(c.Theme().TextMuted)
				})
				if ui.Button(c, "移除").Label("移除 " + app.Name).Disabled(v.actions.removeExcludedApp == nil).Clicked() {
					v.actions.removeExcludedApp(app.BundleID)
				}
			})
		}
		ui.Row(c).FillWidth().Height(1).Background(c.Theme().Border)
		ui.Text(c, "按检测时的前台应用判断；后台复制或快速切换可能无法识别真实来源。有规则但来源不明时会跳过。").FontSize(11).TextColor(c.Theme().TextMuted)
	})
}
