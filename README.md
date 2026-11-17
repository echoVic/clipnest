# Clipnest

简体中文 | [English](README.en.md)

一个键盘优先的剪贴板历史面板，支持 macOS 和 Windows。正常复制，随时找回；用数字、方向键、滚轮选择，确认后回到原应用粘贴。

## 下载

- **macOS 13+**：从 [Releases](https://github.com/echoVic/clipnest/releases) 下载通用安装包，Intel 和 Apple 芯片都支持；已用 Developer ID 签名并经 Apple 公证。
- **Windows（amd64）**：同一个 Release 里的 NSIS 安装器，0.1.9 起支持；当前未签名，首次运行需在 SmartScreen 里选「更多信息 → 仍要运行」。

## 使用

- 按 **⌘⇧V**（Windows 为 **Ctrl+Alt+V**）打开面板；**1–9** 直接粘贴，**↑↓ / 滚轮** 切换，**Enter** 确认，**Esc** 关闭。
- **⌘K** 全文搜索，**⌘,**（Windows 为 **Ctrl+,**）打开偏好设置；「快捷键」页签列出面板里的全部按键。
- 选中条目后按 **⌘/**（Windows 为 **Ctrl+/**），或点预览下方的「⋯」，打开这一条的动作菜单。也可以直接按快捷键（Windows 用 Ctrl 代替 ⌘）：**⌘↩** 仅复制；**⌘S** 存为常用语，编辑框会先填好内容，可修改后保存，图片和超过 64 KB 的文字不能存；**⌘O** 用默认浏览器打开链接；**⌘⌫ / Delete** 删除，删除历史后选中顶上来的下一条，焦点在搜索框时照常删字。
- 顶部可切换到**常用语**：插入方式与历史相同，**⌘N** 新建、**⌘E** 编辑、**⌘⌫ / Delete** 删除（先确认）、**⌥↑ / ⌥↓** 调整顺序。
- 自动粘贴在 macOS 上需要「辅助功能」授权，未授权时可以只用「仅复制」；Windows 不需要授权。
- 唤起快捷键被占用时会依次尝试备选键（macOS **⌥⇧V**，Windows **Ctrl+Shift+V**），也可以在「偏好设置 → 快捷键」里换一个。

## 功能

- 记录纯文本、链接、代码和 PNG 图片；自动去重，重新复制的内容移到最前；文件列表不记录。
- SQLite 存储，历史不限条数，按容量（默认 512 MiB）和保留天数清理；常用语单独保存，最多 500 条。
- 搜索正文、标题和来源应用，可按文字、链接、代码、图片筛选；macOS 上用系统 Vision 识别截图里的文字并加入搜索，本机处理，不上传。
- 按应用排除记录；macOS 15.4 起遵守系统的剪贴板记录权限，默认或拒绝时不会在后台读取。
- 历史每页读取 50 条，列表只渲染可见区域；万条历史也能用方向键和滚轮继续选。
- 支持系统减少动态效果、减少透明度与增强对比度设置。
- 面板提供动作菜单与快捷键：仅复制、存为常用语、打开链接、删除；偏好设置的「快捷键」页签按场景列出全部按键，Windows/Linux 显示 Ctrl/Alt/Shift。

## 隐私与数据

- 不上传、不联网分析。数据在 macOS 的 `~/Library/Application Support/Clipnest/clipnest.sqlite3` 和 Windows 的 `%AppData%\Clipnest\clipnest.sqlite3`。
- 新安装使用 SQLite（文件权限 0600），不是加密存储；需要时暂停记录或清空历史。
- 单条文本上限 512 KiB，图片 8 MiB、最长边 16000 像素；超过限制跳过。
- 带敏感标记的剪贴板会被忽略（macOS 的 `ConcealedType` 等，Windows 的 `ExcludeClipboardContentFromMonitorProcessing`）；没有标记的敏感内容无法可靠识别。

## 本地开发

需要 Go（版本见 `go.mod`，MyGo 固定为 0.8.3）和 Node（浏览器测试用）；运行和应用本身不需要 Node、Bun 或网络。

```sh
go run . --data-dir /tmp/clipnest-dev            # 独立数据目录
go run . --web 127.0.0.1:4183                    # 浏览器演示，使用内存示例数据
```

检查（与 CI 一致）：

```sh
go test ./...
go test -race -count=1 ./...
go vet ./...
go tool mygo vet .
node --test web/*.test.mjs
```

构建 macOS 本地预览包：`go tool mygo build -platform darwin/universal -skip-dmg -skip-notarize -sign - .`；Windows 交叉编译：`go tool mygo build -platform windows/amd64`，需要本机安装 NSIS。正式发布用 `scripts/sign-notarize-macos.sh` 完成签名、公证和 stapling。

## 文档

- [存储与搜索改进记录](docs/storage-and-response.md)
- [应用排除验证记录](docs/app-exclusions-validation.md)
- [图片文字验证记录](docs/image-text-validation.md)
- [CHANGELOG](CHANGELOG.md)：各版本变化

## 许可证

[MIT](LICENSE)，© 2026 echoVic。
