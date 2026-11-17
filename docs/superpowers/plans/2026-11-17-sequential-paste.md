# 顺序粘贴 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 从面板选中一条开始，之后每次 ⌘V（Windows 为 Ctrl+V）依次粘出这一条到最新一条。

**Architecture:** `pasteSequence` 是只在主线程运行的状态机，所有系统操作经 `sequenceOps` 注入，测试用假的实现驱动。`Desktop` 负责把它接到 MyGo 全局快捷键、剪贴板、原生按键和托盘上，并在 `PanelState.Sequence` 里报告剩余条数。前端在动作菜单里开始、在横幅里停止。

**Tech Stack:** Go、MyGo 0.9.0（GlobalShortcut、Tray）、purego（macOS CGEvent）、Win32 SendInput、Node test runner。

---

设计来源：`docs/superpowers/specs/2026-11-17-sequential-paste-design.md`。直接在 `feat/sequential-paste` 分支按顺序执行，不用 git worktree。

## 1. 队列状态机

文件：新增 `sequence.go`、`sequence_test.go`。

- [x] 先写测试：开始注册 ⌘V；每次按下取消注册、写入、发送、延时后重新注册；最后一条后结束且不再注册；停止；10 分钟超时；自己窗口在前时补发不前进；`release` 后延时重新注册；注册失败、重新注册失败、写入失败、发送失败；停止或重开后过期回调无效；重新开始替换队列。确认失败。
- [x] 实现 `pasteSequence`、`sequenceOps`、`start`、`press`、`release`、`stop`、`remaining`。

## 2. 读出队列

文件：`sequence.go`、`sequence_test.go`。

- [x] 先写测试：按复制顺序返回；跟随搜索和筛选；选中条目不符时报错；N < 2、超过 100 条、超过 64 MB 时报错。确认失败。
- [x] 实现 `sequenceItems(v, q, kind, index, id)`。

## 3. 原生按键

文件：`native_darwin.go`、`native_windows.go`、`native_other.go`、`native_darwin_test.go`。

- [x] 实现 `pasteAccelerator()` 与 `PasteHeld()`；Windows 按 Ctrl 状态决定发送 V 还是 Ctrl+V，把这个选择抽成纯函数并测试。
- [x] `GOOS=windows go vet .`、`GOOS=linux go vet .`。

## 4. 接入桌面

文件：新增 `desktop_sequence.go`、`desktop_sequence_test.go`；修改 `desktop.go`、`desktop_menu.go`、`main.go`、`bindings.ts`。

- [x] 先写测试：`StartSequence` 的会话校验和错误文案；`deliver` 粘贴前调用 `release`。确认失败。（`StartSequence` 与 `deliver` 依赖 `RunOnMain` 和窗口，无法单测，改为测试可用性判断 `sequenceUnavailable`，其余留给实机验证。）
- [x] 实现 `StartSequence`、`StopSequence`、`PanelState.Sequence`、托盘标题与提示、托盘菜单按状态生成、退出时停止。
- [x] `go tool mygo generate` 更新 `bindings.ts`。

## 5. 面板与浏览器演示

文件：`web/actions.js`、`web/app.js`、`web/demo.js`、`web/index.html`、`web/style.css` 及对应测试。

- [x] 先写测试：菜单项只在历史页且 N ≥ 2 时出现、带条数；演示的 `StartSequence` 和 `StopSequence`；横幅。确认失败。
- [x] 实现菜单项、`perform`、横幅和停止按钮、演示数据。

## 6. 验证与发布

- [x] `go test ./...`、`go test -race -count=1 ./...`、`go vet ./...`、`go tool mygo vet .`、`node --test web/*.test.mjs`。
- [x] 浏览器演示实际操作一遍。
- [x] Windows 实机（云桌面，记事本）：从第 3 条起按 Ctrl+V 依次粘出三条，第 4 次是普通粘贴；在面板列表和搜索框里按 Ctrl+V 不推进；历史不新增；面板横幅、托盘提示「剩 N 条」与托盘菜单「停止顺序粘贴」都对；用横幅停止后 Ctrl+V 粘贴的是剪贴板原内容，用托盘菜单停止后托盘提示复原。
- [ ] macOS 实机：连续粘贴到文本编辑器和不同输入框，结束后 ⌘V 恢复正常。实机测试会接管 ⌘V，先和用户约好时间和目标应用。
- [x] README、CHANGELOG 未发布一节。
