# 本地图片文字识别 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 v0.1.8 本地图片文字识别：截图中的文字可以搜索、预览和复制。

**Architecture:** SQLite 版本 4 保存识别状态、文字和开关；History 提供读取下一张、写入结果和开关方法，后台循环注入识别函数。macOS 通过 purego/objc 调用 Vision，其他系统不启动识别。预览、偏好设置和浏览器演示读取同一状态。

**Tech Stack:** Go 1.27.3、MyGo 0.7.1、purego/objc + Vision、modernc SQLite FTS5、Node test runner。

---

设计来源：`docs/superpowers/specs/2026-10-28-image-text-design.md`。直接在 `feat/local-ocr` 分支按顺序执行；用户禁止 git worktree，已有授权无需重新询问执行方式。

## 1. 存储版本 4

文件：修改 `sqlite.go`、`sqlite_history.go`、`search_index.go`、`search_results.go`、`history.go`；新增 `image_text.go`、`image_text_test.go`；调整 v1/v2 升级夹具。

数据契约：

```go
// history.ocr_state: 0 待识别、1 已完成、2 失败；非图片及超过 5000 万像素的图片为 1。
// history_ocr(id, text, search): 只保存识别出的文字；search 为小写文字加第一行标题及尾部空格。
// settings.ocr_enabled 默认 1；sqliteStore 缓存开关供 StorageStatus 显示。
// 搜索文档：coalesce(o.search, c.search_prefix) || h.source_search。
type ImageTextState string // pending / ready / failed / disabled / tooLarge / needsSQLite / unsupported
```

- [x] 先写新库为版本 4、版本 3 升级后图片待识别且文字已完成、JSON 迁移后的图片待识别；运行 `go test -run 'ImageText' -count=1 .` 确认因缺少结构而失败。
- [x] 新建与升级在事务中完成；v1/v2 夹具同步移除版本 4 结构。

## 2. 识别结果与开关

- [x] 先写测试：写入后标题变为第一行、短词和长词搜索命中、详情返回文字与状态；512 KiB 按 UTF-8 截断；重新复制保留结果。
- [x] 识别途中删除、关闭时丢弃结果；失败标记为 2，不重复识别；空结果不建文字行。
- [x] 关闭删除文字、恢复标题、重建并合并索引，FTS 存储不再含有识别片段；重新开启后全部图片待识别。
- [x] `recognizePending` 逐张处理直到没有待识别或收到 stop；识别时不持有写锁。
- [x] 关闭后立即重新开启时，丢弃旧轮次的识别文字和错误并重新识别；回归用例先复现失败，再通过。

## 3. 分块、Vision 与桌面接线

文件：新增 `image_text_tiles.go`、`image_text_darwin.go`、`image_text_other.go`、`preferences_image_text.go` 及测试；修改 `history_view.go`、`desktop.go`、`desktop_panel.go`、`preferences.go`、`preferences_view.go`。

- [x] 先写分块几何（核心区域无缝覆盖、区域不超过 1900 px）和合并测试（重叠去重、跨块拼接保留空格、阅读顺序）。
- [x] 真实 Vision 测试识别提交的中英文代码截图与宽图跨块行；锁定线程、utility QoS，每块独立请求与 autorelease pool；超过 5000 万像素不识别。
- [x] `CopyImageText` 复用 `deliver` 的仅复制路径，不写入历史；`SetImageText` 复用历史修改的忙碌保护并重载可见面板；后台循环每 2 秒检查待识别图片，退出时停止。
- [x] 「隐私 → 图片文字」开关；JSON 存储提示迁移，其他系统说明仅支持 macOS。先用 `ui.NewTester` 写失败测试。

## 4. 前端与演示

文件：新增 `web/image-text.js`、`web/image-text.test.mjs`、`web/demo-image.js`；修改 `web/app.js`、`web/demo.js`、`web/demo.test.mjs`、`web/index.html`、`web/style.css`；用 `go tool mygo generate .` 更新 `bindings.ts`。

- [x] 先写预览各状态、`textContent` 渲染、复制按钮、演示搜索/复制/开关的失败测试。
- [x] 预览在图片下方显示文字与「复制文字」；演示提供带文字的示例截图和隐私页开关。

## 5. 文档、版本与验证

- [x] 版本 0.1.8 开发中；README、CHANGELOG、验证记录；评测脚本放入 `testdata/image-text-eval/`。
- [x] `go test -race -count=1 ./...`、`node --test web/*.test.mjs`、`go vet ./...`、`go tool mygo vet .`、`git diff --check`，四个平台 `CGO_ENABLED=0` 构建。
- [x] 浏览器检查演示；独立数据目录原生检查预览与设置。
- [x] dev-verify、dev-code-review 后按逻辑提交，不发布；各提交从暂存快照独立编译，相关回归测试通过。

实施中的调整（已同步到设计文档）：「复制文字」放在预览底部信息栏，长文字滚动时仍可见；其他系统的预览不显示图片文字，只在偏好设置说明；数据库写入失败后等待 15 次检查（约 30 秒）再试；用发布的识别流程复测整屏语料，召回率为 96.7%，低于原型的 regionOfInterest 方式（98.3%），但大图快约 8 倍，保留现方式；分块识别后归还解码内存。
