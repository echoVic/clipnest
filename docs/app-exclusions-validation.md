# v0.1.7 应用排除验证

状态：开发版，尚未生成正式签名或公证安装包。设计与实施范围见 [设计](superpowers/specs/2026-10-28-app-exclusions-design.md) 和 [计划](superpowers/plans/2026-10-28-app-exclusions.md)。

## 核心行为

- 规则独立保存在 `excluded-apps.json`；覆盖添加、同 Bundle ID 去重改名、排序、副本、重启、移除、128 项上限及并发读写。
- 覆盖旧 JSON 后端、迁移、SQLite 重启和清空历史后的规则保留；坏版本、重复 ID、非法名称、非普通文件及原始非法 UTF-8 都会拒绝启动并保留原件。
- 故障注入验证替换前保存失败保留旧内存，替换后目录同步失败发布新内存并返回错误。
- 文字、PNG、TIFF 和主动请求读取均在正文读取前匹配规则。测试断言排除时读取次数为零；跳过后切换应用或移除规则不能补录同一版本，新复制重新判断。
- 等待系统授权时保留首次观察到的来源；空规则、暂停、敏感标记、读取前后版本变化与权限变化仍遵守原有行为。
- 主线程派发被 MyGo 丢弃时拒绝修改；取消和 sheet 完成只能返回一次结果。完成时先使原生句柄失效，再返回结果并释放面板，退出不能再次访问已完成面板。规则发布后消耗当前版本，不调用历史会话重载。

规则和过滤测试先在缺少行为时失败。临时关闭读取前过滤后，文字和两种图片、延后授权以及未知来源的回归测试再次失败，恢复后通过。

## 原生选择器与设置

Finder.app 的真实 NSBundle 名称和 Bundle ID 读取通过；无标识及非字符串标识包被拒绝，不启动所选应用。真实类型查询发现：MyGo 0.7.1 对 `.app` 扩展名解析得到 `com.apple.application-file`，而应用包为 `com.apple.application-bundle`，二者不符合。因此选择器使用明确的应用包 UTI，并保留原生 NSOpenPanel sheet。

原生验收入口为 `scripts/test-native-exclusions.py`，宿主和 AppKit 驱动位于 `testdata/native-exclusions/`。2026-10-28 在 macOS 26.6.2 上实际执行，7 项检查全部通过：

| 进程 | 验收行为 |
| --- | --- |
| `manage` | 原生取消后规则、文件和剪贴板版本不变；实际选择应用并确认，校验名称、Bundle ID、0600 文件权限、设置显示及当前版本消耗 |
| `restart` | 使用上一进程的同一数据目录恢复规则；再次实际确认同一应用，不重复也不消耗版本；通过原生移除按钮清除磁盘和界面规则，并消耗当前版本 |
| `quit-open` | 选择器仍打开时退出，原生取消路径使添加工作线程正常返回 |
| `quit-confirm` | 实际点击“添加”，在 AppKit URL 验证回调中排队原生退出，添加工作线程正常返回，无崩溃或竞态报告 |

每项同时核对暂停状态、历史正文、常用语和既有会话。历史检查既读取原有会话快照，也打开完成写入后的新数据库视图，避免旧 SQLite 快照掩盖真实数据丢失。退出超时报告独立于工作线程持有的报告，避免失败路径出现报告读写竞态。

宿主使用独立 Bundle ID、临时 SQLite 数据和临时应用包，暂停采集，不启动监听或全局快捷键。启动器复制应用源码，仅将选择器的初始 `/Applications` 目录改为临时应用所在目录；应用包类型过滤、原生完成回调、元数据读取、规则持久化和版本消耗均使用应用实现。测试驱动通过公开 `NSOpenSavePanelDelegate` 观察用户确认的真实路径，返回 YES，不保留面板，不合成选择结果；退出场景只给原有设置添加动作包一层工作线程完成通知。读取设置的辅助功能树前完成 AppKit 已排队的绘制，不注入设置刷新。

系统文件列表由独立 Open/Save Panel Service 托管。复跑时由操作者或可信外部 UI 工具实际选中临时应用并点击“添加”；宿主输出 `*.phase.json` 表示等待确认，其他动作及断言自动执行。每个进程最多一次实际确认。默认确认期限为 180 秒，可用 `--ui-timeout` 延长；超时或退出失败返回非零并保留报告、日志和仅含测试窗口的辅助功能诊断。

```sh
python3 scripts/test-native-exclusions.py --ui-timeout 600 --output build/native-exclusions-e2e
python3 scripts/test-native-exclusions.py --negative-control --ui-timeout 600 --output build/native-exclusions-negative
```

第二条是错误对照，预期退出码为 1：实际选择和确认仍完成，但刻意错误的预期 Bundle ID 触发 `confirmed selection mismatch`。本轮已验证该失败，不会把仅关闭面板或已有规则视为确认成功。正向报告保存在 `build/native-exclusions-verified-final/native-exclusions-results.json`，错误对照报告保存在 `build/native-exclusions-negative/native-exclusions-results.json`；构建与运行产物不提交到 Git。

`ui.NewTester` 验证隐私分组、名称和 ID、添加与按 ID 移除、忙碌禁用、非 macOS 不提供选择器、版本和更新入口及保留策略草稿。520 像素宽的深浅色渲染已检查，长 Bundle ID 正常换行。原生布局检查使用 UI 渲染器；真实窗口另外核验浅色设置页。

## 浏览器演示

61 项前端测试通过。真实浏览器检查示例应用添加、重复添加改名、取消、移除、深浅色和窄窗口；搜索、当前选择、预览及未保存保留策略维持原样。演示只管理内存示例，不访问真实应用或剪贴板。

## 最终命令

以下命令在本轮开发中通过：

```sh
go test -race -count=1 ./...
node --test web/*.test.mjs
go vet ./...
go tool mygo vet .
go tool mygo generate .
git diff --check
```

`CGO_ENABLED=0 go build` 分别检查 darwin/arm64、darwin/amd64、linux/amd64、windows/amd64，通过。交叉编译不代表在 Intel、Linux 或 Windows 上完成运行验收。

来源仍按检测时的前台应用判断，不能保证后台复制或快速切换时识别真实写入者。正式发布时仍需按项目流程签名、公证、验证更新包，并同步独立 echovic-apps 官网的中英文说明及版本兜底信息。
