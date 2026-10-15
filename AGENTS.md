# AGENTS.md

本文件是 AI Coding Agent 在本仓库工作时的规则。**一次只处理一个 Task。**

## 这是什么

Boku Desktop：托盘上的一个图标，点开是一张悬浮小卡片。

- **Panel（面板）**：API 余额、软件订阅、读书时长、游戏时长、自定义指标
- **Metric / Observation / Snapshot**：跨 Provider 的统一指标模型
- **Provider / Adapter**：第三方数据接入与适配
- **Tool（工具）**：划词翻译、JSON 格式化、剪贴板历史

已放弃的能力（不得重新引入）：Note、Transaction、Timeline、Calendar、
Folder、Tag、Wiki Link、Backlinks、Markdown 笔记。

核心原则：

> 外部服务失败只能让一张卡片失败，不能让应用或本地数据失败。

> 凭据永远不落明文库。

## Before Coding

先读 `README.md`（构建、目录、界面约定、数据与凭据都写在那里）。
改代码前对照本文件下面的架构规则。

## Architecture Rules

Desktop 是单个 Go 进程。顶层按**功能**分包：每个功能自己拥有它的服务、它的存储
与它认识的厂商。跨功能共享的只有三样：领域契约、纯函数、外部世界适配。

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

规则：

- 一个功能一个包；**功能之间不互相 import**。它们只在 `internal/domain` 的接口上
  相遇（`SecretStore`、`ClipboardDevice`…），由 `main.go` 注入实现。
- 功能可以依赖 `domain`、`kit`、`platform`、`contract`；反过来一律不许。
- `domain`、`contract/*` 不 import 任何其它 internal 包；`kit` 只依赖 `domain`。
- 有 I/O 的通用技术只写在 `internal/platform/*`；某个功能自己的存储写在它的包里。
- 只有 `internal/platform/*` 与 `internal/panel/codex` 碰操作系统。
- 只有 `internal/platform/httpc` 与厂商包（`internal/panel/<vendor>`、
  `internal/translate/<backend>`）引入 `net/http`。
- 厂商身份（`providerId`、凭据 key 名）只写在 `internal/contract/<厂商>`；面板与
  翻译器都从那里读，不各写一份字符串。
- `main.go` 是唯一装配点，也是唯一 import 所有功能包的文件。

### Provider / Adapter 规则

- Provider 只做 HTTP 与错误翻译，不认识 `Panel` / `Snapshot` / `Metric`。
- Adapter 只做纯函数映射，**不发网络请求**，必须能离线单元测试。
- Provider 与 Adapter 必须成对注册；启动时校验，缺一即启动失败。
- 领域模型里**不允许**出现厂商字段名（`steam`、`weread`、`deepseek` 等）。

### 厂商代码的落点

一个厂商一个包：`internal/panel/<厂商>`（面板）与 `internal/translate/<后端>`
（翻译）。同一个包内按**代码性质**分文件，靠 review 与测试维持：

```text
spec.go     配置：面板定义、指标 key、刷新周期          （纯）
client.go   出网：请求与错误翻译                        （有 I/O）
adapter.go  纯映射：原始 JSON → domain.Snapshot         （纯，不得碰时钟、文件、网络）
login.go    借来的凭据：读另一个工具自己的登录文件        （有 I/O，只有一个厂商有）
```

`adapter.go` 的纯净由包内约定保证，因此**新增厂商时必须检查它没有引入
`platform/*`、`os`、`net`**。

### 应用设置

所有设置存在一个文件 `settings.json` 里，但**存储本身不知道里面有什么**。

`internal/settings` 只认识 `Section`：一个名字，一个「从整份设置里挑出我自己的键」
的方法，一个「把我自己的键交出去」的方法。它读写文件、加锁、合并，对字段一无所知。

```go
type Section interface {
    Name() string
    Decode(values map[string]json.RawMessage) error  // 拿到全部键，只挑自己的
    Encode() (map[string]any, error)                 // 只交出自己拥有的键
}
```

规则：

- **每个有设置的功能自己实现 `Section`**，放在自己的包里（`clipboard/section.go`、
  `translate/section.go`、`panel/section.go`）。
- 功能**不 import `internal/settings`**：它声明自己需要的最小接口（`StoreSettings`），
  由 `main.go` 注入实现。
- 改一个设置走 `Store.Update`，**不要 `Load` 之后再 `Save`**：后者是三步，两个功能
  各自「读旧值 → 写回旧值 + 我的改动」会互相抹掉，这就是丢失更新。
- **文件格式是扁平的** `{"version":1,"tools":{...}}`，所有功能的键平铺在 `tools` 下。
  这是已有用户文件的形状，**保持它就不需要迁移**。
- 磁盘上的键名是**存储决定**，不是 Go 字段名。每个功能把它的键名列成常量，只写
  一处。改字段名不改键名。
- 文件里**没有的键**＝这个设置保持默认值。**不要**把缺失当成零值读：那会把开关
  关掉、把时长清零。
