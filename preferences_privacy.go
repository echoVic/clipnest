package main

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

var secretModes = []struct{ mode, label string }{
	{secretsMask, "遮罩后记录"}, {secretsSkip, "不记录"}, {secretsOff, "不识别"},
}

// sensitive draws the secret, clear and ignore settings. Each change saves the
// whole settings value; the rule input keeps its draft until the rule is saved.
func (v *preferencesView) sensitive(c *ui.Context) {
	theme := c.Theme()
	current := v.state.Privacy
	save := func(change func(*PrivacySettings)) {
		next := current.clone()
		change(&next)
		v.actions.savePrivacy(next)
	}
	disabled := v.actions.savePrivacy == nil
	v.group(c, "敏感内容", func() {
		v.row(c, "密钥、令牌与银行卡号", "识别常见格式的密钥、令牌，以及整条复制的银行卡号。遮罩后列表和搜索只显示首尾，粘贴仍是原文。", func() {
			labels := make([]string, len(secretModes))
			selected := ""
			for i, m := range secretModes {
				labels[i] = m.label
				if m.mode == current.Secrets {
					selected = m.label
				}
			}
			if ui.Select(c, &selected, labels).Label("密钥、令牌与银行卡号").Width(180).Disabled(disabled).Changed() {
				for _, m := range secretModes {
					if m.label == selected {
						save(func(s *PrivacySettings) { s.Secrets = m.mode })
					}
				}
			}
		})
		ui.Row(c).FillWidth().Height(1).Background(theme.Border)
		v.row(c, "清空时删除", "复制后 5 分钟内剪贴板被清空（密码管理器常这样做）时，删除刚记下的那条。", func() {
			on := current.ClearSync
			if ui.Switch(c, &on).Disabled(disabled).Changed() {
				save(func(s *PrivacySettings) { s.ClearSync = on })
			}
		})
		ui.Row(c).FillWidth().Height(1).Background(theme.Border)
		ui.Column(c).FillWidth().Gap(4).Children(func() {
			ui.Text(c, "忽略内容").FontWeight(500)
			ui.Text(c, "文字匹配任一正则表达式时不记录，图片不受影响。区分大小写，可用 (?i)、^、$。").FontSize(12).TextColor(theme.TextMuted)
		})
		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
			input := ui.TextInput(c, &v.ignoreDraft).Label("添加忽略规则").Placeholder(`例如 ^\d{6}$`).Grow(1).MinWidth(0).Disabled(disabled)
			add := ui.Button(c, "添加").Disabled(disabled || strings.TrimSpace(v.ignoreDraft) == "").Clicked()
			if input.Submitted() || add {
				v.addIgnoreRule()
			}
		})
		if len(current.Ignore) == 0 {
			ui.Text(c, "尚未添加规则").FontSize(12).TextColor(theme.TextMuted)
		}
		for _, rule := range current.Ignore {
			ui.Row(c).FillWidth().Height(1).Background(theme.Border)
			ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Text(c, rule).Grow(1).MinWidth(0)
				if ui.Button(c, "移除").Label("移除 " + rule).Disabled(disabled).Clicked() {
					save(func(s *PrivacySettings) {
						s.Ignore = slices.DeleteFunc(s.Ignore, func(r string) bool { return r == rule })
					})
				}
			})
		}
	})
}

// addIgnoreRule checks the drafted rule before saving it, so a mistake keeps
// the draft for editing.
func (v *preferencesView) addIgnoreRule() {
	rule := strings.TrimSpace(v.ignoreDraft)
	if rule == "" || v.busy || v.actions.savePrivacy == nil {
		return
	}
	next := v.state.Privacy.clone()
	next.Ignore = append(next.Ignore, rule)
	if _, err := next.compile(); err != nil {
		v.notice, v.failed = err.Error(), true
		return
	}
	v.notice, v.failed = "", false
	v.actions.savePrivacy(next)
}
