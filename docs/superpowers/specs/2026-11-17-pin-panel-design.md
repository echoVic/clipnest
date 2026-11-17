# 钉住面板设计

日期：2026-11-17
分支：`feat/pin-panel`（基于 `main`）

## 目标

面板现在一失焦就收起，粘贴一条也会收起。整理资料、在几个应用之间来回粘贴时，要反复按快捷键唤起。钉住后面板留在桌面上，切到其他应用也不消失，可以一条接一条地点着粘贴。

做法是给现有面板加一个「钉住」状态，不另开窗口：同一个窗口、同一套会话、同一份选中状态，没钉住时行为完全不变。

## 钉住和取消

- 顶部右侧、暂停按钮之前加图钉按钮，悬停提示「钉住面板（⌘P）」，钉住后高亮，提示改为「取消钉住（⌘P）」。快捷键 ⌘P（Windows、Linux 为 Ctrl+P），历史和常用语页都能用，焦点在搜索框时也生效。
- 再按一次取消钉住：面板留在原处，恢复失焦即收起。
- 面板收起就结束钉住。esc、「关闭」、在面板有键盘焦点时按唤起快捷键、托盘按钮，都会收起面板。下次唤起是普通面板，照旧出现在鼠标所在屏幕的中间。
- 钉住状态不写入设置，重启后不保留。

## 钉住期间

- 面板照旧浮在所有窗口和所有桌面空间上面。按住顶部空白处可以拖动，位置保留到面板收起；按钮、标签页和输入框不触发拖动。没钉住时顶部不能拖。
- 粘贴目标：最近一个在前台、且不是 Clipnest 的应用。底部「粘贴到 X」跟着切换实时更新。
- 粘贴（↩、1–9、「粘贴」按钮、常用语）：内容粘进目标应用，面板不收起，键盘交还给目标应用。想再粘一条，点面板或按唤起快捷键。
- 「仅复制」不交还键盘，和现在一样只复制。
- 唤起快捷键和托盘按钮：面板没有键盘焦点时，只把键盘拿回面板；已经有键盘焦点时收起面板。
- 打开链接：浏览器到前面，面板不收起。
- 开始顺序粘贴：面板不收起，键盘交还给目标应用。横幅照常显示剩余条数和「停止」。
- 顺序粘贴期间，面板有键盘焦点时按 ⌘V 算在 Clipnest 里按的，原样补发、队列不前进；在其他应用里按 ⌘V 照常粘出下一条。
- 没有可粘贴的目标（例如钉住后一直只用 Clipnest 自己的窗口），只复制并提示「已复制，返回目标应用即可粘贴」，和现在一样。

## 实时显示新复制

面板显示期间（钉住或没钉住都算）记下了新的复制，或者自动清理删掉了条目，列表要跟着更新：

- 新条目出现在顶部；搜索词、筛选和当前页签保持不变。
- 选中的那条仍然选中，并停在列表里原来的高度。它已被删除或清理时，改选原来位置的那一条（超出末尾时选最后一条）。
- 对话框、动作菜单开着，或者正在粘贴、删除时先不刷新，关掉或完成后再刷新。

## 实现

### 后端

- `PanelState.Pinned`；`Desktop.pinned` 只在主线程读写。
- `hidden()` 在提前返回之前清掉 `pinned`，所有收起路径都结束钉住。
- 新文件 `desktop_pin.go`：
  - `SetPinned(session, pinned) (PanelState, error)`：会话过期或面板没显示时报「面板已更新，请重新选择」。
  - `Reload(session) (PanelState, error)`：会话一致、面板显示且不忙时换上新的历史快照并广播；否则返回当前状态，不报错。
  - `Locate(session, q, kind, id) (int, error)`：在当前快照里按搜索和筛选找这一条的位置，按 200 条一页最多找 1000 条，找不到返回 -1。
  - 纯函数，供单测：
    - `hideOnBlur(busy, permission, pinned bool) bool`
    - `panelToggle(visible, pinned, focused bool)`：返回打开、聚焦或收起
    - `trackedTarget(current, front targetApp) targetApp`：前台是 Clipnest（PID 0）时保留原目标
