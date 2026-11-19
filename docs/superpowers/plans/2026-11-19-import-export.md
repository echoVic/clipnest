# 导入与导出 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 偏好设置「存储」页可以把历史、常用语和设置导出成一个 ZIP，也可以从这样的文件导入，按内容去重合并进现有数据。

**Architecture:** 导出另开一个只读的 SQLite 连接，在一个读事务（WAL 快照）里流式写 ZIP，不挡记录。导入分两遍：第一遍只读文件、算数字（新增、已有、跳过、无效、常用语、清理预估），第二遍在 `d.busy` 下用一个事务写入，事务提交后再保存 JSON 设置和快捷键。格式、读文件、清理预估、两遍各自是独立的单元，桌面和设置页只做编排和文字。

**Tech Stack:** Go 1.27、`archive/zip`、`encoding/json`（流式 `Decoder`）、modernc SQLite 3.54（WAL、`UPDATE … FROM`、窗口函数）、MyGo 0.9.0（`Dialog.Message` 的复选框、`Dialog.Save`、`Dialog.Open`）、`mygo/ui` 测试器。

**Spec:** `docs/superpowers/specs/2026-11-19-import-export-design.md`

## Global Constraints

- 直接在 `main` 上按顺序做，不用 git worktree。每个任务做完就提交；提交信息不加 Co-Authored-By，作者 echoVic。不推送，等用户明说。
- 格式名 `clipnest-export`，版本 1；文件名 `manifest.json`、`history.json`、`phrases.json`、`settings.json`、`images/<64 位小写十六进制>.png`。
- 上限：`manifest.json` 64 KB，`settings.json` 1 MB，`phrases.json` 200 MB，`history.json` 单条 4 MB，历史最多 `maxRetainedEntries`（1000000）条，常用语最多 `maxPhrases`（500）条，图片 `maxImage`（8 MB），来源 256 字节。
- JSON 写出时 `SetEscapeHTML(false)`。导出文件权限 0600，先写 `<目标>.part` 再改名。
- 敏感条目：非图片，且 `secret` 不为空或 `findSecrets(text)` 非空。
- 导入时每条历史走 `newEntry`，再按本机隐私设置判断（和复制时同一个函数）。时间在未来的改成导入时刻。
- 只支持 SQLite 存储，JSON 存储时提示「请先迁移到 SQLite，再导入或导出。」
- 文案照抄 spec。macOS 和 Windows 都要编译通过：`GOOS=windows go vet .`、`GOOS=linux go vet .`。
- 单个任务的测试：`go test -run '<名字>' .`；全量：`go test ./...`。

## 文件

| 文件 | 内容 |
|---|---|
| `transfer_format.go`（新） | 格式常量、上限、四个 JSON 结构、错误、`imageName`、`validImageName`、`sensitive`、`platformName` |
| `transfer_export.go`（新） | `openSnapshot`、`Storage.snapshot`、`ExportSummary`、`Storage.ExportSummary`、`exportOptions`、`Storage.Export`、`writeExport` |
| `transfer_import.go`（新） | `importFile`：打开和检查 ZIP、逐条读 `history.json`、`entry`、`importedEntry`、`phrases`、`settings`、`exportSettings.check` |
| `transfer_prune.go`（新） | `pruneItem`、`predictPrune` |
| `transfer_plan.go`（新） | `ImportPlan`、`Storage.PlanImport`、`mergeHistory`、`planPhrases`、`entryKey` |
| `transfer_apply.go`（新） | `ImportResult`、`Storage.Import`、`Storage.importFile`、`importHistorySQL` |
| `desktop_transfer.go`（新） | `Desktop.exportData`、`Desktop.importData` |
| `preferences_transfer.go`（新） | 「导入与导出」组、两个对话框、提示文字、`preferencesWindow.exportData`/`importData` |
| `privacy.go` | 加 `privacyStore.apply`（从 `Desktop.applyPrivacy` 搬过来） |
| `desktop_clipboard.go` | `applyPrivacy` 改为调用 `apply` |
| `app_exclusions.go` | 抽出 `checkExcludedApps`，加 `appExclusions.Replace` |
| `phrases.go` | 加 `Phrases.texts`、`phraseTexts` |
| `image_text.go` | 抽出 `sqliteStore.setImageTextSQL` |
| `preferences.go` | `run` 改成调用新的 `report`；`setShortcut` 抽出 `Desktop.applyShortcut`；接上两个动作 |
| `preferences_view.go` | 动作表加两项；存储页加一组 |
| `README.md`、`README.en.md`、`docs/roadmap.md` | 说明导入导出，路线图第 7 项标完成 |

---

### Task 1: 格式和快照

**Files:**
- Create: `transfer_format.go`、`transfer_export.go`、`transfer_export_test.go`

**Interfaces:**
- Produces: 常量 `exportFormat`、`exportVersion`、`manifestName`、`historyName`、`phrasesName`、`settingsName`、`imagePrefix`、`maxManifestBytes`、`maxSettingsBytes`、`maxPhrasesBytes`、`maxHistoryItemBytes`、`maxSourceBytes`、`maxExtraFiles`；类型 `exportManifest`、`exportItem`、`exportPhrases`、`exportSettings`；`errNotExport`、`errNewerExport`、`errDamaged`、`damaged(detail string) error`；`imageName(id string) string`、`validImageName(name string) bool`、`sensitive(kind, text, secret string) bool`、`platformName(goos string) string`；`openSnapshot(path string) (*sql.DB, *sql.Tx, error)`、`(s *Storage) snapshot() (*sql.DB, *sql.Tx, error)`；`ExportSummary{History, Images, Phrases, Sensitive int}`、`(s *Storage) ExportSummary() (ExportSummary, error)`；测试帮手 `transferStorage(t) *Storage`。

- [ ] **Step 1: 写失败的测试** `transfer_export_test.go`

```go
package main

import (
	"strings"
	"testing"
	"time"
)

// transferStorage is an empty SQLite storage.
func transferStorage(t *testing.T) *Storage {
	t.Helper()
	s, err := openStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func addEntries(t *testing.T, s *Storage, entries ...Entry) {
	t.Helper()
	for _, e := range entries {
		if err := s.History.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.History.Save(); err != nil {
		t.Fatal(err)
	}
}

func TestSensitiveEntries(t *testing.T) {
	for _, c := range []struct {
		kind, text, secret string
		want               bool
	}{
		{"text", "普通文字", "", false},
		{"text", "AWS=" + testAWSKey, "", true}, // recorded while privacy was off
		{"text", "规则后来认不出了", "AWS 访问密钥", true},
		{"image", "", "", false},
	} {
		if got := sensitive(c.kind, c.text, c.secret); got != c.want {
			t.Errorf("sensitive(%q, %q, %q) = %v", c.kind, c.text, c.secret, got)
		}
	}
}

func TestValidImageName(t *testing.T) {
	id := strings.Repeat("a1", 32)
	for name, want := range map[string]bool{
		"images/" + id + ".png":                    true,
		"images/" + strings.ToUpper(id) + ".png":   false,
		"images/" + id[:63] + ".png":               false,
		"images/../" + id[:61] + ".png":            false,
		id + ".png":                                false,
		"images/" + id + ".jpg":                    false,
	} {
		if validImageName(name) != want {
			t.Errorf("validImageName(%q) != %v", name, want)
		}
	}
}

func TestExportSnapshotDoesNotHoldUpWrites(t *testing.T) {
	s := transferStorage(t)
	addEntries(t, s, entry(t, "导出前"))
	panel, err := s.History.OpenView() // holds one of the storage's connections
	if err != nil {
		t.Fatal(err)
	}
	defer panel.Close()
	db, tx, err := s.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer tx.Rollback()
	late := entry(t, "导出时复制的")
	done := make(chan error, 1)
	go func() {
		s.History.Add(late)
		done <- s.History.Save()
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a copy waited for the snapshot")
	}
	var n int
	if err := tx.QueryRow("SELECT count(*) FROM history").Scan(&n); err != nil || n != 1 {
		t.Fatal("the snapshot moved", n, err)
	}
}

func TestExportSummary(t *testing.T) {
	s := transferStorage(t)
	masked := entry(t, "AWS_SECRET="+testAWSKey)
	masked.mask()
	addEntries(t, s, masked, entry(t, "token "+testGitHub), entry(t, "普通文字"), imageEntry(t, 10))
	if _, err := s.Phrases.Save("", "常用语"); err != nil {
		t.Fatal(err)
	}
	got, err := s.ExportSummary()
	if want := (ExportSummary{History: 4, Images: 1, Phrases: 1, Sensitive: 2}); err != nil || got != want {
		t.Fatal(got, err)
	}
}
```

- [ ] **Step 2: 跑测试，确认失败**

Run: `go test -run 'TestSensitiveEntries|TestValidImageName|TestExportSnapshot|TestExportSummary' .`
Expected: 编译失败，`sensitive`、`validImageName`、`snapshot`、`ExportSummary` 未定义。

- [ ] **Step 3: 写 `transfer_format.go`**

```go
package main

import (
	"errors"
	"fmt"
	"strings"
)

// An export file is a ZIP of JSON documents and PNG images. See
// docs/superpowers/specs/2026-11-19-import-export-design.md.
const (
	exportFormat  = "clipnest-export"
	exportVersion = 1

	manifestName = "manifest.json"
	historyName  = "history.json"
	phrasesName  = "phrases.json"
	settingsName = "settings.json"
	imagePrefix  = "images/"
)

// What an import reads at most. Sizes in a ZIP's headers aren't trusted:
// every file is read through these limits.
const (
	maxManifestBytes = 64 << 10
	maxSettingsBytes = 1 << 20
	maxPhrasesBytes  = 200 << 20 // 500 phrases of 64 KB, escaped
	// One element of history.json: 512 KB of text escapes to at most six
	// times as long, plus the other fields.
	maxHistoryItemBytes = 4 << 20
	maxSourceBytes      = 256
	// Files in the ZIP besides one image per entry.
	maxExtraFiles = 16
)

type exportManifest struct {
	Format   string `json:"format"`
	Version  int    `json:"version"`
	App      string `json:"app"`
	Platform string `json:"platform"`
	Exported int64  `json:"exported"` // Unix milliseconds
	// The counts are for people; an import counts the content itself.
	History int  `json:"history"`
	Images  int  `json:"images"`
	Phrases int  `json:"phrases"`
	Secrets bool `json:"secrets"` // whether sensitive entries are included
}

// exportItem is one history entry: text, or the name of its image. The
// rest of an entry is derived again on import.
type exportItem struct {
	Text    string `json:"text,omitempty"`
	Image   string `json:"image,omitempty"`
	Source  string `json:"source"`
	Created int64  `json:"created"`
}

type exportPhrases struct {
	Phrases []string `json:"phrases"`
}

// exportSettings are the settings an export carries. The shortcut and the
// excluded apps belong to Platform.
type exportSettings struct {
	Retention    RetentionPolicy `json:"retention"`
	ImageText    bool            `json:"imageText"`
	Privacy      PrivacySettings `json:"privacy"`
	Platform     string          `json:"platform"`
	Shortcut     string          `json:"shortcut"`
	ExcludedApps []ExcludedApp   `json:"excludedApps"`
}

var (
	errNotExport   = errors.New("这不是 Clipnest 的导出文件")
	errNewerExport = errors.New("这个文件来自更新版本的 Clipnest，请先更新")
	errDamaged     = errors.New("导入文件已损坏")
)

func damaged(detail string) error { return fmt.Errorf("%w：%s", errDamaged, detail) }

func imageName(id string) string { return imagePrefix + id + ".png" }

// validImageName reports whether name is images/<64 lowercase hex>.png, the
// only image names an export has.
func validImageName(name string) bool {
	id, ok := strings.CutPrefix(name, imagePrefix)
	if ok {
		id, ok = strings.CutSuffix(id, ".png")
	}
	if !ok || len(id) != 64 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// sensitive reports whether a history entry may hold a password or key: it
// was recorded masked, or the rules recognize a secret in it now. History
// recorded while privacy was off isn't masked, but it still counts.
func sensitive(kind, text, secret string) bool {
	return kind != "image" && (secret != "" || len(findSecrets(text)) > 0)
}

func platformName(goos string) string {
	switch goos {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	}
	return goos
}
```

- [ ] **Step 4: 写 `transfer_export.go` 的快照和统计**

