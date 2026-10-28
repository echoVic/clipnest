# 按应用排除记录 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现用户已确认的 v0.1.7 按应用排除记录。

**Architecture:** 独立规则配置由 Storage 持有；剪贴板版本保留来源元数据，正文读取前执行排除。原生隐私页通过应用选择器管理规则，浏览器演示只使用样例。

**Tech Stack:** Go 1.27.3、MyGo 0.7.1、purego/AppKit、现有私有 JSON 写入器、原生 ui.Tester、Node test runner。

---

设计来源：`docs/superpowers/specs/2026-10-28-app-exclusions-design.md`。直接在 `feat/app-exclusions` 分支按顺序执行；用户禁止 git worktree，已有授权无需重新询问执行方式。

## 1. 规则配置

文件：新增 `app_exclusions.go`、`app_exclusions_test.go`；修改 `migration.go`，为 `Storage` 增加规则持有者并在打开历史之前加载规则。

数据契约：

```go
type ExcludedApp struct {
    BundleID string `json:"bundleID"`
    Name string `json:"name"`
}
// 配置版本为 1，apps 为非 nil 的应用数组。
// appExclusions 提供 List() []ExcludedApp、Contains(string) bool、
// Add(ExcludedApp) error、Remove(string) error。
```

- [x] 先覆盖添加、同 ID 改名去重、列表副本、重启、移除和空列表；运行 `go test -run '^TestAppExclusions' -count=1 .` 确认缺失行为失败。
- [x] 使用 `decodePrivateJSON` / `writePrivateJSON` 实现配置；验证版本、128 项上限、标识、名称、重复值；失败不丢弃原文件。
- [x] 测试替换前失败保留旧内存，替换后同步失败更新内存并报告错误。
- [x] 验证 `Storage` 新安装、旧 JSON、迁移、清空历史后规则不丢失；运行规则与存储相关测试。

## 2. 来源快照与采集过滤

文件：修改 `native_darwin.go`、`native_other.go`、`desktop.go`、`desktop_clipboard.go`；扩展 `desktop_clipboard_test.go` 的可控来源。

元数据契约：

```go
type targetApp struct {
    PID int
    Name string
    BundleID string
}
// Desktop 除 lastRevision（已处理版本）外，
// 保存首次观察到的版本和来源，用于延后的权限读取。
```

- [x] 先写文字、图片、主动读取的排除测试，断言测试读取器 `reads == 0` 与历史条数不变；运行 `go test -run '^TestClipboard.*Exclu' -count=1 .` 确认行为失败。
- [x] 为原生前台应用读取 Bundle ID；以命名字段更新现有结构体字面量。
- [x] 首次观察版本时缓存来源；同版本不随切换重新归因。暂停与已处理版本继续直接跳过。
- [x] 有规则时，在正文及图片读取之前排除匹配或无法确认的 macOS 来源，并更新已处理版本。
- [x] 覆盖等待授权时的来源、切换应用、撤销规则、新复制、读取途中版本变化和权限变化。
- [x] 运行 `go test -run '^TestClipboard|^TestNative' -count=1 .`。

## 3. 应用选择与原生设置

文件：新增 `desktop_exclusions.go`、`preferences_exclusions.go`；修改 `preferences.go`、`preferences_view.go`、`native_darwin.go` / `native_other.go`，新增针对应用元数据与设置操作的测试。

- [x] 先用 `ui.NewTester` 验证「隐私」页出现排除分组、名称/标识、添加与移除动作；验证版本与更新入口仍可用。
- [x] 原生 NSOpenPanel 选择单个应用。真实验证发现 MyGo 的 `.app` 扩展名映射为 `com.apple.application-file`，不接受应用包；改用明确的 `com.apple.application-bundle` UTI，保留原生 sheet、默认路径和取消语义。
- [x] 在主线程用 NSBundle 读取标识与显示名称，拒绝无有效 Bundle ID 的对象；不执行应用。测试真实 Finder.app 以及临时无标识包。
- [x] 添加/移除在 busy/quitting 保护下持久化并刷新设置，消耗当前剪贴板版本，避免设置修改引发延迟录入。取消对数据无影响；退出取消 sheet，主线程派发被丢弃时返回错误。
- [x] 分组沿用 `settingsGroup`；列表显示名称和 Bundle ID，移除按钮具有独立语义，长标识和窄窗可用。
- [x] 运行设置和规则相关 Go 测试。

## 4. 浏览器演示与绑定

文件：修改 `web/demo.js`、`web/demo.test.mjs`、`web/preferences.js`、`web/app.js`、`web/index.html`、`web/style.css`；更新生成的 `bindings.ts`。

- [x] 为演示添加/移除、重复添加、取消和历史/常用语不变编写失败测试。
- [x] 演示仅提供明确标注的示例应用；新增绑定返回 `[]ExcludedApp`，列表按返回值刷新，不重建历史会话。
- [x] 添加隐私页圆角分组和示例选择器；以 `textContent` 渲染名称/标识。
- [x] 测试按钮操作失败后恢复可用，规则列表更新时搜索和选择不改变。
- [x] 运行 `node --test web/*.test.mjs`；用 `go tool mygo generate .` 更新绑定，核对实际 CLI 用法后执行。

## 5. 文档、版本与完成验证

文件：修改 `mygo.json`、`README.md`、`CHANGELOG.md`。

- [x] 版本配置更新到 0.1.7，文档标明开发中；记录前台应用归因限制、未知来源跳过、配置位置、已有内容不清理。
- [x] 浏览器验证添加、移除、取消、深浅色与窄窗口；原生元数据与设置测试验证真实路径。使用独立数据目录，不修改用户的剪贴板历史或权限。
- [x] 运行 `go test -race -count=1 ./...`、`node --test web/*.test.mjs`、`go vet ./...`、`go tool mygo vet .`、`git diff --check`。
- [x] 验证 macOS 两种架构及 Linux/Windows 可编译；检查生成绑定与版本显示。
- [x] 按原生存储/过滤与设置、浏览器演示、版本文档组织逻辑提交，保留用户现有未跟踪文件；dev-verify 通过，dev-code-review 的生命周期问题修正并复核为 READY。
- [x] 开发功能与验证边界记录在 [验证记录](../../app-exclusions-validation.md)，保留开发分支；正式 Release/签名分发等待发布指令。发布时同步 echovic-apps 官网。