- `desktop_panel.go`：`OnBlur` 用 `hideOnBlur`；`show()` 用 `panelToggle`，聚焦时 Windows 同样先 `acquireForeground`。
- 跟踪目标：`monitor` 每 300 ms 那一轮，钉住时更新 `d.target`，名字变了就广播状态。`deliver`、`StartSequence` 用目标前再更新一次。
- `deliver`：钉住时不调 `hide()`，改调 `handOff(target)` 交还键盘后再激活目标。等待就绪的条件加上「面板没有键盘焦点」，交不出去就超时，提示「内容已复制，未能自动粘贴」，绝不粘进面板本身。
- `OpenLink`、`StartSequence`：钉住时不收起面板，`StartSequence` 同样 `handOff`。
- 交还键盘 `handOff`：
  - Windows：`Activate(target)` 用 `SetForegroundWindow` 交出前台，面板保持置顶。
  - macOS：面板拿键盘时不让 Clipnest 成为前台应用。先试 `Activate(target)`，面板仍是 key window 时退一步：收起面板再用 `ShowInactive` 原地显示、不拿键盘，期间用标记让 `hidden()` 不结束会话和钉住。哪种可行以实机为准，实现时最先验证。
- 新事件 `clipnest:history-changed`，内容是会话号。`captureWith` 记下新条目、面板显示时发出；`monitor` 每小时的自动清理成功、面板又在显示时也发（`Cleanup` 不报告删了几条，一小时多刷新一次无妨）。
- 顺序粘贴的 `self` 改为 `sequenceOwnWindow(panelFocused, front)`：面板显示且有键盘焦点，或前台是 Clipnest。
- `go tool mygo generate` 更新 `bindings.ts`。

### 前端

- `index.html` 加图钉按钮和图标；`style.css` 里 `body.pinned .header { --app-region: drag; }` 和按钮高亮。
- `header()` 按 `state.pinned` 更新按钮和 `body.pinned`。
- `keys.js` 加 ⌘P 动作 `pin`；`shortcuts.json`「面板」组加「钉住或取消钉住面板」，现有测试核对清单与按键规则一致。
- 收到 `clipnest:history-changed` 且会话一致：空闲就调 `Reload`，否则记下，空闲后再调。刷新前记下选中条目的 ID、位置和它在可视区里的高度；新快照到达后用 `Locate` 找回，复用删除后的 `restore` 流程，从按位置恢复扩展为按条目恢复，找不到时按位置恢复。
- 浏览器演示实现 `SetPinned`、`Reload`、`Locate`，钉住时演示面板失焦不收起。

## 错误处理

- 刷新时读不到历史，沿用现有提示「历史无法读取：…」。
- 刷新和点击撞在一起，旧会话的调用失败并提示「面板已更新，请重新选择」。前端只在空闲时刷新，这种情况很少。
- `Locate` 出错时按位置恢复，不提示。

## 已知限制

- 钉住的面板 860×570，会一直挡住一块屏幕，只能拖开，不能缩小。
- 面板长时间显示时，历史快照占着一个只读事务，直到下一次刷新。期间写入照常，只是 SQLite 的 WAL 要等刷新后才能回收。

## 测试

- Go 单测（先写测试）：
  - `hideOnBlur`、`panelToggle`、`trackedTarget`。
  - `sequenceOwnWindow` 按键盘焦点判断。
  - `historyView.Locate`：能找到、找不到、带搜索和筛选、超过 1000 条。
  - 新复制在面板显示时才发通知。
- 前端 node 测试：
  - 图钉按钮和 `body.pinned` 跟随状态。
  - ⌘P 的按键规则与快捷键清单。
  - 空闲时才刷新，对话框开着时延后。
  - 刷新后仍选中同一条，找不到时按位置。
  - 演示的 `SetPinned`。
- 实机，macOS（文本编辑）与 Windows（云桌面记事本）：
  - 钉住后切换应用，「粘贴到」跟着变，粘进最后用的应用，面板不收起，键盘回到目标应用。
  - 新复制实时出现，选中不跳。
  - 唤起快捷键拿回键盘；esc 收起并结束钉住；拖动正常。
  - 钉住时顺序粘贴，在其他应用里 ⌘V 前进，在面板搜索框里 ⌘V 不前进。
  - Windows 上 Ctrl+P 不弹出 WebView2 的打印。
