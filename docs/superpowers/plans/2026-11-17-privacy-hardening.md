# 隐私补强 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 识别并遮罩密钥、令牌和银行卡号；按正则忽略内容；剪贴板被清空时删除刚记下的那条。

**Architecture:** `secrets.go` 找出密钥位置并生成遮罩文字，`Entry.mask` 用它改写标题、摘要和搜索文本。`privacy.go` 读写 `privacy.json` 并编译忽略规则。`captureFrom` 在读到文字后依次判断忽略规则和密钥，在看到清空时按最近一次记录删除。SQLite 版本 5 增加 `history.secret`，读取详情时据此重新遮罩。偏好设置、面板和浏览器演示读取同一份设置和字段。

**Tech Stack:** Go、MyGo 0.9.0 原生 UI、modernc SQLite、Node test runner。

---

设计来源：`docs/superpowers/specs/2026-11-17-privacy-hardening-design.md`。直接在 `feat/privacy-hardening` 分支按顺序执行，不用 git worktree。

## 1. 识别与遮罩

文件：新增 `secrets.go`、`secrets_test.go`；修改 `history.go`、`search.go`。

- [ ] 先写表驱动测试：每种类型的正例和遮罩结果、多处遮罩、类型取第一处、反例（git 哈希、kebab-case、`task-…`、文中的 16 位订单号、Luhn 不通过、未知前缀）。令牌在测试里运行时拼接。确认失败。
- [ ] 实现 `findSecrets`、`maskSecrets`；`Entry` 加 `Secret`、`Masked`，`mask()` 重算标题、摘要和搜索文本；`summary()` 去掉 `Masked`。
- [ ] 标题、摘要的计算抽成一个函数，`newEntry` 和 `mask` 共用。

## 2. 隐私设置

文件：新增 `privacy.go`、`privacy_test.go`；修改 `migration.go`（`Storage.Privacy`）。

- [ ] 先写测试：默认值、往返、每种校验错误、损坏文件、写入失败不改内存。确认失败。
- [ ] 实现 `openPrivacy`、`Settings`、`Save`、`Ignored`，写法沿用 `appExclusions`。

## 3. 存储版本 5

文件：修改 `sqlite.go`、`sqlite_history.go`、`history_view.go`、`search_results.go`、`history.go`（JSON 读回时按 `secret` 遮罩）；调整 `image_text_test.go`、`sqlite_search_test.go`、`paging_test.go` 的旧版本夹具。

- [ ] 先写测试：新库版本 5；版本 4 升级后有 `secret` 列；遮罩条目的标题、摘要、分页、搜索不含原文、详情遮罩；重新复制改变遮罩状态时更新。确认失败。
- [ ] 实现建表、升级、插入、更新、分页和详情。

## 4. 捕获

文件：修改 `desktop_clipboard.go`、`desktop.go`；测试写在 `desktop_clipboard_test.go`。

- [ ] 先写测试：遮罩、不记录、不识别、正则忽略、清空删除及各个不删的条件。确认失败。
- [ ] 实现：读到文字后判断忽略和密钥；没有格式或只有空白文字时调用 `forgetCleared`；记录成功后保存 `recentCopy`。

## 5. 桌面接口与原生偏好设置

文件：修改 `desktop.go`（`Privacy`、`SavePrivacy`）、`preferences.go`、`preferences_view.go`；新增 `preferences_privacy.go`、`preferences_privacy_test.go`；重新生成 `bindings.ts`。

- [ ] 先写测试：保存后跳过当前剪贴板；无效规则返回错误、设置不变；偏好设置渲染各控件、添加和移除规则调用接口。确认失败。
- [ ] 实现并用 MyGo Tester 截图检查 macOS 浅色、Windows 深色、最小宽度。

## 6. 面板与浏览器演示

文件：修改 `web/app.js`、`web/actions.js`、`web/demo.js`、`web/preferences.js`、`web/index.html`、`web/style.css`；测试写在对应的 `*.test.mjs`。

- [ ] 先写测试：动作菜单不列存为常用语；演示里的密钥条目遮罩、详情带 `masked`；演示偏好设置的保存和校验。确认失败。
- [ ] 实现列表锁图标、预览「显示」、⌘S 提示、演示数据和演示偏好设置。

## 7. 收尾

- [ ] `go test ./...`、`go test -race -count=1 ./...`、`go vet ./...`、`go tool mygo vet .`、`node --test web/*.test.mjs`。
- [ ] 浏览器演示实际操作一遍；macOS 桌面版实测遮罩、忽略规则和清空删除。
- [ ] README 功能列表、CHANGELOG 未发布一节。