- 同一个键被两个 `Section` 声明是装配错误，`write` 会报错而不是静默二选一。

### Forbidden

- 在 `internal/app` 里写 SQL 或 HTTP 请求。
- 在 `internal/domain` 里 import `net/http`、`database/sql`、`mygo/*`。
- 在 `internal/kit` 里依赖窗口、网络、数据库、文件或 `mygo/*`（只许依赖 `domain`）。
- 一个功能包 import 另一个功能包。
- 在 `adapter.go` 里读时钟、开文件或发请求。
- 把构建期 `ui.Element` 存进状态结构体或传给 goroutine。
- 在视图函数里做慢 I/O、网络请求或修改状态（视图每帧可能被调用多次）。
- 在 `GlobalShortcut` / 菜单回调里同步做网络请求（回调在主线程，必须立刻返回）。
- 把凭据明文写入数据库、日志或错误信息。
- 把外部服务失败显示为 0。
- 用浮点数保存金额或时长。
- 未经任务要求修改数据库 Schema。
- 未经任务要求重构无关代码。

## Repository Layout

```text
main.go         装配：应用名、生命周期监听、托盘、悬浮窗。唯一装配点
mygo.json       应用名、identifier、版本、产物目录
resources/      icon.png：应用图标，由 mygo build 打进应用包
internal/       app / domain / kit / platform / contract / panel / clipboard
                / translate / credential / settings / logging
testdata/       适配器测试用的原始响应样本
```

## Toolchain Rules

- 命令入口只有 `go` 与 `go tool mygo`。
- 不引入第二套构建工具或前端构建链。
- 所有代码必须能在 `CGO_ENABLED=0` 下编译，不引入需要 cgo 的依赖。
- 验证命令：`gofmt -l .`、`go vet ./...`、`go tool mygo vet .`、`go test ./...`。

## Data Rules

- 所有核心实体使用稳定 ID（UUIDv7）；`ProviderConfig` 以 `providerId` 为主键。
- 时间使用明确的 RFC 3339 / UTC 规范。
- 时间字段语义：`recordedAt` 用户视角的事件时间、`copiedAt` 采集时间、
  `observedAt` 取数时间、`createdAt` / `updatedAt` / `deletedAt` 系统时间。
- 删除使用 soft delete，唯一例外是可再生的缓存（`snapshot_cache`）。
- 金额使用整数最小货币单位，字段名以 `Minor` 结尾。
- 时长使用整数秒，字段名以 `Seconds` 结尾。
- 百分比使用 `0..100`，不使用 `0..1`。
- 跨币种不做隐式换算。

## Secrets Rules

- 凭据只存密钥库；数据库只存 `secretRef`。
- 测试中禁止使用真实凭据；用 `testdata/` 的弱密码向量。
- 日志与错误信息输出凭据前必须 `redact`；错误只允许包含 key 名，不允许包含值。
- UI 展示凭据只显示后 4 位。

## Network Rules

- 出网行为只写在 `internal/panel/<厂商>` 与 `internal/translate/<后端>`，统一走
  `internal/platform/httpc`。
- 每个请求必须有超时与取消；默认超时 10 秒。
- 响应体读取有上限（默认 1 MiB）。
- HTTP 错误必须翻译成领域错误，不透传响应体。
- `ErrProviderBadResponse` 携带的响应片段必须裁剪到 ≤512 字节。
- 网络失败不得影响本地功能与本地数据。

## Testing Rules

新增业务逻辑至少覆盖：

- happy path
- invalid input
- edge case

额外要求：

- **业务模块的 UI 部分不做单测**：视图、布局、绘制、像素与坐标断言不写测试，
  靠运行时验收。这条覆盖 `internal/app` 里任何用 `ui.NewTester` 渲染并断言画面
  的测试。服务与状态机的行为测试照写——测的是「做了什么」，不是「画成什么样」。
- Adapter 必须有 fixture 测试，且包含**畸形响应**用例，断言返回
  `ErrProviderBadResponse` 而不是产出 0。
- HTTP 测试使用 `httptest`，禁止真实出网。
- 涉及并发与定时器的代码必须能通过 `go test -race`。

## Task Discipline

完成后报告：

```text
Changed files:
...

Behavior:
...

Tests:
...

Risks:
...

Follow-ups:
...
```

如果发现当前 Task 之外的问题：可以记录，不要顺手修复。

## Coding Style

优先：小函数、清晰命名、单一职责、可测试代码、显式依赖注入。

避免：过度抽象、为未来不存在的需求建复杂框架、大型万能 Service、
巨型视图状态（把所有功能塞进一个 struct 与一个 View 函数）、巨型 UseCase。

## Definition of Done

Task 只有在以下条件全部满足时才算完成：

- 编译通过
- 测试通过
- `gofmt`、`go vet`、`go tool mygo vet` 没有报错
- 没有修改任务范围外的业务行为
- 文档需要更新时已经更新
- 输出 changed files / behavior / tests / risks / follow-ups
