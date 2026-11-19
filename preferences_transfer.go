package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

var zipFilters = []mygo.FileFilter{{Name: "ZIP", Extensions: []string{"zip"}}}

// transfer is the storage tab's import and export group.
func (v *preferencesView) transfer(c *ui.Context) {
	v.group(c, "导入与导出", func() {
		sqlite := v.state.Storage.Backend == "sqlite"
		note := "把历史、常用语和设置导出成一个文件，或从文件导入并合并。"
		if !sqlite {
			note = "请先迁移到 SQLite，再导入或导出。"
		}
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Text(c, note).Grow(1).MinWidth(0).FontSize(12).TextColor(c.Theme().TextMuted)
			if ui.Button(c, "导出…").Disabled(!sqlite || v.actions.exportData == nil).Clicked() {
				v.actions.exportData()
			}
			if ui.Button(c, "导入…").Disabled(!sqlite || v.actions.importData == nil).Clicked() {
				v.actions.importData()
			}
		})
	})
}

func exportFileName(now time.Time) string {
	return "Clipnest 导出 " + now.Format("2006-01-02") + ".zip"
}

func exportDialog(s ExportSummary) mygo.MessageOptions {
	images := ""
	if s.Images > 0 {
		images = fmt.Sprintf("（其中 %d 张图片）", s.Images)
	}
	detail := fmt.Sprintf("历史共 %d 条%s，常用语 %d 条，另有设置。导出的文件没有加密，拿到文件的人能看到其中的全部内容，包括常用语。", s.History, images, s.Phrases)
	opts := mygo.MessageOptions{
		Type: mygo.MessageQuestion, Title: "Clipnest", Message: "导出历史、常用语和设置？",
		Detail: detail, Buttons: []string{"导出…", "取消"}, CancelButton: 1,
	}
	if s.Sensitive > 0 {
		opts.Detail += fmt.Sprintf("\n\n有 %d 条历史含密码或密钥，默认不导出。", s.Sensitive)
		opts.CheckboxLabel = fmt.Sprintf("包含敏感条目（%d 条）", s.Sensitive)
	}
	return opts
}

func exportNotice(path string, s ExportSummary, secrets bool) string {
	notice := fmt.Sprintf("已导出到 %s：%d 条历史、%d 条常用语", path, s.History, s.Phrases)
	if !secrets && s.Sensitive > 0 {
		notice += fmt.Sprintf("，%d 条敏感条目没有导出", s.Sensitive)
	}
	return notice
}

// importDialog lists what importing would do, a line per count that isn't
// zero.
func importDialog(plan ImportPlan, name string) mygo.MessageOptions {
	m := plan.Manifest
	lines := []string{fmt.Sprintf("导出于 %s，来自 %s 上的 Clipnest %s", time.UnixMilli(m.Exported).Format("2006-01-02 15:04"), platformName(m.Platform), m.App)}
	add := func(cond bool, format string, args ...any) {
		if cond {
			lines = append(lines, fmt.Sprintf(format, args...))
		}
	}
	history := fmt.Sprintf("历史：新增 %d 条", plan.New)
	if plan.Existing > 0 {
		history += fmt.Sprintf("，已有 %d 条，已有的条目保留较晚的时间", plan.Existing)
	}
	add(plan.New > 0 || plan.Existing > 0, "%s", history)
	add(plan.Skipped > 0, "按本机隐私设置不导入 %d 条", plan.Skipped)
	add(plan.Invalid > 0, "无效、已跳过 %d 条", plan.Invalid)
	add(plan.NewPhrases > 0 || plan.DuplicatePhrases > 0, "常用语：新增 %d 条，重复 %d 条", plan.NewPhrases, plan.DuplicatePhrases)
	add(plan.PhrasesOver > 0, "常用语最多 500 条，%d 条放不下", plan.PhrasesOver)
	add(plan.Pruned > 0, "按当前保留策略，导入后会清理 %d 条最旧的历史，其中 %d 条是这次导入的", plan.Pruned, plan.PrunedNew)
	if s := plan.Settings; s != nil {
		add(plan.PrunedWithSettings != plan.Pruned, "同时导入设置的话，按文件里的保留策略会清理 %d 条", plan.PrunedWithSettings)
		add(s.Platform != runtime.GOOS, "快捷键和排除的应用来自 %s，不会导入", platformName(s.Platform))
	}
	add(plan.SettingsProblem != "", "文件里的设置无效，不会导入：%s", plan.SettingsProblem)
	if plan.nothingNew() {
		return mygo.MessageOptions{Type: mygo.MessageInfo, Title: "Clipnest", Message: "没有可导入的新内容", Detail: strings.Join(lines, "\n"), Buttons: []string{"好"}}
	}
	lines = append(lines, "导入期间不记录新的复制。")
	opts := mygo.MessageOptions{
		Type: mygo.MessageQuestion, Title: "Clipnest", Message: "导入「" + name + "」？",
		Detail: strings.Join(lines, "\n"), Buttons: []string{"导入", "取消"}, CancelButton: 1,
	}
	if plan.Settings != nil {
		opts.CheckboxLabel = "同时导入设置（覆盖当前设置）"
	}
	return opts
}