```go
package main

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
)

// openSnapshot opens its own read-only connection to the database at path
// and starts a read transaction on it. Under WAL the transaction keeps the
// database as of its first read while the storage goes on writing, and it
// takes neither of the storage's two connections, one of which an open
// panel holds.
func openSnapshot(path string) (*sql.DB, *sql.Tx, error) {
	q := url.Values{"mode": {"ro"}, "_pragma": {"busy_timeout(5000)", "query_only(1)"}}
	db, err := sql.Open("sqlite", sqliteFileURI(path)+"?"+q.Encode())
	if err != nil {
		return nil, nil, err
	}
	db.SetMaxOpenConns(1)
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err == nil {
		// BEGIN alone doesn't take the snapshot; the first read does.
		var n int
		if err = tx.QueryRow("SELECT count(*) FROM history").Scan(&n); err != nil {
			tx.Rollback()
		}
	}
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	return db, tx, nil
}

// snapshot writes the history not yet in the database, then opens a
// snapshot of the database.
func (s *Storage) snapshot() (*sql.DB, *sql.Tx, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, nil, errors.New("存储已关闭")
	}
	if !s.sqlite.Load() {
		return nil, nil, errors.New("请先迁移到 SQLite，再导入或导出。")
	}
	if err := s.History.Save(); err != nil {
		return nil, nil, err
	}
	return openSnapshot(filepath.Join(s.dir, databaseName))
}

// ExportSummary counts what an export holds. Sensitive counts the history
// entries that may hold a password or key.
type ExportSummary struct {
	History, Images, Phrases, Sensitive int
}

func (s *Storage) ExportSummary() (ExportSummary, error) {
	var sum ExportSummary
	db, tx, err := s.snapshot()
	if err != nil {
		return sum, err
	}
	defer db.Close()
	defer tx.Rollback()
	err = tx.QueryRow("SELECT count(*), count(*) FILTER (WHERE kind='image') FROM history").Scan(&sum.History, &sum.Images)
	if err == nil {
		err = tx.QueryRow("SELECT count(*) FROM phrases").Scan(&sum.Phrases)
	}
	if err != nil {
		return sum, err
	}
	rows, err := tx.Query("SELECT h.kind, c.text, h.secret FROM history h JOIN history_content c ON c.id=h.id WHERE h.kind<>'image'")
	if err != nil {
		return sum, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, text, secret string
		if err := rows.Scan(&kind, &text, &secret); err != nil {
			return sum, err
		}
		if sensitive(kind, text, secret) {
			sum.Sensitive++
		}
	}
	return sum, rows.Err()
}
```

- [ ] **Step 5: 跑测试，确认通过**

Run: `go test -run 'TestSensitiveEntries|TestValidImageName|TestExportSnapshot|TestExportSummary' .`
Expected: PASS。如果 `mode=ro` 在 WAL 上打不开（`SQLITE_CANTOPEN`），改成 `mode=rw` 加 `query_only(1)`，其余不变。

- [ ] **Step 6: 提交**

```bash
gofmt -l . ; go vet .
git add transfer_format.go transfer_export.go transfer_export_test.go
git commit -m "feat(transfer): define the export format and read a snapshot of the data"
```

### Task 2: 写出导出文件

**Files:**
- Modify: `transfer_export.go`
- Test: `transfer_export_test.go`

**Interfaces:**
- Consumes: Task 1 的全部。
- Produces: `exportOptions{Secrets bool; Shortcut, App string; Now time.Time; read func() error}`、`(s *Storage) Export(path string, opts exportOptions) (ExportSummary, error)`（返回写进去的条数，`Sensitive` 是认出的敏感条目数，不论是否带上）；测试帮手 `readExport(t, path) map[string][]byte`、`exportItems(t, files) []exportItem`。

- [ ] **Step 1: 写失败的测试**（加到 `transfer_export_test.go`）

```go
// readExport reads every file of an export, by name.
func readExport(t *testing.T, path string) map[string][]byte {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	files := map[string][]byte{}
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[f.Name] = data
		if strings.HasPrefix(f.Name, imagePrefix) && f.Method != zip.Store {
			t.Errorf("%s is compressed again", f.Name)
		}
	}
	return files
}

func exportItems(t *testing.T, files map[string][]byte) []exportItem {
	t.Helper()
	var items []exportItem
	if err := json.Unmarshal(files[historyName], &items); err != nil {
		t.Fatal(err)
	}
	return items
}

func TestExportWritesTheFile(t *testing.T) {
	s := transferStorage(t)
	first, img, second := entry(t, "第一条"), imageEntry(t, 20), entry(t, "a<b & c")
	addEntries(t, s, first, img, second)
	for _, text := range []string{"甲", "乙"} {
		if _, err := s.Phrases.Save("", text); err != nil {
			t.Fatal(err)
		}
	}
	privacy := s.Privacy.Settings()
	privacy.ClearSync = false
	if err := s.Privacy.Save(privacy); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "Clipnest 导出.zip")
	now := time.UnixMilli(1763520000000)
	got, err := s.Export(path, exportOptions{Shortcut: "Alt+V", App: "0.1.17", Now: now})
	if want := (ExportSummary{History: 3, Images: 1, Phrases: 2}); err != nil || got != want {
		t.Fatal(got, err)
	}
	files := readExport(t, path)
	names := slices.Sorted(maps.Keys(files))
	want := []string{imageName(img.ID), historyName, manifestName, phrasesName, settingsName}
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Fatal(names)
	}
	if !bytes.Equal(files[imageName(img.ID)], img.Image) {
		t.Fatal("image changed")
	}
	if !bytes.Contains(files[historyName], []byte("a<b & c")) {
		t.Fatal("HTML escaped", string(files[historyName]))
	}
	// Newest first, as the panel lists them.
	items := exportItems(t, files)
	wantItems := []exportItem{
		{Text: second.Text, Source: second.Source, Created: second.Created},
		{Image: imageName(img.ID), Source: img.Source, Created: img.Created},
		{Text: first.Text, Source: first.Source, Created: first.Created},
	}
	if !reflect.DeepEqual(items, wantItems) {
		t.Fatal(items)
	}
	var manifest exportManifest
	var phrases exportPhrases
	var settings exportSettings
	for name, v := range map[string]any{manifestName: &manifest, phrasesName: &phrases, settingsName: &settings} {
		if err := json.Unmarshal(files[name], v); err != nil {
			t.Fatal(name, err)
		}
	}
	if want := (exportManifest{Format: exportFormat, Version: 1, App: "0.1.17", Platform: runtime.GOOS, Exported: now.UnixMilli(), History: 3, Images: 1, Phrases: 2}); manifest != want {
		t.Fatal(manifest)
	}
	if !slices.Equal(phrases.Phrases, []string{"甲", "乙"}) {
		t.Fatal(phrases)
	}
	wantSettings := exportSettings{Retention: RetentionPolicy{MaxMiB: 512}, ImageText: true, Privacy: privacy, Platform: runtime.GOOS, Shortcut: "Alt+V", ExcludedApps: []ExcludedApp{}}
	if !reflect.DeepEqual(settings, wantSettings) {
		t.Fatal(settings)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("permissions", info.Mode(), err)
		}
	}
	if _, err := os.Stat(path + ".part"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("left the part file", err)
	}
}

func TestExportLeavesOutSensitiveEntriesUnlessAsked(t *testing.T) {
	s := transferStorage(t)
	masked := entry(t, "AWS_SECRET="+testAWSKey)
	masked.mask()
	addEntries(t, s, masked, entry(t, "token "+testGitHub), entry(t, "普通文字"))
	for _, secrets := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "export.zip")
		got, err := s.Export(path, exportOptions{Secrets: secrets, Now: time.Now()})
		history := 1
		if secrets {
			history = 3
		}
		if want := (ExportSummary{History: history, Sensitive: 2}); err != nil || got != want {
			t.Fatal(secrets, got, err)
		}
		files := readExport(t, path)
		if items := exportItems(t, files); len(items) != history {
			t.Fatal(secrets, items)
		}
		var manifest exportManifest
		json.Unmarshal(files[manifestName], &manifest)
		if manifest.Secrets != secrets {
			t.Fatal("manifest says", manifest.Secrets)
		}
	}
}

func TestExportGoesOnWhileRecording(t *testing.T) {
	s := transferStorage(t)
	addEntries(t, s, entry(t, "导出前"))
	path := filepath.Join(t.TempDir(), "export.zip")
	late := entry(t, "导出时复制的")
	_, err := s.Export(path, exportOptions{Now: time.Now(), read: func() error {
		done := make(chan error, 1)
		go func() {
			s.History.Add(late)
			done <- s.History.Save()
		}()
		select {
		case err := <-done:
			return err
		case <-time.After(5 * time.Second):
			return errors.New("a copy waited for the export")
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if items := exportItems(t, readExport(t, path)); len(items) != 1 || items[0].Text != "导出前" {
		t.Fatal(items)
	}
	if entries, _ := storedSnapshot(t, s.History); len(entries) != 2 {
		t.Fatal("the copy was lost", entries)
	}
}

func TestExportFailureLeavesNoFile(t *testing.T) {
	s := transferStorage(t)
	addEntries(t, s, entry(t, "内容"))
	path := filepath.Join(t.TempDir(), "export.zip")
	if err := os.WriteFile(path, []byte("上次的导出"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := s.Export(path, exportOptions{Now: time.Now(), read: func() error { return errors.New("磁盘已满") }})
	if err == nil || !strings.Contains(err.Error(), "磁盘已满") {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "上次的导出" {
		t.Fatal("replaced the old file", string(data))
	}
	if _, err := os.Stat(path + ".part"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("left the part file", err)
	}
}
```

（文件开头的 import 加上 `archive/zip`、`bytes`、`encoding/json`、`errors`、`io`、`maps`、`os`、`path/filepath`、`reflect`、`runtime`、`slices`。）

- [ ] **Step 2: 跑测试，确认失败**

Run: `go test -run 'TestExport' .`
Expected: 编译失败，`exportOptions`、`Export` 未定义。

- [ ] **Step 3: 实现**（加到 `transfer_export.go`，import 加 `archive/zip`、`encoding/json`、`io`、`log`、`os`、`runtime`、`time`）

```go
// exportOptions are what an export takes besides the storage.
type exportOptions struct {
	Secrets  bool   // include sensitive entries
	Shortcut string // the summon shortcut, "" when none is registered
	App      string // the Clipnest version
	Now      time.Time
	// read runs inside the snapshot, after the images; tests stall or fail
	// the export there.
	read func() error
}

// Export writes the history, phrases and settings to path. It reads a
// snapshot on its own connection, so recording and panel edits go on, and
// the file appears only once it is complete. The summary counts what it
// wrote; Sensitive counts the sensitive entries whether or not they went in.
func (s *Storage) Export(path string, opts exportOptions) (ExportSummary, error) {
	// The JSON settings are taken before the read starts.
	settings := exportSettings{
		Privacy: s.Privacy.Settings(), Platform: runtime.GOOS,
		Shortcut: opts.Shortcut, ExcludedApps: s.Exclusions.List(),
	}
	db, tx, err := s.snapshot()
	if err != nil {
		return ExportSummary{}, err
	}
	defer db.Close()
	defer tx.Rollback()
	r := &settings.Retention
	err = tx.QueryRow("SELECT max_mib, keep_days, max_entries, ocr_enabled FROM settings WHERE id=1").Scan(&r.MaxMiB, &r.KeepDays, &r.MaxEntries, &settings.ImageText)
	if err != nil {
		return ExportSummary{}, err
	}
	part := path + ".part"
	f, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return ExportSummary{}, err
	}
	err = f.Chmod(0600) // a part file left by a crash may have other permissions
	var sum ExportSummary
	if err == nil {
		sum, err = writeExport(f, tx, settings, opts)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(part, path)
	}
	if err != nil {
		os.Remove(part)
		return ExportSummary{}, err
	}
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		log.Print("导出文件所在的文件夹未能同步：", err)
	}
	return sum, nil
}

// writeExport writes the ZIP. Images go first: a ZIP is written one file at
// a time, and history.json only names them.
func writeExport(w io.Writer, tx *sql.Tx, settings exportSettings, opts exportOptions) (ExportSummary, error) {
	var sum ExportSummary
	zw := zip.NewWriter(w)
	header := func(name string, method uint16) *zip.FileHeader {
		return &zip.FileHeader{Name: name, Method: method, Modified: opts.Now}
	}
	rows, err := tx.Query("SELECT h.id, c.image FROM history h JOIN history_content c ON c.id=h.id WHERE h.kind='image' ORDER BY h.position")
	if err != nil {
		return sum, err
	}
	for rows.Next() {
		var id string
		var png []byte
		if err = rows.Scan(&id, &png); err != nil {
			break
		}
		var fw io.Writer
		if fw, err = zw.CreateHeader(header(imageName(id), zip.Store)); err == nil {
			_, err = fw.Write(png)
		}
		if err != nil {
			break
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err == nil && opts.read != nil {
		err = opts.read()
	}
	if err != nil {
		return sum, err
	}

	fw, err := zw.CreateHeader(header(historyName, zip.Deflate))
	if err != nil {
		return sum, err
	}
	enc := json.NewEncoder(fw)
	enc.SetEscapeHTML(false)
	if _, err := io.WriteString(fw, "["); err != nil {
		return sum, err
	}
	rows, err = tx.Query("SELECT h.id, h.kind, h.source, h.created, h.secret, c.text FROM history h JOIN history_content c ON c.id=h.id ORDER BY h.position")
	if err != nil {
		return sum, err
	}
	for rows.Next() {
		var id, kind, source, secret, text string
		var created int64
		if err = rows.Scan(&id, &kind, &source, &created, &secret, &text); err != nil {
			break
		}
		if sensitive(kind, text, secret) {
			sum.Sensitive++
			if !opts.Secrets {
				continue
			}
		}
		item := exportItem{Text: text, Source: source, Created: created}
		if kind == "image" {
			item = exportItem{Image: imageName(id), Source: source, Created: created}
			sum.Images++
		}
		if sum.History > 0 {
			if _, err = io.WriteString(fw, ","); err != nil {
				break
			}
		}
		if err = enc.Encode(item); err != nil {
			break
		}
		sum.History++
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err == nil {
		_, err = io.WriteString(fw, "]\n")
	}
	if err != nil {
		return sum, err
	}

	texts := []string{}
	rows, err = tx.Query("SELECT text FROM phrases ORDER BY position")
	if err != nil {
		return sum, err
	}
	for rows.Next() {
		var text string
		if err = rows.Scan(&text); err != nil {
			break
		}
		texts = append(texts, text)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return sum, err
	}
	sum.Phrases = len(texts)

	manifest := exportManifest{
		Format: exportFormat, Version: exportVersion, App: opts.App, Platform: runtime.GOOS,
		Exported: opts.Now.UnixMilli(), History: sum.History, Images: sum.Images, Phrases: sum.Phrases,
		Secrets: opts.Secrets && sum.Sensitive > 0,
	}
	for _, doc := range []struct {
		name string
		v    any
	}{{phrasesName, exportPhrases{texts}}, {settingsName, settings}, {manifestName, manifest}} {
		fw, err := zw.CreateHeader(header(doc.name, zip.Deflate))
		if err != nil {
			return sum, err
		}
		enc := json.NewEncoder(fw)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(doc.v); err != nil {
			return sum, err
		}
	}
	return sum, zw.Close()
}
```

