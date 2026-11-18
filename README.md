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
- **顺序粘贴**：连续复制几段内容后，在历史里选中最早的那条，从动作菜单选「从这条起顺序粘贴」。之后每按一次 **⌘V**（Windows 为 **Ctrl+V**）按复制顺序粘贴下一条，到最新一条为止，最多 100 条。菜单栏图标旁（Windows 在托盘提示里）显示剩余条数，可从托盘菜单或面板顶部的横幅停止，10 分钟没有再按会自动结束。仅 macOS 与 Windows，macOS 需要辅助功能授权。
- **钉住面板**：按 **⌘P**（Windows 为 **Ctrl+P**）或点顶部的图钉，面板留在桌面上：切到其他应用、粘贴之后都不收起，粘贴后键盘交还给目标应用。底部「粘贴到」跟着最近用过的应用变。再按唤起快捷键把键盘拿回面板，**Esc** 收起面板并结束钉住。
- 顶部可切换到**常用语**：插入方式与历史相同，**⌘N** 新建、**⌘E** 编辑、**⌘⌫ / Delete** 删除（先确认）、**⌥↑ / ⌥↓** 调整顺序。
- 自动粘贴在 macOS 上需要「辅助功能」授权，未授权时可以只用「仅复制」；Windows 不需要授权。
- 唤起快捷键被占用时会依次尝试备选键（macOS **⌥⇧V**，Windows **Ctrl+Shift+V**），也可以在「偏好设置 → 快捷键」里换一个。
- Windows 上可以在「偏好设置 → 快捷键」打开「用 Win+V 打开 Clipnest」。Clipnest 会从系统剪贴板面板拿走 **Win+V** 并重启资源管理器，任务栏会消失几秒。系统面板里的表情、GIF 和剪贴板历史改用 **Win+.** 打开；Clipnest 未运行时 Win+V 没有反应。关闭开关或卸载 Clipnest，Win+V 会交还给系统。

## 功能

- 记录纯文本、链接、代码和 PNG 图片；自动去重，重新复制的内容移到最前；文件列表不记录。
- SQLite 存储，按容量（默认 512 MiB）、保留天数和条数清理，默认不限天数和条数；常用语单独保存，最多 500 条。
- 搜索正文、标题和来源应用，可按文字、链接、代码、图片筛选；截图里的文字在本机识别后加入搜索，不上传：macOS 用系统 Vision，Windows 用系统自带的文字识别，识别中文需要在 Windows 的语言设置里装中文（含「光学字符识别」）。
- 按应用排除记录；macOS 15.4 起遵守系统的剪贴板记录权限，默认或拒绝时不会在后台读取。
- 识别常见的密钥、令牌（AWS、GitHub、GitLab、Slack、Stripe、Google、JWT、私钥等）和整条复制的银行卡号，默认遮罩后记录：列表、预览和搜索只显示首尾，按「显示」查看原文，粘贴仍是原文；也可改为不记录或不识别。
- 按正则表达式忽略内容，例如 `^\d{6}$` 跳过验证码；密码管理器在复制后 5 分钟内清空剪贴板时，历史里刚记下的那条一并删除（默认开启）。
- 历史每页读取 50 条，列表只渲染可见区域；万条历史也能用方向键和滚轮继续选。
- 面板可以钉在桌面上，在几个应用之间来回粘贴不用反复唤起；面板开着时新复制的内容实时出现在列表里，选中的条目不跳。
- 可在「偏好设置 → 通用」里设为登录时打开，开机后在后台运行，不弹出面板。
- 支持系统减少动态效果、减少透明度与增强对比度设置。
- 面板提供动作菜单与快捷键：仅复制、顺序粘贴、存为常用语、打开链接、删除；偏好设置的「快捷键」页签按场景列出全部按键，Windows/Linux 显示 Ctrl/Alt/Shift。

## 隐私与数据

- 不上传、不联网分析。数据在 macOS 的 `~/Library/Application Support/Clipnest/clipnest.sqlite3` 和 Windows 的 `%AppData%\Clipnest\clipnest.sqlite3`。
- 新安装使用 SQLite（文件权限 0600），不是加密存储；需要时暂停记录或清空历史。
- 单条文本上限 512 KiB，图片 8 MiB、最长边 16000 像素；超过限制跳过。
- 带敏感标记的剪贴板会被忽略（macOS 的 `ConcealedType` 等，Windows 的 `ExcludeClipboardContentFromMonitorProcessing`）。没有标记的内容只能按格式识别：遮罩只影响显示和搜索，数据库里仍是原文；识别不到的格式请用忽略规则。设置保存在数据目录的 `privacy.json`。

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
