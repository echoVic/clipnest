package main

import (
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func transferView() *preferencesView {
	v := settingsView()
	v.tab = 2
	return v
}

func TestPreferencesOfferImportAndExport(t *testing.T) {
	v := transferView()
	exported, imported := 0, 0
	v.actions.exportData = func() { exported++ }
	v.actions.importData = func() { imported++ }
	tt := ui.NewTester(v.draw, 620, 900)
	if !tt.HasText("导入与导出") || !tt.HasText("把历史、常用语和设置导出成一个文件，或从文件导入并合并。") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("导出…"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("导入…"); err != nil {
		t.Fatal(err)
	}
	if exported != 1 || imported != 1 {
		t.Fatal(exported, imported)
	}
}

func TestPreferencesImportAndExportNeedSQLite(t *testing.T) {
	v := transferView()
	v.state.Storage.Backend, v.state.Storage.MigrationAvailable = "json", true
	v.actions.exportData = func() { t.Fatal("exported") }
	v.actions.importData = func() { t.Fatal("imported") }
	tt := ui.NewTester(v.draw, 620, 900)
	tt.Click("导出…") // disabled: the click does nothing
	tt.Click("导入…")
	if !tt.HasText("请先迁移到 SQLite，再导入或导出。") {
		t.Fatal(tt.Texts())
	}
}

func TestExportDialog(t *testing.T) {
	plain := exportDialog(ExportSummary{History: 3, Phrases: 2})
	if plain.CheckboxLabel != "" || !strings.Contains(plain.Detail, "历史共 3 条，常用语 2 条，另有设置。") || !strings.Contains(plain.Detail, "没有加密") {
		t.Fatalf("%+v", plain)
	}
	secret := exportDialog(ExportSummary{History: 5, Images: 1, Phrases: 0, Sensitive: 2})
	if secret.CheckboxLabel != "包含敏感条目（2 条）" || secret.CheckboxChecked || !strings.Contains(secret.Detail, "（其中 1 张图片）") || !strings.Contains(secret.Detail, "有 2 条历史含密码或密钥，默认不导出。") {
		t.Fatalf("%+v", secret)
	}
	if !slices.Equal(secret.Buttons, []string{"导出…", "取消"}) || secret.CancelButton != 1 {
		t.Fatal(secret.Buttons)
	}
}

func TestImportDialog(t *testing.T) {
	plan := ImportPlan{
		Manifest: exportManifest{App: "0.1.17", Platform: "darwin", Exported: time.Date(2026, 11, 19, 10, 30, 0, 0, time.Local).UnixMilli()},
		New:      4, Existing: 2, Skipped: 1, Invalid: 3, NewPhrases: 2, DuplicatePhrases: 1, PhrasesOver: 5,
		Pruned: 6, PrunedNew: 2,
		Settings: &exportSettings{Platform: "windows"}, PrunedWithSettings: 0,
	}
	opts := importDialog(plan, "Clipnest 导出 2026-11-19.zip")
	for _, line := range []string{
		"导出于 2026-11-19 10:30，来自 macOS 上的 Clipnest 0.1.17",
		"历史：新增 4 条，已有 2 条，已有的条目保留较晚的时间",
		"按本机隐私设置不导入 1 条",
		"无效、已跳过 3 条",
		"常用语：新增 2 条，重复 1 条",
		"常用语最多 500 条，5 条放不下",
		"按当前保留策略，导入后会清理 6 条最旧的历史，其中 2 条是这次导入的",
		"同时导入设置的话，按文件里的保留策略会清理 0 条",
		"导入期间不记录新的复制。",
	} {
		if !strings.Contains(opts.Detail, line) {
			t.Errorf("missing %q in\n%s", line, opts.Detail)
		}
	}
	if runtime.GOOS != "windows" && !strings.Contains(opts.Detail, "快捷键和排除的应用来自 Windows，不会导入") {
		t.Error("no note about the other platform", opts.Detail)
	}
	if opts.Message != "导入「Clipnest 导出 2026-11-19.zip」？" || opts.CheckboxLabel != "同时导入设置（覆盖当前设置）" || opts.CheckboxChecked || !slices.Equal(opts.Buttons, []string{"导入", "取消"}) {
		t.Fatalf("%+v", opts)
	}

	quiet := importDialog(ImportPlan{New: 1, Manifest: plan.Manifest}, "a.zip")
	for _, absent := range []string{"已有", "隐私", "无效", "常用语", "清理", "设置"} {
		if strings.Contains(quiet.Detail, absent) {
			t.Errorf("%q shown for a zero count:\n%s", absent, quiet.Detail)
		}
	}
	if quiet.CheckboxLabel != "" {
		t.Error("offered settings the file doesn't have")
	}
	invalid := importDialog(ImportPlan{New: 1, Manifest: plan.Manifest, SettingsProblem: "保留天数无效"}, "a.zip")
	if !strings.Contains(invalid.Detail, "文件里的设置无效，不会导入：保留天数无效") || invalid.CheckboxLabel != "" {
		t.Fatalf("%+v", invalid)
	}
	nothing := importDialog(ImportPlan{Existing: 3, Manifest: plan.Manifest}, "a.zip")
	if nothing.Message != "没有可导入的新内容" || !slices.Equal(nothing.Buttons, []string{"好"}) || nothing.CheckboxLabel != "" {
		t.Fatalf("%+v", nothing)
	}
}

func TestTransferNotices(t *testing.T) {
	if got := exportNotice("/Users/me/Downloads/a.zip", ExportSummary{History: 3, Phrases: 2, Sensitive: 1}, false); got != "已导出到 /Users/me/Downloads/a.zip：3 条历史、2 条常用语，1 条敏感条目没有导出" {
		t.Error(got)
	}
	if got := exportNotice("a.zip", ExportSummary{History: 3, Sensitive: 1}, true); got != "已导出到 a.zip：3 条历史、0 条常用语" {
		t.Error(got)
	}
	r := ImportResult{New: 4, NewPhrases: 2, Skipped: 1, Invalid: 3, PhrasesOver: 5, Pruned: 6, Settings: true, Problems: []string{"隐私设置没能保存：磁盘已满"}}
	if got := importNotice(r); got != "已导入：新增 4 条历史、2 条常用语；按隐私设置跳过 1 条，无效 3 条，5 条常用语放不下，清理了 6 条最旧的历史；设置已导入；隐私设置没能保存：磁盘已满" {
		t.Error(got)
	}
	if got := importNotice(ImportResult{New: 1}); got != "已导入：新增 1 条历史、0 条常用语" {
		t.Error(got)
	}
	if got := exportFileName(time.Date(2026, 11, 19, 8, 0, 0, 0, time.Local)); got != "Clipnest 导出 2026-11-19.zip" {
		t.Error(got)
	}
}