注意 `TestExportLeavesOutSensitiveEntriesUnlessAsked` 里带上敏感条目时 `Sensitive` 仍是 2，`manifest.Secrets` 为 true；不带时 `Secrets` 为 false。

- [ ] **Step 4: 跑测试，确认通过**

Run: `go test -run 'TestExport|TestSensitive|TestValidImageName' .`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
gofmt -l . ; go vet .
git add transfer_export.go transfer_export_test.go
git commit -m "feat(transfer): export history, phrases and settings to a ZIP"
```

### Task 3: 读导入文件

**Files:**
- Create: `transfer_import.go`、`transfer_import_test.go`
- Modify: `app_exclusions.go`（抽出 `checkExcludedApps`）

**Interfaces:**
- Consumes: Task 1 的常量、类型、错误。
- Produces: `type importFile struct{ zr *zip.ReadCloser; files map[string]*zip.File; manifest exportManifest }`、`openImportFile(path string) (*importFile, error)`、`(f *importFile) Close() error`、`(f *importFile) eachItem(fn func(item exportItem, ok bool) error) error`、`(f *importFile) entry(item exportItem, now int64) (Entry, error)`、`(f *importFile) phrases() ([]string, error)`、`(f *importFile) settings() (*exportSettings, error)`、`(s *exportSettings) check() error`、`readZipFile(zf *zip.File, limit int64) ([]byte, error)`、`decodeExportJSON(data []byte, v any) error`、`checkExcludedApps(apps []ExcludedApp) ([]ExcludedApp, error)`；测试帮手 `testZip(t, files ...[2]string) string`、`manifestJSON(version int) string`、`importOf(t, history string, more ...[2]string) string`。

- [ ] **Step 1: 写失败的测试** `transfer_import_test.go`

```go
package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// testZip writes files, given as name and content, to a ZIP.
func testZip(t *testing.T, files ...[2]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "import.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, file := range files {
		w, err := zw.Create(file[0])
		if err == nil {
			_, err = w.Write([]byte(file[1]))
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func manifestJSON(version int) string {
	return fmt.Sprintf(`{"format":"clipnest-export","version":%d,"app":"0.1.17","platform":"darwin","exported":1763520000000}`, version)
}

// importOf is an export file with the given history.json and more files.
func importOf(t *testing.T, history string, more ...[2]string) string {
	t.Helper()
	files := [][2]string{{manifestName, manifestJSON(1)}, {historyName, history}, {phrasesName, `{"phrases":[]}`}}
	return testZip(t, append(files, more...)...)
}

func openImport(t *testing.T, path string) *importFile {
	t.Helper()
	f, err := openImportFile(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestImportRefusesOtherFiles(t *testing.T) {
	notZip := filepath.Join(t.TempDir(), "notes.zip")
	os.WriteFile(notZip, []byte("hello"), 0600)
	basic := [][2]string{{historyName, "[]"}, {phrasesName, `{"phrases":[]}`}}
	with := func(manifest string, more ...[2]string) string {
		return testZip(t, append([][2]string{{manifestName, manifest}}, append(more, basic...)...)...)
	}
	for name, c := range map[string]struct {
		path string
		want error
	}{
		"not a ZIP":          {notZip, errNotExport},
		"no manifest":        {testZip(t, basic...), errNotExport},
		"another format":     {with(`{"format":"other","version":1}`), errNotExport},
		"newer":              {with(manifestJSON(2)), errNewerExport},
		"version 0":          {with(manifestJSON(0)), errDamaged},
		"broken manifest":    {with(`{"format":`), errDamaged},
		"two manifests":      {with(manifestJSON(1), [2]string{manifestName, manifestJSON(1)}), errDamaged},
		"manifest too large": {with(`{"format":"clipnest-export","version":1,"app":"` + strings.Repeat("x", maxManifestBytes) + `"}`), errDamaged},
		"no history":         {testZip(t, [2]string{manifestName, manifestJSON(1)}, [2]string{phrasesName, `{"phrases":[]}`}), errDamaged},
	} {
		t.Run(name, func(t *testing.T) {
			f, err := openImportFile(c.path)
			if f != nil {
				f.Close()
			}
			if !errors.Is(err, c.want) {
				t.Fatal(err)
			}
		})
	}
}

func countItems(f *importFile) (int, error) {
	n := 0
	err := f.eachItem(func(exportItem, bool) error { n++; return nil })
	return n, err
}

func TestImportHistoryLimits(t *testing.T) {
	near := strings.Repeat("s", maxHistoryItemBytes-100)
	for name, c := range map[string]struct {
		history string
		items   int
		ok      bool
	}{
		"empty":                {"[]", 0, true},
		"two elements near the limit": {`[{"source":"` + near + `","created":1},{"source":"` + near + `","created":1}]`, 2, true},
		"element too large":    {`[{"text":"` + strings.Repeat("a", maxHistoryItemBytes) + `","created":1}]`, 0, false},
		"whitespace bomb":      {"[" + strings.Repeat(" ", maxHistoryItemBytes+1) + "]", 0, false},
		"too many":             {"[" + strings.Repeat("{},", maxRetainedEntries) + "{}]", maxRetainedEntries, false},
		"not an array":         {`{}`, 0, false},
		"trailing data":        {"[] []", 0, false},
		"broken":               {`[{"text":`, 0, false},
	} {
		t.Run(name, func(t *testing.T) {
			n, err := countItems(openImport(t, importOf(t, c.history)))
			if n != c.items || (err == nil) != c.ok || (err != nil && !errors.Is(err, errDamaged)) {
				t.Fatal(n, err)
			}
		})
	}
}

func TestImportChecksEachItem(t *testing.T) {
	png := testImage(t, 24, 16, 7)
	img, err := newEntry("", png, "截图")
	if err != nil {
		t.Fatal(err)
	}
	notPNG, large := imageName(strings.Repeat("c", 64)), imageName(strings.Repeat("d", 64))
	now := time.Now().UnixMilli()
	history := `[
		{"text":"好的","source":"` + strings.Repeat("长", 100) + `","created":1},
		{"text":"未来","created":` + fmt.Sprint(now+3600_000) + `},
		{"created":1},
		{"text":"x","image":"` + imageName(img.ID) + `","created":1},
		{"text":"   ","created":1},
		{"text":"` + strings.Repeat("a", maxText+1) + `","created":1},
		{"text":"零","created":0},
		{"image":"images/../x.png","created":1},
		{"image":"` + imageName(strings.Repeat("b", 64)) + `","created":1},
		{"image":"` + notPNG + `","created":1},
		{"image":"` + large + `","created":1},
		{"text":5,"created":1},
		5,
		{"image":"` + imageName(img.ID) + `","source":"截图","created":2}
	]`
	f := openImport(t, importOf(t, history,
		[2]string{imageName(img.ID), string(png)}, [2]string{notPNG, "not a png"},
		[2]string{large, strings.Repeat("\x00", maxImage+1)}))
	var kept []Entry
	invalid := 0
	err = f.eachItem(func(item exportItem, ok bool) error {
		if !ok {
			invalid++
			return nil
		}
		e, err := f.entry(item, now)
		if err != nil {
			invalid++
		} else {
			kept = append(kept, e)
		}
		return nil
	})
	if err != nil || invalid != 11 || len(kept) != 3 {
		t.Fatal(err, invalid, kept)
	}
	if kept[0].Source != "" || kept[1].Created != now || kept[2].ID != img.ID || kept[2].Kind != "image" || kept[2].Created != 2 {
		t.Fatal(kept[0].Source, kept[1].Created, kept[2].ID, kept[2].Created)
	}
}

// A header that understates a file's size doesn't let more than the limit in.
func TestImportDoesNotTrustSizesInTheZip(t *testing.T) {
	data := []byte(strings.Repeat("\x00", 9<<20))
	name := imageName(strings.Repeat("e", 64))
	path := filepath.Join(t.TempDir(), "lying.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, file := range [][2]string{{manifestName, manifestJSON(1)}, {historyName, `[{"image":"` + name + `","created":1}]`}, {phrasesName, `{"phrases":[]}`}} {
		w, err := zw.Create(file[0])
		if err == nil {
			_, err = w.Write([]byte(file[1]))
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	// The header says 10 bytes; the file holds 9 MB.
	w, err := zw.CreateRaw(&zip.FileHeader{Name: name, Method: zip.Store, CRC32: crc32.ChecksumIEEE(data), CompressedSize64: uint64(len(data)), UncompressedSize64: 10})
	if err == nil {
		_, err = w.Write(data)
	}
	if err == nil {
		err = zw.Close()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	imp := openImport(t, path)
	err = imp.eachItem(func(item exportItem, ok bool) error {
		if _, err := imp.entry(item, time.Now().UnixMilli()); err == nil {
			t.Error("took a file larger than its header says")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestImportPhrasesFile(t *testing.T) {
	many := `{"phrases":[` + strings.Repeat(`"x",`, maxPhrases) + `"x"]}`
	for name, c := range map[string]struct {
		phrases string
		ok      bool
	}{
		"fine":     {`{"phrases":["一","二"]}`, true},
		"too many": {many, false},
		"broken":   {`{"phrases":[`, false},
		"bad UTF-8": {"{\"phrases\":[\"\xff\"]}", false},
	} {
		t.Run(name, func(t *testing.T) {
			path := testZip(t, [2]string{manifestName, manifestJSON(1)}, [2]string{historyName, "[]"}, [2]string{phrasesName, c.phrases})
			texts, err := openImport(t, path).phrases()
			if (err == nil) != c.ok || (err != nil && !errors.Is(err, errDamaged)) {
				t.Fatal(texts, err)
			}
		})
	}
}

func TestImportSettingsFile(t *testing.T) {
	if s, err := openImport(t, importOf(t, "[]")).settings(); s != nil || err != nil {
		t.Fatal("settings without a file", s, err)
	}
	if _, err := openImport(t, importOf(t, "[]", [2]string{settingsName, "{"})).settings(); !errors.Is(err, errDamaged) {
		t.Fatal(err)
	}
	good := func() *exportSettings {
		return &exportSettings{Retention: RetentionPolicy{MaxMiB: 512}, ImageText: true, Privacy: defaultPrivacy(), Platform: runtime.GOOS, Shortcut: "Alt+V", ExcludedApps: []ExcludedApp{{BundleID: "com.example.vault", Name: "Vault"}}}
	}
	for name, c := range map[string]struct {
		change func(*exportSettings)
		ok     bool
	}{
		"fine":            {func(*exportSettings) {}, true},
		"retention":       {func(s *exportSettings) { s.Retention.KeepDays = -1 }, false},
		"privacy":         {func(s *exportSettings) { s.Privacy.Ignore = []string{"("} }, false},
		"secrets mode":    {func(s *exportSettings) { s.Privacy.Secrets = "" }, false},
		"no platform":     {func(s *exportSettings) { s.Platform = "" }, false},
		"shortcut":        {func(s *exportSettings) { s.Shortcut = "Hyper+V" }, false},
		"no shortcut":     {func(s *exportSettings) { s.Shortcut = "" }, true},
		"duplicate apps":  {func(s *exportSettings) { s.ExcludedApps = append(s.ExcludedApps, s.ExcludedApps[0]) }, false},
		"another platform's shortcut and apps": {func(s *exportSettings) {
			s.Platform, s.Shortcut = "plan9", "Hyper+V"
			s.ExcludedApps = append(s.ExcludedApps, s.ExcludedApps[0])
		}, true},
	} {
		t.Run(name, func(t *testing.T) {
			s := good()
			c.change(s)
			if err := s.check(); (err == nil) != c.ok {
				t.Fatal(err)
			}
		})
	}
}
```

- [ ] **Step 2: 跑测试，确认失败**

Run: `go test -run 'TestImportRefuses|TestImportHistoryLimits|TestImportChecksEachItem|TestImportDoesNotTrust|TestImportPhrasesFile|TestImportSettingsFile' .`
Expected: 编译失败，`openImportFile` 等未定义。

- [ ] **Step 3: 抽出 `checkExcludedApps`**（`app_exclusions.go`）

```go
// checkExcludedApps validates a whole list and returns a sorted copy.
func checkExcludedApps(apps []ExcludedApp) ([]ExcludedApp, error) {
	if len(apps) > maxExcludedApps {
		return nil, errors.New("最多排除 128 个应用")
	}
	seen := make(map[string]bool, len(apps))
	for _, app := range apps {
		if err := app.validate(); err != nil {
			return nil, err
		}
		if seen[app.BundleID] {
			return nil, errors.New("排除应用配置包含重复 Bundle ID")
		}
		seen[app.BundleID] = true
	}
	next := append([]ExcludedApp{}, apps...)
	sortExcludedApps(next)
	return next, nil
}
```

`openAppExclusions` 保留版本和 `len > maxExcludedApps` 的那句检查（报错文字不变），后面的逐个校验和去重换成：

```go
	apps, err := checkExcludedApps(config.Apps)
	if err != nil {
		return nil, err
	}
	a.apps = apps
	return a, nil
```

跑 `go test -run 'Exclu' .`，确认原有测试仍通过。

- [ ] **Step 4: 写 `transfer_import.go`**

```go
package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"
	"unicode/utf8"
)

// importFile is an export file open for reading. Only the names of the
// format are looked at; no other file is read.
type importFile struct {
	zr       *zip.ReadCloser
	files    map[string]*zip.File
	manifest exportManifest
}

// openImportFile opens path and checks its manifest. A file that isn't an
// export, comes from a newer Clipnest or is damaged is refused.
func openImportFile(path string) (*importFile, error) {
	zr, err := zip.OpenReader(path)
	if errors.Is(err, zip.ErrInsecurePath) {
		err = nil // nothing is extracted, and such names are never read
	}
	if err != nil {
		return nil, errNotExport
	}
	f := &importFile{zr: zr, files: map[string]*zip.File{}}
	if err := f.index(); err != nil {
		zr.Close()
		return nil, err
	}
	return f, nil
}

func (f *importFile) Close() error { return f.zr.Close() }

func (f *importFile) index() error {
	if len(f.zr.File) > maxRetainedEntries+maxExtraFiles {
		return damaged("文件数量超过上限")
	}
	for _, zf := range f.zr.File {
		name := zf.Name
		if name != manifestName && name != historyName && name != phrasesName && name != settingsName && !validImageName(name) {
			continue
		}
		if f.files[name] != nil {
			return damaged(name + " 出现了不止一次")
		}
		f.files[name] = zf
	}
	if f.files[manifestName] == nil {
		return errNotExport
	}
	data, err := readZipFile(f.files[manifestName], maxManifestBytes)
	if err == nil {
		err = decodeExportJSON(data, &f.manifest)
	}
	if err != nil {
		return damaged("manifest.json：" + err.Error())
	}
	switch {
	case f.manifest.Format != exportFormat:
		return errNotExport
	case f.manifest.Version > exportVersion:
		return errNewerExport
	case f.manifest.Version < 1:
		return damaged("版本无效")
	case f.files[historyName] == nil || f.files[phrasesName] == nil:
		return damaged("缺少 history.json 或 phrases.json")
	}
	return nil
}

var errTooLarge = errors.New("超过大小上限")

// readZipFile reads a file of the ZIP, at most limit bytes whatever its
// header says. Reading to the end also checks its checksum.
func readZipFile(zf *zip.File, limit int64) ([]byte, error) {
	r, err := zf.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s %w", zf.Name, errTooLarge)
	}
	return data, nil
}

// decodeExportJSON decodes exactly one document. encoding/json would
// otherwise replace invalid UTF-8 with U+FFFD.
func decodeExportJSON(data []byte, v any) error {
	if !utf8.Valid(data) {
		return errors.New("包含无效的 UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(v); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("末尾有多余的内容")
	}
	return nil
}

var errItemTooLarge = errors.New("单条历史超过 4 MB")

// limitedReader fails a read past limit, so one element can't grow the
// decoder's buffer without bound.
type limitedReader struct {
	r        io.Reader
	n, limit int64
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.n >= l.limit {
		return 0, errItemTooLarge
	}
	if int64(len(p)) > l.limit-l.n {
		p = p[:l.limit-l.n]
	}
	n, err := l.r.Read(p)
	l.n += int64(n)
	return n, err
}

// eachItem decodes history.json one element at a time and calls fn with
// each, ok false for an element that isn't an item. Broken JSON, an element
// over maxHistoryItemBytes or more than maxRetainedEntries elements make the
// file damaged; an error from fn stops and is returned.
func (f *importFile) eachItem(fn func(item exportItem, ok bool) error) error {
	r, err := f.files[historyName].Open()
	if err != nil {
		return damaged("history.json：" + err.Error())
	}
	defer r.Close()
	bad := func(err error) error { return damaged("history.json：" + err.Error()) }
	in := &limitedReader{r: r, limit: maxHistoryItemBytes}
	dec := json.NewDecoder(in)
	if tok, err := dec.Token(); err != nil {
		return bad(err)
	} else if tok != json.Delim('[') {
		return bad(errors.New("不是数组"))
	}
	for n := 0; ; n++ {
		// Each element, and the space before it, gets the same allowance.
		in.limit = dec.InputOffset() + maxHistoryItemBytes
		if !dec.More() {
			break
		}
		if n == maxRetainedEntries {
			return bad(errors.New("超过 100 万条"))
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return bad(err)
		}
		var item exportItem
		ok := utf8.Valid(raw) && json.Unmarshal(raw, &item) == nil
		if err := fn(item, ok); err != nil {
			return err
		}
	}
	if tok, err := dec.Token(); err != nil {
		return bad(err)
	} else if tok != json.Delim(']') {
		return bad(errors.New("不是数组"))
	}
	if _, err := dec.Token(); err != io.EOF {
		return bad(errors.New("末尾有多余的内容"))
	}
	return nil
}

// entry checks an item the way a copy is checked and builds its entry. A
// source that isn't valid UTF-8 or is too long is dropped; a time in the
// future becomes now.
func (f *importFile) entry(item exportItem, now int64) (Entry, error) {
	if (item.Text == "") == (item.Image == "") {
		return Entry{}, errors.New("条目要么是文字，要么是图片")
	}
	if item.Created <= 0 {
		return Entry{}, errors.New("时间无效")
	}
	source := item.Source
	if len(source) > maxSourceBytes || !utf8.ValidString(source) {
		source = ""
	}
	var png []byte
	if item.Image != "" {
		zf := f.files[item.Image]
		if !validImageName(item.Image) || zf == nil {
			return Entry{}, errors.New("图片不在文件里")
		}
		var err error
		if png, err = readZipFile(zf, maxImage); err != nil {
			return Entry{}, err
		}
	}
	e, err := newEntry(item.Text, png, source)
	if err != nil {
		return Entry{}, err
	}
	e.Created = min(item.Created, now)
	return e, nil
}

// phrases reads the file's phrase texts. Each text is checked when it is
// added; more than maxPhrases make the file damaged.
func (f *importFile) phrases() ([]string, error) {
	data, err := readZipFile(f.files[phrasesName], maxPhrasesBytes)
	var p exportPhrases
	if err == nil {
		err = decodeExportJSON(data, &p)
	}
	if err != nil {
		return nil, damaged("phrases.json：" + err.Error())
	}
	if len(p.Phrases) > maxPhrases {
		return nil, damaged("常用语超过 500 条")
	}
	return p.Phrases, nil
}

// settings reads the file's settings, nil when it has none. check says
// whether they can be imported.
func (f *importFile) settings() (*exportSettings, error) {
	zf := f.files[settingsName]
	if zf == nil {
		return nil, nil
	}
	data, err := readZipFile(zf, maxSettingsBytes)
	var s exportSettings
	if err == nil {
		err = decodeExportJSON(data, &s)
	}
	if err != nil {
		return nil, damaged("settings.json：" + err.Error())
	}
	s.Privacy = s.Privacy.clone() // a missing ignore list is an empty one
	return &s, nil
}

// check says why the settings can't be imported here. Another platform's
// shortcut and excluded apps aren't imported, so they aren't checked.
func (s *exportSettings) check() error {
	if err := s.Retention.validate(); err != nil {
		return err
	}
	if _, err := s.Privacy.compile(); err != nil {
		return err
	}
	if s.Platform == "" {
		return errors.New("没有写明来自哪个平台")
	}
	if s.Platform != runtime.GOOS {
		return nil
	}
	if s.Shortcut != "" {
		if _, err := parseShortcutBinding(runtime.GOOS, s.Shortcut); err != nil {
			return err
		}
	}
	_, err := checkExcludedApps(s.ExcludedApps)
	return err
}
```

- [ ] **Step 5: 跑测试，确认通过**

Run: `go test -run 'TestImportRefuses|TestImportHistoryLimits|TestImportChecksEachItem|TestImportDoesNotTrust|TestImportPhrasesFile|TestImportSettingsFile|Exclu' .`
Expected: PASS。「too many」一例要一两秒。`TestImportDoesNotTrustSizesInTheZip` 里，Go 的 zip 读到超出头里大小的数据会报 `zip.ErrFormat`；即使不报，9 MB 也超过 `maxImage`，照样被拒。

- [ ] **Step 6: 提交**

```bash
gofmt -l . ; go vet .
git add transfer_import.go transfer_import_test.go app_exclusions.go
git commit -m "feat(transfer): read an export file within fixed limits"
```

### Task 4: 清理预估

**Files:**
- Create: `transfer_prune.go`、`transfer_prune_test.go`

**Interfaces:**
- Produces: `type pruneItem struct{ created, size int64; imported bool }`、`predictPrune(items []pruneItem, p RetentionPolicy, now time.Time) (removed, removedImported int)`；`items` 按 position 排（从新到旧）。

- [ ] **Step 1: 写失败的测试** `transfer_prune_test.go`

```go
package main

import (
	"database/sql"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

func TestPredictPrune(t *testing.T) {
	now := time.Now()
	day := func(n int) int64 { return now.Add(-time.Duration(n) * 24 * time.Hour).UnixMilli() }
	items := []pruneItem{
		{created: day(0), size: 600 << 10},
		{created: day(1), size: 600 << 10, imported: true},
		{created: day(2), size: 10},
		{created: day(9), size: 10, imported: true},
	}
	for _, c := range []struct {
		p                 RetentionPolicy
		removed, imported int
	}{
		{RetentionPolicy{}, 0, 0},
		{RetentionPolicy{KeepDays: 5}, 1, 1},
		{RetentionPolicy{MaxEntries: 2}, 2, 1},
		{RetentionPolicy{MaxMiB: 1}, 3, 2}, // the running total passes 1 MiB at the second entry
		{RetentionPolicy{KeepDays: 5, MaxEntries: 1}, 3, 2},
	} {
		removed, imported := predictPrune(items, c.p, now)
		if removed != c.removed || imported != c.imported {
			t.Errorf("%+v: %d, %d", c.p, removed, imported)
		}
	}
}

// The prediction follows pruneSQL on the same rows.
func TestPredictPruneMatchesPruneSQL(t *testing.T) {
	now := time.Now()
	r := rand.New(rand.NewPCG(1, 2))
	for _, p := range []RetentionPolicy{{}, {KeepDays: 3}, {MaxEntries: 7}, {MaxMiB: 1}, {KeepDays: 5, MaxEntries: 12, MaxMiB: 1}} {
		s := transferStorage(t)
		var items []pruneItem
		err := s.History.db.transaction(func(tx *sql.Tx) error {
			for i := range 40 {
				e, err := newEntry(fmt.Sprintf("%d %s", i, strings.Repeat("x", r.IntN(100_000))), nil, "测试")
				if err != nil {
					return err
				}
				e.Created = now.Add(-time.Duration(r.IntN(10*24)) * time.Hour).UnixMilli()
				items = append(items, pruneItem{created: e.Created, size: int64(e.size())})
				if err := insertEntrySQL(tx, e, int64(i)); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		removed, _ := predictPrune(items, p, now)
		var before, after int
		err = s.History.db.transaction(func(tx *sql.Tx) error {
			tx.QueryRow("SELECT count(*) FROM history").Scan(&before)
			if _, err := pruneSQL(tx, p, now); err != nil {
				return err
			}
			return tx.QueryRow("SELECT count(*) FROM history").Scan(&after)
		})
		if err != nil || removed != before-after {
			t.Errorf("%+v: predicted %d, pruned %d (%v)", p, removed, before-after, err)
		}
	}
}
```

- [ ] **Step 2: 跑测试，确认失败**

Run: `go test -run 'TestPredictPrune' .`
Expected: 编译失败，`pruneItem` 未定义。

- [ ] **Step 3: 实现** `transfer_prune.go`

```go
package main

import "time"

// pruneItem is a history entry as retention sees it.
type pruneItem struct {
	created, size int64
	imported      bool
}

// predictPrune counts the entries pruneSQL deletes from items, given in
// position order, and how many of them are imported. It takes pruneSQL's
// steps in turn: age, then count, then the running total of sizes.
func predictPrune(items []pruneItem, p RetentionPolicy, now time.Time) (removed, removedImported int) {
	cutoff := now.Add(-time.Duration(p.KeepDays) * 24 * time.Hour).UnixMilli()
	limit := int64(p.MaxMiB) << 20
	kept := 0
	var used int64
	for _, it := range items {
		drop := p.KeepDays > 0 && it.created < cutoff
		if !drop {
			drop = p.MaxEntries > 0 && kept >= p.MaxEntries
		}
		if !drop {
			// The size step sums the rows the count step left, itself included.
			kept++
			used += it.size
			drop = p.MaxMiB > 0 && used > limit
		}
		if drop {
			removed++
			if it.imported {
				removedImported++
			}
		}
	}
	return removed, removedImported
}
```

- [ ] **Step 4: 跑测试，确认通过**

Run: `go test -run 'TestPredictPrune' .`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
gofmt -l . ; go vet .
git add transfer_prune.go transfer_prune_test.go
git commit -m "feat(transfer): predict what retention deletes after an import"
```

### Task 5: 第一遍——检查和计数

**Files:**
- Create: `transfer_plan.go`、`transfer_plan_test.go`
- Modify: `privacy.go`（加 `apply`）、`desktop_clipboard.go`（`applyPrivacy` 调用它）、`phrases.go`（加 `texts`、`phraseTexts`）、`transfer_import.go`（加 `importedEntry`）

**Interfaces:**
- Consumes: Task 3 的 `importFile`、Task 4 的 `predictPrune`、Task 1 的 `Storage.snapshot`。
- Produces: `(p *privacyStore) apply(e *Entry) bool`；`itemOutcome`（`itemKept`、`itemInvalid`、`itemSkipped`）、`(f *importFile) importedEntry(item exportItem, ok bool, now int64, privacy *privacyStore) (Entry, itemOutcome)`；`(p *Phrases) texts() ([]string, error)`、`phraseTexts([]Phrase) []string`；`planPhrases(current, file []string) (add []string, duplicate, over, invalid int)`；`entryKey(id string) [32]byte`；`ImportPlan{Manifest exportManifest; New, Existing, Skipped, Invalid, NewPhrases, DuplicatePhrases, PhrasesOver, Pruned, PrunedNew int; Settings *exportSettings; SettingsProblem string; PrunedWithSettings int}`、`(p ImportPlan) nothingNew() bool`、`(s *Storage) PlanImport(path string) (ImportPlan, error)`；测试帮手 `exportOf(t, s, secrets) string`、`editZip`、`rewriteZip(t, path, name, data string) string`、`testZipWithout(t, path, name string) string`、`withSettings(t, path, change) string`、`textEntry(t, text, source, created) Entry`、`savePhrases(t, s, texts...)`、`historyTexts(t, s) []string`、`phraseList(t, s) []string`。

- [ ] **Step 1: 写失败的测试** `transfer_plan_test.go`

```go
package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func exportOf(t *testing.T, s *Storage, secrets bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "export.zip")
	if _, err := s.Export(path, exportOptions{Secrets: secrets, Shortcut: "Alt+V", App: "0.1.17", Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return path
}

// editZip copies the ZIP at path, passing each file through edit, which
// returns its new content, or false to leave the file out.
func editZip(t *testing.T, path string, edit func(name string, content []byte) ([]byte, bool)) string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var files [][2]string
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if content, keep := edit(f.Name, content); keep {
			files = append(files, [2]string{f.Name, string(content)})
		}
	}
	return testZip(t, files...)
}

// rewriteZip copies the ZIP at path with name's content replaced.
func rewriteZip(t *testing.T, path, name, data string) string {
	t.Helper()
	return editZip(t, path, func(n string, content []byte) ([]byte, bool) {
		if n == name {
			return []byte(data), true
		}
		return content, true
	})
}

// testZipWithout copies the ZIP at path without name.
func testZipWithout(t *testing.T, path, name string) string {
	t.Helper()
	return editZip(t, path, func(n string, content []byte) ([]byte, bool) { return content, n != name })
}

func withSettings(t *testing.T, path string, change func(*exportSettings)) string {
	t.Helper()
	f := openImport(t, path)
	s, err := f.settings()
	if err != nil || s == nil {
		t.Fatal(s, err)
	}
	change(s)
	data, _ := json.Marshal(s)
	return rewriteZip(t, path, settingsName, string(data))
}

func textEntry(t *testing.T, text, source string, created int64) Entry {
	t.Helper()
	e, err := newEntry(text, nil, source)
	if err != nil {
		t.Fatal(err)
	}
	e.Created = created
	return e
}

func historyTexts(t *testing.T, s *Storage) []string {
	t.Helper()
	entries, _ := storedSnapshot(t, s.History)
	var texts []string
	for _, e := range entries {
		texts = append(texts, e.Text)
	}
	return texts
}

func phraseList(t *testing.T, s *Storage) []string {
	t.Helper()
	texts, err := s.Phrases.texts()
	if err != nil {
		t.Fatal(err)
	}
	return texts
}

func savePhrases(t *testing.T, s *Storage, texts ...string) {
	t.Helper()
	for _, text := range texts {
		if _, err := s.Phrases.Save("", text); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPlanImportCounts(t *testing.T) {
	to := transferStorage(t)
	addEntries(t, to, textEntry(t, "乙", "这边", 1000))
	savePhrases(t, to, "二")
	privacy := to.Privacy.Settings()
	privacy.Secrets = secretsSkip
	to.Privacy.Save(privacy)

	from := transferStorage(t)
	addEntries(t, from, textEntry(t, "甲", "那边", 500), textEntry(t, "乙", "那边", 2000), imageEntry(t, 3), textEntry(t, "AWS="+testAWSKey, "那边", 3000))
	savePhrases(t, from, "一", "二", "三")
	path := exportOf(t, from, true)

	before := historyTexts(t, to)
	plan, err := to.PlanImport(path)
	if err != nil {
		t.Fatal(err)
	}
	want := ImportPlan{New: 2, Existing: 1, Skipped: 1, NewPhrases: 2, DuplicatePhrases: 1}
	got := plan
	got.Manifest, got.Settings, got.PrunedWithSettings = exportManifest{}, nil, 0
	if got != want || plan.Settings == nil || plan.Manifest.App != "0.1.17" {
		t.Fatalf("%+v", plan)
	}
	if !slices.Equal(historyTexts(t, to), before) || !slices.Equal(phraseList(t, to), []string{"二"}) {
		t.Fatal("the check wrote")
	}
}

func TestPlanImportFillsThePhrasesUp(t *testing.T) {
	to := transferStorage(t)
	for i := range maxPhrases - 1 {
		savePhrases(t, to, fmt.Sprint("已有 ", i))
	}
	from := transferStorage(t)
	savePhrases(t, from, "新的一", "新的二", "新的三")
	plan, err := to.PlanImport(exportOf(t, from, false))
	if err != nil || plan.NewPhrases != 1 || plan.PhrasesOver != 2 {
		t.Fatalf("%+v %v", plan, err)
	}
}

func TestPlanImportPredictsCleanup(t *testing.T) {
	to := transferStorage(t)
	now := time.Now().UnixMilli()
	addEntries(t, to, textEntry(t, "新一", "", now-1000), textEntry(t, "新二", "", now))
	if err := to.History.SetRetention(RetentionPolicy{MaxEntries: 2}); err != nil {
		t.Fatal(err)
	}
	from := transferStorage(t)
	addEntries(t, from, textEntry(t, "旧一", "", now-9000), textEntry(t, "旧二", "", now-8000), textEntry(t, "旧三", "", now-7000))
	plan, err := to.PlanImport(exportOf(t, from, false))
	if err != nil || plan.Pruned != 3 || plan.PrunedNew != 3 || plan.PrunedWithSettings != 0 {
		t.Fatalf("%+v %v", plan, err)
	}
}

func TestPlanImportReportsInvalidSettings(t *testing.T) {
	from := transferStorage(t)
	path := withSettings(t, exportOf(t, from, false), func(s *exportSettings) { s.Retention.KeepDays = -1 })
	plan, err := transferStorage(t).PlanImport(path)
	if err != nil || plan.Settings != nil || plan.SettingsProblem == "" {
		t.Fatalf("%+v %v", plan, err)
	}
}

func TestPlanImportNothingNew(t *testing.T) {
	s := transferStorage(t)
	addEntries(t, s, entry(t, "一样"))
	// Without settings.json there are no settings to offer either.
	path := testZipWithout(t, exportOf(t, s, false), settingsName)
	plan, err := s.PlanImport(path)
	if err != nil || !plan.nothingNew() || plan.Existing != 1 {
		t.Fatalf("%+v %v", plan, err)
	}
}
```

- [ ] **Step 2: 跑测试，确认失败**

Run: `go test -run 'TestPlanImport' .`
Expected: 编译失败，`PlanImport`、`Phrases.texts` 未定义。

- [ ] **Step 3: 把隐私判断搬进 `privacyStore`**

`privacy.go` 加：

```go
// apply applies the ignore rules and the secret setting to a text entry the
// way capture does, masking it in place. It reports whether the entry is
// recorded.
func (p *privacyStore) apply(e *Entry) bool {
	if e.Kind == "image" {
		return true
	}
	if p.Ignored(e.Text) {
		return false
	}
	switch p.Settings().Secrets {
	case secretsSkip:
		return len(findSecrets(e.Text)) == 0
	case secretsMask:
		e.mask()
	}
	return true
}
```

`desktop_clipboard.go` 的 `applyPrivacy` 改成：

```go
// applyPrivacy applies the ignore rules and the secret setting to a text copy,
// masking it in place. It reports whether the copy is still recorded.
func (d *Desktop) applyPrivacy(e *Entry) bool {
	if d.storage == nil {
		return true
	}
	return d.storage.Privacy.apply(e)
}
```

跑 `go test -run 'Capture|Privacy' .`，确认原有测试仍通过。

- [ ] **Step 4: 常用语的文字**（`phrases.go`）

```go
// texts lists the phrase texts in order.
func (p *Phrases) texts() ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.broken != nil {
		return nil, p.broken
	}
	return phraseTexts(p.items), nil
}

func phraseTexts(items []Phrase) []string {
	texts := make([]string, len(items))
	for i, x := range items {
		texts[i] = x.Text
	}
	return texts
}
```

- [ ] **Step 5: `importedEntry`**（加到 `transfer_import.go`）

```go
type itemOutcome int

const (
	itemKept itemOutcome = iota
	itemInvalid
	itemSkipped // by this computer's privacy settings
)

// importedEntry builds the entry an item imports as: checked the way a copy
// is, then judged and masked by this computer's privacy settings the way
// capture judges a copy.
func (f *importFile) importedEntry(item exportItem, ok bool, now int64, privacy *privacyStore) (Entry, itemOutcome) {
	if !ok {
		return Entry{}, itemInvalid
	}
	e, err := f.entry(item, now)
	if err != nil {
		return Entry{}, itemInvalid
	}
	if !privacy.apply(&e) {
		return Entry{}, itemSkipped
	}
	return e, itemKept
}
```

- [ ] **Step 6: 写 `transfer_plan.go`**

```go
package main

import (
	"cmp"
	"database/sql"
	"encoding/hex"
	"slices"
	"time"
)

// ImportPlan is what importing a file would do, counted without writing.
type ImportPlan struct {
	Manifest exportManifest
	// History: entries to add, entries already here, entries this
	// computer's privacy settings don't record, and history items and
	// phrases that fail the checks.
	New, Existing, Skipped, Invalid int
	NewPhrases, DuplicatePhrases    int
	PhrasesOver                     int // new phrases past the 500 limit
	// Pruned is how many entries the current retention policy deletes
	// right after the import, PrunedNew how many of those are imported.
	Pruned, PrunedNew int
	// Settings are the file's settings when they can be imported here, and
	// SettingsProblem why not when it has some that can't.
	Settings        *exportSettings
	SettingsProblem string
	// PrunedWithSettings is Pruned under the file's retention policy.
	PrunedWithSettings int
}

// nothingNew reports whether importing would add nothing.
func (p ImportPlan) nothingNew() bool {
	return p.New == 0 && p.NewPhrases == 0 && p.Settings == nil
}

// importMeta is what the check keeps of an entry: never its content.
type importMeta struct {
	created, size int64
	existing      bool
}

func entryKey(id string) [32]byte {
	var key [32]byte
	hex.Decode(key[:], []byte(id))
	return key
}

// PlanImport reads the file at path and counts what importing it would
// do, without writing anything.
func (s *Storage) PlanImport(path string) (ImportPlan, error) {
	f, err := openImportFile(path)
	if err != nil {
		return ImportPlan{}, err
	}
	defer f.Close()
	plan := ImportPlan{Manifest: f.manifest}
	now := time.Now()
	// The file's entries, in the order they first appear.
	var metas []importMeta
	index := map[[32]byte]int{}
	err = f.eachItem(func(item exportItem, ok bool) error {
		e, outcome := f.importedEntry(item, ok, now.UnixMilli(), s.Privacy)
		switch outcome {
		case itemInvalid:
			plan.Invalid++
			return nil
		case itemSkipped:
			plan.Skipped++
			return nil
		}
		key := entryKey(e.ID)
		if i, found := index[key]; found {
			metas[i].created = max(metas[i].created, e.Created)
			return nil
		}
		index[key] = len(metas)
		metas = append(metas, importMeta{created: e.Created, size: int64(e.size())})
		return nil
	})
	if err != nil {
		return ImportPlan{}, err
	}
	texts, err := f.phrases()
	if err != nil {
		return ImportPlan{}, err
	}
	settings, err := f.settings()
	if err != nil {
		return ImportPlan{}, err
	}
	if settings != nil {
		if err := settings.check(); err != nil {
			plan.SettingsProblem = err.Error()
		} else {
			plan.Settings = settings
		}
	}
	current, err := s.Phrases.texts()
	if err != nil {
		return ImportPlan{}, err
	}
	add, duplicate, over, invalid := planPhrases(current, texts)
	plan.NewPhrases, plan.DuplicatePhrases, plan.PhrasesOver = len(add), duplicate, over
	plan.Invalid += invalid

	db, tx, err := s.snapshot()
	if err != nil {
		return ImportPlan{}, err
	}
	defer db.Close()
	defer tx.Rollback()
	merged, err := mergeHistory(tx, metas, index)
	if err != nil {
		return ImportPlan{}, err
	}
	for _, m := range metas {
		if m.existing {
			plan.Existing++
		} else {
			plan.New++
		}
	}
	plan.Pruned, plan.PrunedNew = predictPrune(merged, s.Status().Retention, now)
	if plan.Settings != nil {
		plan.PrunedWithSettings, _ = predictPrune(merged, plan.Settings.Retention, now)
	}
	return plan, nil
}

// mergeHistory lists the history as the import leaves it, before
// retention, in the order Import numbers it: newest first; among equal
// times the existing entries in their order, then the new ones in file
// order. An existing entry takes the file's time when that is later. It
// marks the metas already here.
func mergeHistory(tx *sql.Tx, metas []importMeta, index map[[32]byte]int) ([]pruneItem, error) {
	items := make([]pruneItem, 0, len(metas))
	rows, err := tx.Query("SELECT id, created, byte_size FROM history ORDER BY position")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var it pruneItem
		if err := rows.Scan(&id, &it.created, &it.size); err != nil {
			return nil, err
		}
		if i, found := index[entryKey(id)]; found {
			metas[i].existing = true
			it.created = max(it.created, metas[i].created)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, m := range metas {
		if !m.existing {
			items = append(items, pruneItem{created: m.created, size: m.size, imported: true})
		}
	}
	slices.SortStableFunc(items, func(a, b pruneItem) int { return cmp.Compare(b.created, a.created) })
	return items, nil
}

// planPhrases picks the file's phrases to append to current: valid ones
// whose text isn't there yet, while there is room.
func planPhrases(current, file []string) (add []string, duplicate, over, invalid int) {
	have := make(map[string]bool, len(current)+len(file))
	for _, text := range current {
		have[text] = true
	}
	for _, text := range file {
		switch {
		case checkPhraseText(text) != nil:
			invalid++
		case have[text]:
			duplicate++
		case len(current)+len(add) >= maxPhrases:
			over++
		default:
			have[text] = true
			add = append(add, text)
		}
	}
	return add, duplicate, over, invalid
}
```

- [ ] **Step 7: 跑测试，确认通过**

Run: `go test -run 'TestPlanImport|Capture|Privacy' .`
Expected: PASS

- [ ] **Step 8: 提交**

```bash
gofmt -l . ; go vet .
git add transfer_plan.go transfer_plan_test.go transfer_import.go privacy.go desktop_clipboard.go phrases.go
git commit -m "feat(transfer): count what an import would do before writing"
```

### Task 6: 第二遍——写入

**Files:**
- Create: `transfer_apply.go`、`transfer_apply_test.go`
- Modify: `image_text.go`（抽出 `setImageTextSQL`）、`app_exclusions.go`（加 `Replace`）

**Interfaces:**
- Consumes: Task 3、5 的全部；`insertEntrySQL`、`replaceSearchSQL`、`purgeSearchSQL`、`pruneSQL`、`savePhrasesSQL`。
- Produces: `(s *sqliteStore) setImageTextSQL(tx *sql.Tx, enabled bool) error`、`(a *appExclusions) Replace(apps []ExcludedApp) error`、`ImportResult{New, Existing, Skipped, Invalid, NewPhrases, DuplicatePhrases, PhrasesOver, Pruned int; Settings bool; Shortcut string; Problems []string}`、`(s *Storage) Import(path string, withSettings bool) (ImportResult, error)`、`(s *Storage) importFile(path string, withSettings bool, beforeCommit func() error) (ImportResult, error)`。

- [ ] **Step 1: 写失败的测试** `transfer_apply_test.go`

```go
package main

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestImportRoundTrip(t *testing.T) {
	from := transferStorage(t)
	img := imageEntry(t, 30)
	// The image is still waiting for recognition here, so its title matches
	// the copy's, which waits too.
	addEntries(t, from, entry(t, "文字"), entry(t, "https://example.com/a"), entry(t, "func main() {\n}"), img)
	savePhrases(t, from, "一", "二")
	privacy := from.Privacy.Settings()
	privacy.ClearSync, privacy.Ignore = false, []string{"^PIN"}
	from.Privacy.Save(privacy)
	from.Exclusions.Add(ExcludedApp{BundleID: "com.example.vault", Name: "Vault"})
	from.History.SetRetention(RetentionPolicy{MaxMiB: 100})

	to := transferStorage(t)
	result, err := to.Import(exportOf(t, from, false), true)
	if err != nil {
		t.Fatal(err)
	}
	if result.New != 4 || !result.Settings || result.Shortcut != "Alt+V" || len(result.Problems) != 0 {
		t.Fatalf("%+v", result)
	}
	want, _ := storedSnapshot(t, from.History)
	got, _ := storedSnapshot(t, to.History)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("history differs", got, want)
	}
	if imageTextStates(t, to.History.db.db)[img.ID] != ocrPending {
		t.Fatal("the image isn't queued for recognition")
	}
	if !slices.Equal(phraseList(t, to), []string{"一", "二"}) {
		t.Fatal(phraseList(t, to))
	}
	if !reflect.DeepEqual(to.Privacy.Settings(), privacy) || !slices.Equal(to.Exclusions.List(), from.Exclusions.List()) || to.Status().Retention != (RetentionPolicy{MaxMiB: 100}) {
		t.Fatal("settings", to.Privacy.Settings(), to.Exclusions.List(), to.Status().Retention)
	}
}

func TestImportMergesByContent(t *testing.T) {
	to := transferStorage(t)
	addEntries(t, to, textEntry(t, "丁", "这边", 120), textEntry(t, "乙", "这边", 100), textEntry(t, "戊", "这边", 300))
	from := transferStorage(t)
	addEntries(t, from, textEntry(t, "甲", "那边", 50), textEntry(t, "乙", "那边", 200), textEntry(t, "gamma ray", "那边", 150), textEntry(t, "戊", "那边", 10))
	path := exportOf(t, from, false)
	result, err := to.Import(path, false)
	if err != nil || result.New != 2 || result.Existing != 2 {
		t.Fatalf("%+v %v", result, err)
	}
	entries, _ := storedSnapshot(t, to.History)
	var got []string
	for _, e := range entries {
		got = append(got, e.Text+"@"+e.Source)
	}
	want := []string{"戊@这边", "乙@那边", "gamma ray@那边", "丁@这边", "甲@那边"}
	if !slices.Equal(got, want) {
		t.Fatal(got)
	}
	v, err := to.History.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	page, err := v.Page("gamma", "", 0, 10)
	v.Close()
	if err != nil || len(page.Entries) != 1 {
		t.Fatal("imported entry isn't searchable", page.Entries, err)
	}
	if again, err := to.Import(path, false); err != nil || again.New != 0 || again.Existing != 4 {
		t.Fatalf("second import %+v %v", again, err)
	}
}

func TestImportAppendsPhrases(t *testing.T) {
	to := transferStorage(t)
	savePhrases(t, to, "二", "四")
	from := transferStorage(t)
	savePhrases(t, from, "一", "二", "三")
	result, err := to.Import(exportOf(t, from, false), false)
	if err != nil || result.NewPhrases != 2 || result.DuplicatePhrases != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	if got := phraseList(t, to); !slices.Equal(got, []string{"二", "四", "一", "三"}) {
		t.Fatal(got)
	}
	stored, err := to.History.db.loadPhrases()
	if err != nil || !slices.Equal(phraseTexts(stored), []string{"二", "四", "一", "三"}) {
		t.Fatal("phrases not in the database", stored, err)
	}
}

func TestImportRedoesPrivacy(t *testing.T) {
	from := transferStorage(t)
	addEntries(t, from, entry(t, "AWS="+testAWSKey)) // recorded while privacy was off
	path := exportOf(t, from, true)
	to := transferStorage(t)
	if _, err := to.Import(path, false); err != nil {
		t.Fatal(err)
	}
	entries, _ := storedSnapshot(t, to.History)
	if len(entries) != 1 || entries[0].Secret == "" {
		t.Fatal("not masked under this computer's settings", entries)
	}
	skip := transferStorage(t)
	privacy := skip.Privacy.Settings()
	privacy.Secrets = secretsSkip
	skip.Privacy.Save(privacy)
	if result, err := skip.Import(path, false); err != nil || result.Skipped != 1 || result.New != 0 {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestImportLeavesSettingsUnlessAsked(t *testing.T) {
	from := transferStorage(t)
	privacy := from.Privacy.Settings()
	privacy.ClearSync = false
	from.Privacy.Save(privacy)
	from.History.SetImageText(false)
	to := transferStorage(t)
	before := to.Privacy.Settings()
	result, err := to.Import(exportOf(t, from, false), false)
	if err != nil || result.Settings || result.Shortcut != "" {
		t.Fatalf("%+v %v", result, err)
	}
	if !reflect.DeepEqual(to.Privacy.Settings(), before) || !to.Status().ImageText {
		t.Fatal("settings changed")
	}
	if _, err := to.Import(exportOf(t, from, false), true); err != nil || to.Status().ImageText {
		t.Fatal("image text stays on", err)
	}
}

func TestImportSkipsAnotherPlatformsShortcutAndApps(t *testing.T) {
	from := transferStorage(t)
	path := withSettings(t, exportOf(t, from, false), func(s *exportSettings) {
		s.Platform = "plan9"
		s.Privacy.ClearSync = false
		s.ExcludedApps = []ExcludedApp{{BundleID: "com.example.vault", Name: "Vault"}}
	})
	to := transferStorage(t)
	result, err := to.Import(path, true)
	if err != nil || result.Shortcut != "" || len(to.Exclusions.List()) != 0 || to.Privacy.Settings().ClearSync {
		t.Fatalf("%+v %v %v", result, err, to.Exclusions.List())
	}
}

func TestImportFailureChangesNothing(t *testing.T) {
	from := transferStorage(t)
	addEntries(t, from, entry(t, "新的"))
	savePhrases(t, from, "新常用语")
	from.History.SetRetention(RetentionPolicy{MaxEntries: 5})
	to := transferStorage(t)
	addEntries(t, to, entry(t, "原来的"))
	savePhrases(t, to, "原来的常用语")
	privacy := to.Privacy.Settings()
	_, err := to.importFile(exportOf(t, from, false), true, func() error { return errors.New("磁盘已满") })
	if err == nil || !strings.Contains(err.Error(), "数据没有改动") {
		t.Fatal(err)
	}
	stored, _ := to.History.db.loadPhrases()
	if !slices.Equal(historyTexts(t, to), []string{"原来的"}) || !slices.Equal(phraseList(t, to), []string{"原来的常用语"}) ||
		!slices.Equal(phraseTexts(stored), []string{"原来的常用语"}) || to.Status().Retention != (RetentionPolicy{MaxMiB: 512}) ||
		!reflect.DeepEqual(to.Privacy.Settings(), privacy) {
		t.Fatal("the failed import left changes")
	}
}

func TestImportCleanupMatchesThePlan(t *testing.T) {
	for _, imported := range []bool{false, true} {
		to := transferStorage(t)
		now := time.Now().UnixMilli()
		addEntries(t, to, textEntry(t, "新一", "", now-1000), textEntry(t, "新二", "", now))
		to.History.SetRetention(RetentionPolicy{MaxEntries: 2})
		from := transferStorage(t)
		addEntries(t, from, textEntry(t, "旧一", "", now-9000), textEntry(t, "中", "", now-500), textEntry(t, "旧三", "", now-7000))
		from.History.SetRetention(RetentionPolicy{MaxEntries: 4})
		path := exportOf(t, from, false)
		plan, err := to.PlanImport(path)
		if err != nil {
			t.Fatal(err)
		}
		result, err := to.Import(path, imported)
		want := plan.Pruned
		if imported {
			want = plan.PrunedWithSettings
		}
		if err != nil || result.Pruned != want {
			t.Fatalf("settings %v: planned %d, pruned %d (%v)", imported, want, result.Pruned, err)
		}
	}
}
```

（文件开头的 import 再加上 `time`。）

- [ ] **Step 2: 跑测试，确认失败**

Run: `go test -run 'TestImport(RoundTrip|Merges|Appends|Redoes|Leaves|Skips|Failure|Cleanup)' .`
Expected: 编译失败，`Import`、`importFile` 未定义。

- [ ] **Step 3: 抽出 `setImageTextSQL`**（`image_text.go`）

把 `SetImageText` 里传给 `transaction` 的函数体搬成方法，`h.db` 换成 `s`，别的不改：

```go
// setImageTextSQL turns recognition on or off in tx: on queues every
// eligible image, off deletes the recognized text.
func (s *sqliteStore) setImageTextSQL(tx *sql.Tx, enabled bool) error {
	var current bool
	if err := tx.QueryRow("SELECT ocr_enabled FROM settings WHERE id=1").Scan(&current); err != nil || current == enabled {
		return err
	}
	// Invalidate in-flight work under the same writer lock as its save.
	// A failed settings transaction only causes a pending image to retry.
	s.imageTextEpoch.Add(1)
	if _, err := tx.Exec("UPDATE settings SET ocr_enabled=? WHERE id=1", enabled); err != nil {
		return err
	}
	if enabled {
		_, err := tx.Exec("UPDATE history SET ocr_state=? WHERE kind='image' AND width*height<=?", ocrPending, maxImageTextPixels)
		return err
	}
	ids := []string{}
	rows, err := tx.Query("SELECT id FROM history_ocr")
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	if _, err := tx.Exec("UPDATE history SET title=? WHERE id IN (SELECT id FROM history_ocr)", defaultImageTitle); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM history_ocr"); err != nil {
		return err
	}
	for _, id := range ids {
		if err := replaceSearchSQL(tx, id); err != nil {
			return err
		}
	}
	return purgeSearchSQL(tx)
}
```

`SetImageText` 变成：

```go
func (h *History) SetImageText(enabled bool) error {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	if h.db == nil {
		return errors.New("迁移到 SQLite 后可识别图片文字")
	}
	err := h.db.transaction(func(tx *sql.Tx) error { return h.db.setImageTextSQL(tx, enabled) })
	if err == nil {
		h.db.imageText.Store(enabled)
	}
	return err
}
```

跑 `go test -run 'ImageText' .`，确认通过。

- [ ] **Step 4: `appExclusions.Replace`**（`app_exclusions.go`）

```go
// Replace swaps in a whole list, as an import does.
func (a *appExclusions) Replace(apps []ExcludedApp) error {
	next, err := checkExcludedApps(apps)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if slices.Equal(a.apps, next) {
		return nil
	}
	return a.save(next)
}
```

- [ ] **Step 5: 写 `transfer_apply.go`**

```go
package main

import (
	"database/sql"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"time"
)

// ImportResult is what an import did.
type ImportResult struct {
	New, Existing, Skipped, Invalid int
	NewPhrases, DuplicatePhrases    int
	PhrasesOver                     int
	Pruned                          int
	// Settings says whether the file's settings were imported. Shortcut is
	// its summon shortcut for the desktop to register, "" for none.
	Settings bool
	Shortcut string
	// Problems are the settings that couldn't be saved after the commit.
	Problems []string
}

// Import merges the file at path into the storage in one transaction,
// checking it all again. With withSettings it also takes the file's
// settings: retention and image text in the transaction, privacy and the
// excluded apps right after it. The caller keeps recording and panel edits
// off meanwhile.
func (s *Storage) Import(path string, withSettings bool) (ImportResult, error) {
	return s.importFile(path, withSettings, nil)
}

// importFile is Import with a hook that runs just before the commit; its
// error rolls the import back.
func (s *Storage) importFile(path string, withSettings bool, beforeCommit func() error) (ImportResult, error) {
	f, err := openImportFile(path)
	if err != nil {
		return ImportResult{}, err
	}
	defer f.Close()
	var settings *exportSettings
	if withSettings {
		settings, err = f.settings()
		if err == nil && settings == nil {
			err = errors.New("文件里没有设置")
		}
		if err == nil {
			err = settings.check()
		}
		if err != nil {
			return ImportResult{}, fmt.Errorf("文件里的设置无法导入：%w", err)
		}
	}
	texts, err := f.phrases()
	if err != nil {
		return ImportResult{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ImportResult{}, errors.New("存储已关闭")
	}
	if !s.sqlite.Load() {
		return ImportResult{}, errors.New("请先迁移到 SQLite，再导入或导出。")
	}
	h, p := s.History, s.Phrases
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	if err := h.saveLocked(nil); err != nil {
		return ImportResult{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.broken != nil {
		return ImportResult{}, p.broken
	}
	add, duplicate, over, invalid := planPhrases(phraseTexts(p.items), texts)
	items := slices.Clone(p.items)
	ids := map[string]bool{}
	for _, x := range items {
		ids[x.ID] = true
	}
	for _, text := range add {
		id := p.newID()
		for ids[id] {
			id = p.newID()
		}
		ids[id] = true
		items = append(items, Phrase{ID: id, Text: text})
	}

	db := h.db
	policy := *db.policy.Load()
	if settings != nil {
		policy = settings.Retention
	}
	now := time.Now()
	result := ImportResult{NewPhrases: len(add), DuplicatePhrases: duplicate, PhrasesOver: over, Invalid: invalid}
	err = db.transaction(func(tx *sql.Tx) error {
		reindexed, err := importHistorySQL(tx, f, s.Privacy, now, &result)
		if err != nil {
			return err
		}
		if err := savePhrasesSQL(tx, items); err != nil {
			return err
		}
		if settings != nil {
			if _, err := tx.Exec("UPDATE settings SET max_mib=?,keep_days=?,max_entries=? WHERE id=1", policy.MaxMiB, policy.KeepDays, policy.MaxEntries); err != nil {
				return err
			}
			if err := db.setImageTextSQL(tx, settings.ImageText); err != nil {
				return err
			}
		}
		var before, after int
		if err := tx.QueryRow("SELECT count(*) FROM history").Scan(&before); err != nil {
			return err
		}
		pruned, err := pruneSQL(tx, policy, now)
		if err != nil {
			return err
		}
		if err := tx.QueryRow("SELECT count(*) FROM history").Scan(&after); err != nil {
			return err
		}
		result.Pruned = before - after
		if pruned || reindexed {
			if err := purgeSearchSQL(tx); err != nil {
				return err
			}
		}
		if beforeCommit != nil {
			return beforeCommit()
		}
		return nil
	})
	if err != nil {
		return ImportResult{}, fmt.Errorf("导入失败：%w，数据没有改动", err)
	}
	p.items = items
	if settings == nil {
		return result, nil
	}
	db.policy.Store(&policy)
	db.imageText.Store(settings.ImageText)
	result.Settings = true
	if err := s.Privacy.Save(settings.Privacy); err != nil {
		result.Problems = append(result.Problems, "隐私设置没能保存："+err.Error())
	}
	if settings.Platform == runtime.GOOS {
		if err := s.Exclusions.Replace(settings.ExcludedApps); err != nil {
			result.Problems = append(result.Problems, "排除的应用没能保存："+err.Error())
		}
		result.Shortcut = settings.Shortcut
	}
	return result, nil
}

// importHistorySQL writes the file's history in tx: new entries the way
// copies are written, and for content already here the later time and its
// source. Then it numbers the whole history newest first again, so imported
// entries sit among the others by time; among equal times the existing ones
// stay first. It reports whether search entries were replaced, leaving old
// terms to purge.
func importHistorySQL(tx *sql.Tx, f *importFile, privacy *privacyStore, now time.Time, result *ImportResult) (bool, error) {
	var next int64
	if err := tx.QueryRow("SELECT coalesce(max(position),0) FROM history").Scan(&next); err != nil {
		return false, err
	}
	reindexed := false
	seen := map[[32]byte]bool{}
	err := f.eachItem(func(item exportItem, ok bool) error {
		e, outcome := f.importedEntry(item, ok, now.UnixMilli(), privacy)
		switch outcome {
		case itemInvalid:
			result.Invalid++
			return nil
		case itemSkipped:
			result.Skipped++
			return nil
		}
		key := entryKey(e.ID)
		var created int64
		var source string
		err := tx.QueryRow("SELECT created, source FROM history WHERE id=?", e.ID).Scan(&created, &source)
		if errors.Is(err, sql.ErrNoRows) {
			// After the existing entries, in file order, until renumbered.
			next++
			seen[key] = true
			result.New++
			return insertEntrySQL(tx, e, next)
		}
		if err != nil {
			return err
		}
		if !seen[key] {
			seen[key] = true
			result.Existing++
		}
		if e.Created <= created {
			return nil
		}
		if _, err := tx.Exec("UPDATE history SET created=?,source=?,source_search=? WHERE id=?", e.Created, e.Source, strings.ToLower(e.Source), e.ID); err != nil {
			return err
		}
		if source == e.Source {
			return nil
		}
		reindexed = true
		return replaceSearchSQL(tx, e.ID)
	})
	if err != nil {
		return false, err
	}
	_, err = tx.Exec(`UPDATE history SET position=r.n FROM
		(SELECT id, row_number() OVER (ORDER BY created DESC, position) AS n FROM history) AS r
		WHERE r.id=history.id`)
	return reindexed, err
}
```

注意：`seen` 同时记「这次插入的」和「已经算过的已有条目」，所以文件里重复的内容既不会重复算新增，也不会把这次插入的算成已有。

- [ ] **Step 6: 跑测试，确认通过**

Run: `go test -run 'TestImport|TestPlanImport|ImageText|Exclu' .`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
gofmt -l . ; go vet .
git add transfer_apply.go transfer_apply_test.go image_text.go app_exclusions.go
git commit -m "feat(transfer): import history, phrases and settings in one transaction"
```

### Task 7: 接入桌面

**Files:**
- Create: `desktop_transfer.go`
- Modify: `preferences.go`（`setShortcut` 抽出 `applyShortcut`）

**Interfaces:**
- Consumes: `Storage.Export`、`Storage.Import`、`ImportResult`。
- Produces: `(d *Desktop) exportData(path string, secrets bool) (ExportSummary, error)`、`(d *Desktop) importData(path string, withSettings bool) (ImportResult, error)`、`(d *Desktop) applyShortcut(accelerator string) error`。

这一层只有主线程调度和窗口操作，依赖 `mygo.RunOnMain`，单元测试跑不了（和 `MigrateStorage`、`changeDataDir` 一样），靠前面几层的测试和真机测试。

- [ ] **Step 1: 抽出 `applyShortcut`**（`preferences.go`）

```go
func (d *Desktop) setShortcut(accelerator string) error {
	if _, err := parseShortcutBinding(runtime.GOOS, accelerator); err != nil {
		return err
	}
	started := false
	mygo.RunOnMain(func() {
		if !d.busy && !d.quitting {
			d.busy, started = true, true
		}
	})
	if !started {
		return errors.New("正在处理其他操作，请稍后再试")
	}
	defer mygo.RunOnMain(func() { d.busy = false })
	return d.applyShortcut(accelerator)
}

// applyShortcut registers accelerator as the summon shortcut and saves it.
// The caller holds d.busy.
func (d *Desktop) applyShortcut(accelerator string) error {
	binding, err := parseShortcutBinding(runtime.GOOS, accelerator)
	if err != nil {
		return err
	}
	var previous string
	mygo.RunOnMain(func() { previous = d.shortcutAccelerator })
	committed, err := replaceShortcut(previous, binding.Accelerator, d.shortcutPath, d.shortcutPressed, shortcutOps{
		register: func(acc string, show func()) (err error) {
			mygo.RunOnMain(func() { err = mygo.GlobalShortcut.Register(acc, show) })
			return
		},
		unregister: func(acc string) { mygo.RunOnMain(func() { mygo.GlobalShortcut.Unregister(acc) }) },
		save:       saveShortcutConfig,
	})
	if !committed {
		return err
	}
	mygo.RunOnMain(func() {
		if d.notice == d.shortcutNotice {
			d.notice = ""
		}
		d.shortcut, d.shortcutAccelerator = binding.Label, binding.Accelerator
		d.shortcutNotice = ""
		if err != nil {
			d.shortcutNotice = err.Error()
		}
		// Win+V was the summon shortcut; the switch keeps it.
		if previous == winVAccelerator {
			d.registerWinV()
		}
		if d.win != nil && d.win.IsVisible() {
			_ = panelChanged.Broadcast(d.snapshot())
		}
	})
	return err
}
```

和原来相比只有两处不同：`d.busy` 由调用方设和清；`previous` 在 `applyShortcut` 里另读一次（调用方已经设了 `d.busy`，别的操作改不了它）。导入的快捷键和当前的相同时，`replaceShortcut` 不重新注册，只重写 `shortcut.json`。

- [ ] **Step 2: 写 `desktop_transfer.go`**

```go
package main

import (
	"errors"
	"reflect"
	"slices"
	"time"

	"github.com/egoist/mygo"
)

// exportData writes the export file. Recording and panel edits go on.
func (d *Desktop) exportData(path string, secrets bool) (ExportSummary, error) {
	var shortcut string
	mygo.RunOnMain(func() { shortcut = d.shortcutAccelerator })
	return d.storage.Export(path, exportOptions{Secrets: secrets, Shortcut: shortcut, App: applicationVersion(), Now: time.Now()})
}

// importData imports a checked file the way a migration runs: recording,
// panel edits and opening the panel wait, and an open panel closes first,
// giving back its read transaction.
func (d *Desktop) importData(path string, withSettings bool) (ImportResult, error) {
	started := false
	mygo.RunOnMain(func() {
		if d.busy || d.quitting {
			return
		}
		d.busy, started = true, true
		if d.win != nil && d.win.IsVisible() {
			d.hide()
		}
	})
	if !started {
		return ImportResult{}, errors.New("正在处理其他操作，请稍后再试")
	}
	defer mygo.RunOnMain(func() { d.busy = false })
	privacy, apps := d.storage.Privacy.Settings(), d.ExcludedApps()
	result, err := d.storage.Import(path, withSettings)
	if err != nil {
		return result, err
	}
	if !reflect.DeepEqual(privacy, d.storage.Privacy.Settings()) || !slices.Equal(apps, d.ExcludedApps()) {
		// Like savePrivacy: the copy on the clipboard was judged by the old rules.
		mygo.RunOnMain(func() { d.lastRevision = d.n.Revision() })
	}
	if result.Shortcut != "" {
		if err := d.applyShortcut(result.Shortcut); err != nil {
			result.Problems = append(result.Problems, err.Error())
		}
	}
	return result, nil
}
```

- [ ] **Step 3: 编译和原有测试**

Run: `go vet . && GOOS=windows go vet . && GOOS=linux go vet . && go test -run 'Shortcut' .`
Expected: 没有输出错误，PASS

- [ ] **Step 4: 提交**

```bash
gofmt -l .
git add desktop_transfer.go preferences.go
git commit -m "feat(transfer): run imports and exports from the desktop"
```

### Task 8: 设置页

**Files:**
- Create: `preferences_transfer.go`、`preferences_transfer_test.go`
- Modify: `preferences.go`（`report`、接上动作）、`preferences_view.go`（动作表、存储页）

**Interfaces:**
- Consumes: `ExportSummary`、`ImportPlan`、`ImportResult`、`Desktop.exportData`、`Desktop.importData`、`Storage.ExportSummary`、`Storage.PlanImport`、`platformName`。
- Produces: `preferencesActions.exportData func()`、`preferencesActions.importData func()`；`(v *preferencesView) transfer(c *ui.Context)`；`exportDialog(s ExportSummary) mygo.MessageOptions`、`exportNotice(path string, s ExportSummary, secrets bool) string`、`importDialog(plan ImportPlan, name string) mygo.MessageOptions`、`importNotice(r ImportResult) string`、`exportFileName(now time.Time) string`；`(p *preferencesWindow) report(progress string, work func() (string, error), resetRetention bool)`、`(p *preferencesWindow) exportData()`、`(p *preferencesWindow) importData()`。

- [ ] **Step 1: 写失败的测试** `preferences_transfer_test.go`

```go
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
	tt := ui.NewTester(v.draw, 620, 580)
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
	tt := ui.NewTester(v.draw, 620, 580)
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
```

- [ ] **Step 2: 跑测试，确认失败**

Run: `go test -run 'TestPreferencesOfferImport|TestPreferencesImportAndExport|TestExportDialog|TestImportDialog|TestTransferNotices' .`
Expected: 编译失败，`exportData` 动作、`exportDialog` 等未定义。

- [ ] **Step 3: 写 `preferences_transfer.go`**

```go
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
```

- [ ] **Step 4: `report` 和接线**（`preferences.go`）

`run` 改成调用 `report`：

```go
// All view state changes are on main. Only persistence work runs in a worker.
func (p *preferencesWindow) run(work func() error, success string, resetRetention bool) {
	p.report("", func() (string, error) { return success, work() }, resetRetention)
}

// report runs work in a worker, showing progress meanwhile and then what
// work says it did. Work that returns "" and no error says nothing, as a
// canceled dialog.
func (p *preferencesWindow) report(progress string, work func() (string, error), resetRetention bool) {
	if p.view.busy || p.desktop.busy || p.desktop.quitting {
		return
	}
	p.view.busy, p.view.notice, p.view.failed = true, progress, false
	p.win.Invalidate()
	go func() {
		success, err := work()
		p.win.Update(func() {
			p.view.busy = false
			p.view.failed = err != nil && !errors.Is(err, errPermissionCanceled)
			if errors.Is(err, errPermissionCanceled) {
				p.view.notice = ""
			} else if err != nil {
				p.view.notice = err.Error()
			} else {
				p.view.notice = success
				if resetRetention {
					p.view.retentionDirty = false
				}
			}
			p.refresh()
		})
	}()
}
```

`p.win.Update` 里的内容和原来的 `run` 一样，只是 `success` 换成 `work` 返回的。

`newPreferencesWindow` 的动作表在 `moveData: p.moveData,` 后面加：

```go
		exportData:     p.exportData,
		importData:     p.importData,
```

- [ ] **Step 5: 动作表和存储页**（`preferences_view.go`）

`preferencesActions` 在 `moveData` 后面加：

```go
	exportData        func()
	importData        func()
```

`storage` 里「历史保留策略」组之后、「清理历史」组之前加一行：

```go
	v.transfer(c)
```

- [ ] **Step 6: 跑测试，确认通过**

Run: `go test -run 'TestPreferences|TestExportDialog|TestImportDialog|TestTransferNotices' .`
Expected: PASS（包括原有的设置页测试）。如果 620×580 的测试器点不到「导出…」（这一组排在页面下方），像 `preferences_view_test.go` 里那一处一样改用 `ui.NewTester(v.draw, 620, 900)`。

- [ ] **Step 7: 提交**

```bash
gofmt -l . ; go vet . ; GOOS=windows go vet . ; GOOS=linux go vet .
git add preferences_transfer.go preferences_transfer_test.go preferences.go preferences_view.go
git commit -m "feat(preferences): export and import from the storage tab"
```

### Task 9: 文档和全量检查

**Files:**
- Modify: `README.md`、`README.en.md`、`docs/roadmap.md`

- [ ] **Step 1: README**

`README.md`「隐私与数据」里数据位置那条后面加一条：

```markdown
- 偏好设置 →「存储」→「导入与导出」可以把历史、常用语和设置导出成一个 ZIP，在另一台电脑（macOS 和 Windows 互通）导入，按内容去重合并。导出文件没有加密；含密码或密钥的历史默认不导出，要勾选才带上。导入时按这台电脑的隐私设置重新判断，设置要勾选才覆盖。
```

`README.en.md` 对应位置加：

```markdown
- Preferences → Storage → Import and export saves the history, phrases and settings to one ZIP, which another computer (macOS or Windows) can import, merging by content. The file isn't encrypted; history with passwords or keys stays out unless you tick the box. An import judges entries by that computer's privacy settings, and replaces the settings only when you tick the box.
```

- [ ] **Step 2: 路线图**

表格第 7 行改成 `| 7 | 导入导出（已完成） | 全部 | 中 | 后面考虑 |`。「### 7. 导入导出」下的三条换成：

```markdown
- 偏好设置 →「存储」→「导入与导出」。设计见 [spec](superpowers/specs/2026-11-19-import-export-design.md)。
- 导出：一个 ZIP，里面是 `manifest.json`、`history.json`、`phrases.json`、`settings.json` 和图片。另开只读连接在一个读事务里读，导出期间照常记录。含密码或密钥的历史默认不导出，导出前说明文件没有加密。
- 导入：先检查和计数，确认框写明新增、已有、跳过、放不下和会被清理的条数；确认后在一个事务里写入，失败就回滚。内容相同的保留较晚的时间，按这台电脑的隐私设置重新打码。设置要勾选才覆盖，别的平台的快捷键和排除的应用不导入。
```

- [ ] **Step 3: 全量检查**

Run: `gofmt -l . && go vet . && GOOS=windows go vet . && GOOS=linux go vet . && go test ./...`
Expected: `gofmt` 没有输出，`go test` 全部 PASS

- [ ] **Step 4: 提交**

```bash
git add README.md README.en.md docs/roadmap.md
git commit -m "docs(transfer): describe import and export"
```

### Task 10: 真机测试（先问用户）

按 `mac-device-test-etiquette` 和 `windows-cloud-desktop-testing` 两条记忆：先问；确认空闲；保存并恢复剪贴板；退出已安装的 Clipnest，测完再打开；用 `MYGO_LDFLAGS="-X main.defaultDataDirName=Clipnest.verify"` 和 `.verify` 标识；测完删掉测试数据。Windows 上用自己的会话、`%USERPROFILE%\clipnest-verify`、`WEBVIEW2_USER_DATA_FOLDER` 和 `$env:APPDATA` 隔开；用 chrome-devtools 浏览器前先确认别的会话没在用。

- [ ] Mac：造几条文字、一张图片、一条打码的密钥、几条常用语，改一项隐私设置。导出两次（不带、带敏感条目），`unzip -l` 看文件列表，看 `manifest.json`、`history.json` 内容和 0600 权限。把同一个文件导回本机：确认框全是「已有」，导入后没有变化。
- [ ] Win11 云桌面：把 Mac 导出的文件经剪贴板传进 VM，导入：看确认框的数字和「快捷键和排除的应用来自 macOS，不会导入」；不勾设置导入一次，再勾设置导入一次，看隐私设置和保留策略变了、快捷键没变；面板里图片显示，识图文字重新识别出来。
- [ ] 清理两边的测试数据，关掉自己的 Chrome。

---

## 自查

- spec 每一节都有任务：格式（1、2）、导出（1、2、8）、导入第一遍（3、5）、确认框（8）、第二遍（6、7）、出错表（3、5、6、8 的测试）、设置页（8）、测试（各任务和 10）。
- spec 原来写「进行中的状态和结果显示在这一组下面」。设置页现有的提示都显示在这一页底部（`noticeText`），这里沿用，spec 已随这份计划改成「显示在这一页底部的提示里」。
- 类型和名字前后一致：`exportItem`、`exportSettings`、`importFile.eachItem(fn func(exportItem, bool) error)`、`importedEntry(..., privacy *privacyStore)`、`predictPrune(items, policy, now)`、`ImportPlan`/`ImportResult` 的字段、`Desktop.applyShortcut`。
