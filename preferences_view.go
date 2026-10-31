package main

import (
	"errors"
	"math"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type preferencesActions struct {
	appearance        func(mygo.ThemeSource)
	pause             func(bool)
	request           func()
	clipboardSettings func() error
	accessibility     func() error
	addExcludedApp    func()
	removeExcludedApp func(string)
	imageText         func(bool)
	migrate           func()
	saveRetention     func(RetentionPolicy)
	clear             func()
	update            func()
	close             func()
}

type preferencesView struct {
	state          PreferencesState
	actions        preferencesActions
	tab            int
	busy           bool
	notice         string
	failed         bool
	maxMiB         float64
	keepDays       float64
	retentionDirty bool
	confirmClear   bool
}

func (v *preferencesView) setState(s PreferencesState) {
	v.state = s
	if !v.retentionDirty {
		v.maxMiB, v.keepDays = float64(s.Storage.Retention.MaxMiB), float64(s.Storage.Retention.KeepDays)
	}
}

var preferenceTabs = []struct {
	label string
	icon  *ui.SVG
}{
	{"通用", ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M4 7h7m5 0h4M4 17h3m5 0h8"/><circle cx="13.5" cy="7" r="2.5"/><circle cx="9.5" cy="17" r="2.5"/></svg>`))},
	{"隐私", ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><path d="m12 3 8 3v5c0 5-8 10-8 10s-8-5-8-10V6Z"/><path d="m8 11 3 3 5-5"/></svg>`))},
	{"存储", ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M4 5v14c0 4 16 4 16 0V5M4 12c0 4 16 4 16 0"/></svg>`))},
}

func preferencesTheme(c *ui.Context) ui.Theme {
	theme := *c.Theme()
	theme.Accent, theme.AccentHover, theme.AccentPressed = ui.Hex("#7864da"), ui.Hex("#8976e3"), ui.Hex("#6551c6")
	if theme.Dark {
		theme.Accent = ui.Hex("#b0a0ef")
	}
	c.SetTheme(&theme)
	return theme
}

func (v *preferencesView) draw(c *ui.Context) {
	theme := preferencesTheme(c)
	if !v.confirmClear && !v.busy && (c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Cmd, ui.KeyW)) && v.actions.close != nil {
		v.actions.close()
	}
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Height(max(36, c.TitleBar().Height)).Justify(ui.Center).AlignItems(ui.Center).DragWindow().Children(func() {
			ui.Text(c, preferenceTabs[v.tab].label).FontSize(16).Bold()
		})
		ui.Row(c).Height(86).Justify(ui.Center).AlignItems(ui.Center).DragWindow().Children(func() {
			tabs := ui.TabsBase(c, &v.tab, len(preferenceTabs))
			tabs.List.Gap(4).Children(func() {
				for i, item := range preferenceTabs {
					tab := tabs.Tab(i).Width(84).Height(66).Radius(12).TextColor(theme.TextMuted)
					if i == v.tab {
						tab.Background(theme.Surface).TextColor(theme.Accent)
					}
					tab.Children(func() {
						ui.Column(c).Fill().Center().Gap(5).Children(func() {
							ui.Icon(c, item.icon).Size(23, 23)
							ui.Text(c, item.label).FontSize(12).FontWeight(500)
						})
					})
				}
			})
		})
		ui.Row(c).FillWidth().Height(1).Background(theme.Border)
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(22, 24).Gap(18).Children(func() {
				switch v.tab {
				case 0:
					v.general(c)
				case 1:
					v.privacy(c)
				case 2:
					v.storage(c)
				}
				if v.notice != "" {
					color := theme.TextMuted
					if v.failed {
						color = theme.Danger
					}
					ui.Text(c, v.notice).FontSize(12).TextColor(color)
				}
			})
		})
	})
	if ui.AlertDialog(c, &v.confirmClear, "清空所有复制记录？", "此操作无法撤销。常用语和当前系统剪贴板不会被清空。", "确认清空", "取消") == 0 && v.actions.clear != nil {
		v.actions.clear()
	}
}

func (v *preferencesView) group(c *ui.Context, name string, fn func()) {
	settingsGroup(c, name, v.busy, fn)
}

