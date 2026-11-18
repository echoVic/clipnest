package main

import "github.com/egoist/mygo/ui"

// dataLocation is the data directory's row in the storage group.
func (v *preferencesView) dataLocation(c *ui.Context) {
	muted := c.Theme().TextMuted
	ui.Row(c).FillWidth().Height(1).Background(c.Theme().Border)
	reason := ""
	switch {
	case v.state.DataDirFixed:
		reason = "数据位置由启动参数 --data-dir 指定，不能在这里更改。"
	case v.state.Storage.Backend != "sqlite":
		reason = "请先迁移到 SQLite，再更改数据位置。"
	}
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
			ui.Text(c, "数据位置").FontWeight(500)
			ui.Text(c, v.state.DataDir).FontSize(12).TextColor(muted)
		})
		if ui.Button(c, "打开文件夹").Disabled(v.actions.openDataDir == nil).Clicked() {
			v.open(v.actions.openDataDir)
		}
		if ui.Button(c, "更改…").Disabled(reason != "" || v.actions.pickDataDir == nil).Clicked() {
			v.actions.pickDataDir()
		}
	})
	if reason != "" {
		ui.Text(c, reason).FontSize(12).TextColor(muted)
	} else if !v.state.DataDirDefault {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Text(c, "换到别的位置后，可以随时搬回默认位置。").Grow(1).MinWidth(0).FontSize(12).TextColor(muted)
			if ui.Button(c, "恢复默认位置").Disabled(v.actions.restoreDataDir == nil).Clicked() {
				v.actions.restoreDataDir()
			}
		})
	}
	if v.state.DataDirNotice != "" {
		ui.Text(c, v.state.DataDirNotice).FontSize(12).TextColor(muted)
	}
}

// askMove shows the confirmation for moving to t.
func (v *preferencesView) askMove(t moveTarget) {
	v.moveTarget, v.confirmMove = t, true
}

// moveDialog asks before Clipnest moves its data, or switches to the data a
// folder already has, and restarts.
func moveDialog(t moveTarget) (title, message, confirm string) {
	if t.existing {
		title, confirm = "改用这里的数据？", "改用这里的数据"
		message = "「" + t.dir + "」里已有 Clipnest 的数据。Clipnest 会重启并改用这份数据，当前位置的数据原样保留，两边不会合并。"
	} else {
		title, confirm = "把数据移到这里？", "移动并重启"
		message = "数据会复制到「" + t.dir + "」，校验无误后 Clipnest 重启并改用这里，新位置启动成功后删除旧位置的数据。移动期间不记录新的复制。"
	}
	if t.synced != "" {
		message += "\n\n这个文件夹由 " + t.synced + " 同步。同步中的数据库可能损坏，多台电脑同时使用也会冲突，建议选本机的文件夹。"
	}
	return title, message, confirm
}
