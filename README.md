# Boku Desktop

Go + MyGo，窗口界面由 MyGo 自己绘制（native UI，`ui` 包）。

托盘上的一个图标，点开是一张悬浮小卡片：面板（API 余额、额度）与工具
（划词翻译、剪贴板历史）都住在里面。

## 构建与运行

需要 Go 1.27 或更新。命令入口只有 `go` 与 `go tool mygo`——没有 npm、
没有 vite、没有第二套构建编排层。

```bash
go tool mygo dev        # 开发：构建 dev 版本、启动、改动 .go 后重建重启
go tool mygo build      # 生产构建与打包，产物在 build/<os>-<arch>/
go tool mygo doctor     # 检查本机环境
```

验证（CI 跑的就是这四条）：

```bash
gofmt -l .              # 应无输出
go vet ./...
go tool mygo vet .      # 检查把构建期 element 存进状态或 goroutine 的写法
go test ./...
```

所有代码在 `CGO_ENABLED=0` 下编译，不引入需要 cgo 的依赖。
Go 模块与预编译 SQLite 库从模块代理下载，默认 `GOPROXY` 即可；
国内网络下可 `go env -w GOPROXY=https://goproxy.cn,direct`。

## 形态

```text
托盘图标  ← 常驻。左键点击 开 / 合 快捷窗口
快捷窗口  ← mygo.NewFlyout，锚定托盘，竖版，点别处自动关闭
设置窗口  ← mygo.NewWindow，懒创建，只有点「设置」才打开
```

刻意**不设常驻托盘菜单**：MyGo 在 Windows / macOS 上「有菜单时左键弹出菜单」，
那样左键就点不出悬浮窗。菜单只在**右键**弹出期间挂上，用完立刻摘掉，里面是
「打开 Boku」与「退出 Boku」（托盘应用没有主窗口可关，必须有退出入口）。
Linux 例外：AppIndicator 只显示菜单、不报告点击，因此在 Linux 上菜单是常驻的。

托盘与悬浮窗的四条硬约束：

1. 默认不挂托盘菜单，否则左键永远点不出悬浮窗。
2. 每次开合都新建 flyout，复用 `Hidden` 的窗口不会被重新定位到托盘旁。
3. 必须注册 `App.OnWindowAllClosed`，否则悬浮窗一关进程就退出。
4. 托盘图标用 `go:embed`，不运行时读盘。

## 目录

```text
main.go        装配：应用名、生命周期监听、托盘、悬浮窗。唯一装配点
mygo.json      应用名、identifier、版本、产物目录
resources/     icon.png：应用图标，由 mygo build 打进应用包
internal/
  app/         Presentation：视图状态、托盘、悬浮窗、View
  app/assets/  嵌入的图标（编译期需要，不能运行时读盘）
  domain/      实体、值对象、端口接口、错误码、Clock。零依赖
  kit/         纯函数（金额、文本、语言、脱敏）。只依赖 domain
  platform/    外部世界适配：httpc / sysclip / selection / atomicfile
  contract/    厂商身份常量：providerId、凭据 key、申请页
  panel/       功能：面板（服务 + 注册表 + 每个厂商一棵子树）
  clipboard/   功能：剪贴板历史（服务 + 轮询 + 历史与图片存储）
  translate/   功能：划词翻译（openai 协议 + 各后端）
  credential/  功能：凭据（服务 + 存储）
  settings/    功能：应用设置的存储
testdata/      适配器测试用的原始响应样本
```

依赖方向：

```text
main → 各功能包 → domain / kit / platform / contract
app（Presentation）→ 各功能包
```

不得反向：功能包之间**不互相 import**，它们只在 `internal/domain` 的接口上相遇
（`SecretStore`、`ClipboardDevice`…），由 `main.go` 注入实现。
`domain`、`contract/*` 不 import 任何其它 internal 包；`kit` 只依赖 `domain`。

第三方数据结构的差异收口在厂商包里：

```text
internal/panel/<厂商>/client.go  Fetch(HTTP)   → FetchResult(原始 JSON)
internal/panel/<厂商>/adapter.go Normalize     → Snapshot(统一指标)
```

Adapter 是纯函数，必须有 fixture 测试且包含**畸形响应**用例——断言返回
`ErrProviderBadResponse`，而不是产出 0。

## 界面约定

MyGo native UI 是即时模式，视图每帧重新构建：

- 视图函数必须无副作用。查询事件用 `.Clicked()` / `.Changed()` / `.Submitted()`，
  或用 `.OnClick(...)` 把动作延后到本帧构建结束。
- 控件值用 `.Bind(&state.Field)` 直接绑定到状态字段。
- 需要跨帧身份的控件用 `c.Key(...)`——**必须在构造之前**设到 context 上；
  元素构建完再调 `.Key()` 会 panic。
- 构建期 `ui.Element` 只在一帧内有效，不要存进状态或传给 goroutine。
- **按钮、菜单、托盘与全局快捷键的回调都在主线程**：只启动 goroutine，
  绝不等待网络；后台结果用 `Window.Update` 回到主线程后再写状态。
  隐藏的窗口可能一帧都不绘制，直接改状态可能永远不反映到界面上。

测试不需要窗口，用 `ui.NewTester` 在内存里渲染并模拟输入：

```go
tt := ui.NewTester(app.View, 320, 300)
_ = tt.Click("刷新")
if !tt.HasText("¥42.10") { /* ... */ }
```

改动悬浮窗 / 设置窗口的布局时，可以把两种窗口渲染成 PNG 人工比对
（`internal/app/render_manual_test.go`，不设变量就跳过，不写任何文件）：

```powershell
$env:BOKU_RENDER_DIR='<某个目录>'
go test ./internal/app/ -run TestRenderScreensForInspection
```

## 数据与凭据

- 数据库与凭据都在 `mygo.App.Path(mygo.PathUserData)` 之下。
- 数据库只保存凭据的引用（`secretRef`），**不保存明文**。
- 凭据当前存明文 `secrets.json`（权限 0600）。Windows 上这不等于 ACL，
  因此**不是安全边界**；加密密钥库（主密码）尚未实现，设置界面已如实告知用户。
- 日志写在用户数据目录下的 `logs/`，一天一个文件、保留最近 30 天。
- 测试只用 `testdata/` 的向量，不得使用真实凭据。

## 关于这个仓库

这是 Boku 的 Desktop 客户端。同一产品的 Apple 端（SwiftUI）是独立的、
尚未开源的实现，与本仓库共享领域契约：相同的实体与枚举取值、相同的
`providerId` 与凭据 key 命名、相同的密钥库格式与 KDF 参数、相同的金额与
时长规则、相同的错误语义。`testdata/providers/` 的响应样本就是这份契约的
可执行形式——两端读取同一份向量，断言相同的 Snapshot。

## License

MIT，见 [LICENSE](./LICENSE)。