func settingsGroup(c *ui.Context, name string, busy bool, fn func()) {
	ui.Fieldset(c, name, fn).FillWidth().Margin(0).Padding(16, 18).Radius(14).Background(c.Theme().Surface).Disabled(busy)
}

func (v *preferencesView) row(c *ui.Context, name, description string, control func()) {
	ui.Form(c, func() {
		ui.Field(c, name, func() {
			ui.Row(c).FillWidth().Justify(ui.End).Children(control)
		}).FillWidth().Description(description)
	}).FillWidth()
}

func (v *preferencesView) general(c *ui.Context) {
	v.group(c, "界面与操作", func() {
		v.row(c, "外观", "", func() {
			names := map[mygo.ThemeSource]string{mygo.ThemeSystem: "跟随系统", mygo.ThemeLight: "浅色", mygo.ThemeDark: "深色"}
			selected := names[v.state.Appearance]
			if ui.Select(c, &selected, []string{"跟随系统", "浅色", "深色"}).Label("外观").Width(180).Disabled(v.actions.appearance == nil).Changed() {
				for source, name := range names {
					if name == selected {
						v.actions.appearance(source)
					}
				}
			}
		})
		ui.Row(c).FillWidth().Height(1).Background(c.Theme().Border)
		v.row(c, "唤起面板", "方向键或数字选择，Enter 粘贴。", func() { ui.Text(c, v.state.Shortcut).Bold() })
	})
	v.group(c, "版本与反馈", func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(16).Children(func() {
			ui.Column(c).Grow(1).Gap(5).Children(func() {
				ui.Text(c, "Clipnest "+v.state.Version).FontWeight(500)
				ui.Text(c, "每 4 小时自动检查更新。").FontSize(12).TextColor(c.Theme().TextMuted)
			})
			if ui.Button(c, "检查更新…").Disabled(v.actions.update == nil).Clicked() {
				v.actions.update()
			}
		})
		ui.Row(c).FillWidth().Height(1).Background(c.Theme().Border)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "遇到问题，或想提出建议？").Grow(1).FontSize(12).TextColor(c.Theme().TextMuted)
			ui.Link(c, "发送反馈…", "https://github.com/echoVic/clipnest/issues/new")
		})
	})
}

func (v *preferencesView) privacy(c *ui.Context) {
	v.group(c, "记录权限", func() {
		v.row(c, "暂停记录", "新的复制不保存，已有历史仍可使用。", func() {
			paused := v.state.Paused
			if ui.Switch(c, &paused).Label("暂停记录").Disabled(v.actions.pause == nil).Changed() {
				v.actions.pause(paused)
			}
		})
		ui.Row(c).FillWidth().Height(1).Background(c.Theme().Border)
		v.row(c, "剪贴板记录", clipboardDescription(v.state.ClipboardAccess), func() {
			ui.Column(c).Width(160).Gap(8).Children(func() {
				access := v.state.ClipboardAccess
				switch access {
				case clipboardAllowed:
					ui.Text(c, "已允许自动记录").TextColor(c.Theme().TextMuted)
				case clipboardDenied:
					ui.Text(c, "记录已被拒绝").TextColor(c.Theme().Danger)
				case clipboardDefault, clipboardAsk:
					if ui.Button(c, "开始记录").Disabled(v.state.Paused || v.actions.request == nil).Clicked() {
						v.actions.request()
					}
				case clipboardUnsupported:
					ui.Text(c, "此系统无需单独授权").TextColor(c.Theme().TextMuted)
				default:
					ui.Text(c, "无法确认记录权限").TextColor(c.Theme().Danger)
				}
				if access != clipboardUnsupported && access != clipboardDefault {
					if ui.Button(c, "记录权限设置…").Disabled(v.actions.clipboardSettings == nil).Clicked() {
						v.open(v.actions.clipboardSettings)
					}
				}
			})
		})
	})
	v.exclusions(c)
	v.imageText(c)
	v.group(c, "自动粘贴", func() {
		v.row(c, "辅助功能", "允许后，选完即可粘贴到原应用；未授权时仍可「仅复制」。", func() {
			switch {
			case v.state.Platform == "windows":
				ui.Text(c, "已启用，选完直接粘贴回原应用").TextColor(c.Theme().TextMuted)
			case v.state.Platform != "darwin":
				ui.Text(c, "此系统使用「仅复制」")
			case v.state.Accessible:
				ui.Text(c, "已允许").TextColor(c.Theme().TextMuted)
			default:
				if ui.Button(c, "辅助功能设置…").Disabled(v.actions.accessibility == nil).Clicked() {
					v.open(v.actions.accessibility)
				}
			}
		})
	})
	ui.Text(c, "文字与图片仅保存在这台电脑。标记为机密的内容不会记录。").FontSize(12).TextColor(c.Theme().TextMuted)
}

