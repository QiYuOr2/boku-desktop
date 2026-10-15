# Boku Desktop

**托盘上的一个图标，点开是一张悬浮小卡片。**

面板（API 余额、额度）与工具（划词翻译、剪贴板历史）都住在里面。核心原则是
「一个入口，看见状态，用完就走」——没有主窗口，不进 Dock，不留后台标签页。

> 外部服务失败只能让一张卡片失败，不能让应用或本地数据失败。

用 Go 与 [MyGo](https://github.com/egoist/mygo) 写的原生桌面应用：界面由 MyGo
自己绘制（Windows / Linux），不打包 webview，也不引入前端工具链。

## 状态

早期开发中，v0.1.0。已经能用的是托盘、悬浮窗、加密前过渡的凭据存储、
DeepSeek 余额面板、Codex 额度面板与划词翻译；其余在路线图里。

| 能力 | 状态 |
| --- | --- |
| 托盘 + 悬浮快捷窗口 + 设置窗口 | ✅ |
| DeepSeek 余额面板 | ✅ |
| Codex 额度面板（读本机 CLI 登录） | ✅ |
| 划词翻译 | ✅ |
| 剪贴板历史 | ✅ |
| 加密密钥库（主密码） | 🚧 未做，凭据当前是明文 |
| 软件订阅 / 读书时长 / 游戏时长 | 计划中 |

已知限制：

- 凭据目前明文存在 `secrets.json`（权限 0600，Windows 上不等于 ACL），
  **不是安全边界**。加密密钥库还没做，设置界面已如实告知用户。
- 快照缓存只在内存，重启后第一次打开需要联网。
- 只有一个全局快捷键（划词翻译）。用来开合悬浮窗的 `Ctrl+Alt+B` 还没做，
  打开悬浮窗目前只能点托盘左键。
- 悬浮窗位置、托盘左键、点击别处自动关闭需要有图形会话的机器上验收。

## 构建

需要 **Go 1.27 或更新**。命令入口只有 `go` 与 `go tool mygo`——
没有 npm、没有 vite、没有第二套构建编排层。

```bash
git clone https://github.com/QiYuOr2/boku-desktop.git
cd boku-desktop

go tool mygo dev        # 开发：构建 dev 版本、启动，改动 .go 后重建重启
go tool mygo build      # 生产构建与打包，产物在 build/<os>-<arch>/
go tool mygo doctor     # 检查本机环境
```

所有代码在 `CGO_ENABLED=0` 下编译，不引入需要 cgo 的依赖。

国内网络下如果拉取依赖慢：

```bash
go env -w GOPROXY=https://goproxy.cn,direct
```

## 开发

提交前跑这四条，CI 跑的是同一套：

```bash
gofmt -l .              # 应无输出
go vet ./...
go tool mygo vet .      # 检查把构建期 element 存进状态或 goroutine 的写法
go test ./...
```

改动界面时不需要窗口——MyGo 的 `ui.NewTester` 能在内存里渲染并模拟输入：

```go
tt := ui.NewTester(app.View, 320, 300)
_ = tt.Click("刷新")
if !tt.HasText("¥42.10") { /* ... */ }
```

视图与布局本身**不写像素断言测试**，靠运行时验收。测试要测的是「做了什么」，
不是「画成什么样」。

## 架构

单个 Go 进程，顶层**按功能分包**：每个功能自己拥有它的服务、存储与它认识的厂商。
跨功能共享的只有三样：领域契约、纯函数、外部世界适配。

```text
internal/domain            实体、值对象、端口接口、错误码、Clock。零依赖
internal/kit               纯函数（金额、文本、语言、脱敏）。只依赖 domain
internal/platform          外部世界适配：httpc / sysclip / selection / atomicfile
internal/contract/<厂商>    厂商身份常量：providerId、凭据 key、申请页
        ↑
internal/panel             功能：面板（服务 + 注册表 + 每个厂商一棵子树）
internal/clipboard         功能：剪贴板历史（服务 + 轮询 + 历史与图片存储）
internal/translate         功能：划词翻译（openai 协议 + 各后端）
internal/credential        功能：凭据（服务 + 存储）
internal/settings          功能：应用设置的存储
        ↑
internal/app               Presentation：视图、窗口、托盘、快捷键
main.go                    唯一装配点
```

三条规则撑起这套结构：

1. **功能之间不互相 import。** 它们只在 `internal/domain` 的接口上相遇
   （`SecretStore`、`ClipboardDevice`…），由 `main.go` 注入实现。
2. **依赖方向单向。** 功能可以依赖 `domain` / `kit` / `platform` / `contract`，
   反过来一律不许；`domain` 与 `contract/*` 不 import 任何其它 internal 包。
3. **出网收口。** 只有 `internal/platform/httpc` 与厂商包引入 `net/http`，
   其他任何地方都不发请求。

第三方数据结构的差异收口在每个厂商自己的包里，provider 与 adapter 成对存在：

```text
internal/panel/<厂商>/client.go   Fetch(HTTP)  → FetchResult(原始 JSON)
internal/panel/<厂商>/adapter.go  Normalize    → Snapshot(统一指标)
```

Adapter 必须是纯函数——不读时钟、不开文件、不发请求——因此可以离线测试，
且必须有包含**畸形响应**的 fixture 用例：断言返回 `ErrProviderBadResponse`，
而不是把失败画成 `0`。

新增一个数据源的完整流程见 `AGENTS.md` 的 Provider / Adapter 规则。

## 界面约定

MyGo 的 native UI 是即时模式，视图每帧重新构建。几条硬约束：

- 视图函数必须无副作用；查询事件用 `.Clicked()` / `.Changed()` / `.Submitted()`，
  或用 `.OnClick(...)` 把动作延后到本帧构建结束。
- 控件值用 `.Bind(&state.Field)` 绑定到状态字段。
- 需要跨帧身份的控件用 `c.Key(...)`，**必须在构造之前**设到 context 上——
  元素构建完再调 `.Key()` 会 panic。
- 构建期的 `ui.Element` 只在一帧内有效，不要存进状态或传给 goroutine。
- **按钮、菜单、托盘与全局快捷键的回调都在主线程**：只启动 goroutine，
  绝不等待网络；后台结果用 `Window.Update` 回到主线程后再写状态。

托盘与悬浮窗还有四条容易踩的约束：

1. 默认不挂托盘菜单，否则左键永远点不出悬浮窗（MyGo 有菜单时左键弹菜单）。
2. 每次开合都新建 flyout；复用 `Hidden` 的窗口不会被重新定位到托盘旁。
3. 必须注册 `App.OnWindowAllClosed`，否则悬浮窗一关进程就退出。
4. 托盘图标用 `go:embed`，不运行时读盘。

## 数据与凭据

- 数据库与凭据都在 `mygo.App.Path(mygo.PathUserData)` 之下。
- 数据库只保存凭据的引用（`secretRef`），**不保存明文**。
- 凭据当前存明文 `secrets.json`（权限 0600）。见上面「已知限制」。
- 日志写在用户数据目录下的 `logs/`，一天一个文件、保留最近 30 天。
- 日志与错误信息输出凭据前必须 `redact`；错误只允许含 key 名，不含值。
- 测试只用 `testdata/` 里的向量，不使用真实凭据。

## 目录

```text
main.go        装配：应用名、生命周期监听、托盘、悬浮窗。唯一装配点
mygo.json      应用名、identifier、版本、产物目录
resources/     icon.png：应用图标，由 mygo build 打进应用包
internal/      app / domain / kit / platform / contract / panel / clipboard
               / translate / credential / settings / logging
testdata/      适配器测试用的原始响应样本
```

## 关于这个仓库

这是 Boku 的 Desktop 客户端。同一产品的 Apple 端（SwiftUI）是独立的、
尚未开源的实现，与本仓库共享领域契约：相同的实体字段与枚举取值、相同的
`providerId` 与凭据 key 命名、相同的密钥库格式与 KDF 参数、相同的金额与
时长规则、相同的错误语义。

`testdata/providers/` 的响应样本就是这份契约的可执行形式——两端读取同一份
向量，断言相同的 Snapshot，这样「同一份数据在两端显示成同一个值」才有保障。

## 贡献

Issue 与 PR 都欢迎。请注意：

- 一次只做一件事；改动尽量小而完整。
- 新增业务逻辑要带测试（happy path / invalid input / edge case）。
- 视图与布局不写像素断言测试，靠运行时验收。
- 不要把外部服务失败显示成 0；不要用浮点数存金额或时长。

架构规则与代码风格写在 [`AGENTS.md`](./AGENTS.md)，动手前值得一读。

## License

[MIT](./LICENSE)