func importNotice(r ImportResult) string {
	notice := fmt.Sprintf("已导入：新增 %d 条历史、%d 条常用语", r.New, r.NewPhrases)
	var skipped []string
	if r.Skipped > 0 {
		skipped = append(skipped, fmt.Sprintf("按隐私设置跳过 %d 条", r.Skipped))
	}
	if r.Invalid > 0 {
		skipped = append(skipped, fmt.Sprintf("无效 %d 条", r.Invalid))
	}
	if r.PhrasesOver > 0 {
		skipped = append(skipped, fmt.Sprintf("%d 条常用语放不下", r.PhrasesOver))
	}
	if r.Pruned > 0 {
		skipped = append(skipped, fmt.Sprintf("清理了 %d 条最旧的历史", r.Pruned))
	}
	if len(skipped) > 0 {
		notice += "；" + strings.Join(skipped, "，")
	}
	if r.Settings {
		notice += "；设置已导入"
	}
	for _, problem := range r.Problems {
		notice += "；" + problem
	}
	return notice
}

// exportData asks what to export and where, then writes the file.
func (p *preferencesWindow) exportData() {
	p.report("正在准备导出…", func() (string, error) {
		summary, err := p.desktop.storage.ExportSummary()
		if err != nil {
			return "", err
		}
		opts := exportDialog(summary)
		opts.Parent = p.win
		answer, err := mygo.Dialog.Message(opts)
		if err != nil || answer.Button != 0 {
			return "", err
		}
		secrets := answer.CheckboxChecked && summary.Sensitive > 0
		dir, _ := os.UserHomeDir()
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent: p.win, Title: "导出 Clipnest 数据", ButtonLabel: "导出",
			DefaultPath: filepath.Join(dir, "Downloads", exportFileName(time.Now())),
			Filters:     zipFilters, CreateDirectories: true,
		})
		if err != nil || path == "" {
			return "", err
		}
		p.win.Update(func() { p.view.notice = "正在导出…" })
		written, err := p.desktop.exportData(path, secrets)
		if err != nil {
			return "", fmt.Errorf("导出失败：%w", err)
		}
		return exportNotice(path, written, secrets), nil
	}, false)
}

// importData checks a picked file, shows what importing it would do and
// imports it when confirmed.
func (p *preferencesWindow) importData() {
	p.report("", func() (string, error) {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: p.win, Title: "导入 Clipnest 数据", ButtonLabel: "导入", Filters: zipFilters})
		if err != nil || len(paths) == 0 {
			return "", err
		}
		p.win.Update(func() { p.view.notice = "正在检查导入文件…" })
		plan, err := p.desktop.storage.PlanImport(paths[0])
		if err != nil {
			return "", err
		}
		opts := importDialog(plan, filepath.Base(paths[0]))
		opts.Parent = p.win
		answer, err := mygo.Dialog.Message(opts)
		if err != nil || plan.nothingNew() || answer.Button != 0 {
			return "", err
		}
		p.win.Update(func() { p.view.notice = "正在导入…" })
		result, err := p.desktop.importData(paths[0], answer.CheckboxChecked && plan.Settings != nil)
		if err != nil {
			return "", err
		}
		return importNotice(result), nil
	}, false)
}