func clipboardDescription(access ClipboardAccess) string {
	switch access {
	case clipboardDefault:
		return "先在其他应用复制普通文字，再点「开始记录」，由系统询问授权。"
	case clipboardAsk:
		return "每次询问时，后台不会读取。持续自动记录需在系统设置中改为「允许」。"
	case clipboardDenied:
		return "新的复制不会记录，历史与常用语仍可使用。请在系统设置中改为「允许」。"
	case clipboardAllowed:
		return "记录权限与自动粘贴权限独立，可分别调整。"
	case clipboardUnsupported:
		return "沿用当前系统的剪贴板采集方式。"
	default:
		return "已停止读取新的复制，请检查系统设置。"
	}
}

func (v *preferencesView) storage(c *ui.Context) {
	v.group(c, "本地存储", func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(16).Children(func() {
			ui.Column(c).Grow(1).Gap(5).Children(func() {
				ui.Text(c, v.state.Storage.Backend).FontWeight(500)
				ui.Text(c, "历史与常用语在本机保存，重启后保留。").FontSize(12).TextColor(c.Theme().TextMuted)
			})
			if v.state.Storage.MigrationAvailable || v.state.Storage.CleanupPending {
				label := "一键迁移"
				if v.state.Storage.CleanupPending {
					label = "重试清理"
				}
				if ui.Button(c, label).Disabled(v.actions.migrate == nil).Clicked() {
					v.actions.migrate()
				}
			}
		})
	})
	v.group(c, "历史保留策略", func() {
		v.row(c, "历史内容容量(MiB)", "", func() {
			if ui.NumberInput(c, &v.maxMiB, 0, 1<<20, 1).Width(180).Justify(ui.End).Disabled(v.state.Storage.Backend != "sqlite").Changed() {
				v.retentionDirty = true
			}
		})
		v.row(c, "保留天数", "", func() {
			if ui.NumberInput(c, &v.keepDays, 0, 36500, 1).Width(180).Justify(ui.End).Disabled(v.state.Storage.Backend != "sqlite").Changed() {
				v.retentionDirty = true
			}
		})
		ui.Row(c).FillWidth().Height(1).Background(c.Theme().Border)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(18).Children(func() {
			note := "0 表示不限。保存后从最旧的历史开始清理，常用语始终保留。"
			if v.state.Storage.Backend != "sqlite" {
				note = "请先迁移到 SQLite，再设置历史保留策略。"
			}
			ui.Text(c, note).Grow(1).MinWidth(0).FontSize(12).TextColor(c.Theme().TextMuted)
			if ui.Button(c, "保存并清理").Disabled(v.state.Storage.Backend != "sqlite" || v.actions.saveRetention == nil).Clicked() {
				policy, err := v.retentionPolicy()
				if err != nil {
					v.notice, v.failed = err.Error(), true
				} else {
					v.actions.saveRetention(policy)
				}
			}
		})
	})
	v.group(c, "清理历史", func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(16).Children(func() {
			ui.Text(c, "仅清空复制历史，不删除常用语。").Grow(1).FontSize(12).TextColor(c.Theme().TextMuted)
			if ui.Button(c, "清空…").TextColor(c.Theme().Danger).Disabled(v.actions.clear == nil).Clicked() {
				v.confirmClear = true
			}
		})
	})
}

func (v *preferencesView) retentionPolicy() (RetentionPolicy, error) {
	for _, value := range []float64{v.maxMiB, v.keepDays} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value != math.Trunc(value) || value > 1<<20 {
			return RetentionPolicy{}, errors.New("请输入 0 或正整数")
		}
	}
	p := RetentionPolicy{int(v.maxMiB), int(v.keepDays)}
	return p, p.validate()
}

func (v *preferencesView) open(fn func() error) {
	if err := fn(); err != nil {
		v.notice, v.failed = err.Error(), true
	}
}
