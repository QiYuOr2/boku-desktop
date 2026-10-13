# Desktop 客户端

Go + MyGo，窗口界面由 MyGo 自己绘制（native UI，`ui` 包）。
规范见 `docs/desktop.md`。

## 形态

托盘优先：

```text
托盘图标  ← 常驻。左键点击 开 / 合 快捷窗口
快捷窗口  ← mygo.NewFlyout，锚定托盘，竖版 320×300，点别处自动关闭
设置窗口  ← mygo.NewWindow，懒创建，只有点「设置」才打开
```

刻意**不设常驻托盘菜单**：MyGo 在 Windows / macOS 上「有菜单时左键弹出菜单」，
那样左键就点不出悬浮窗。菜单只在**右键**弹出期间挂上，用完立刻摘掉，
里面是「打开 Boku」与「退出 Boku」（托盘应用没有主窗口可关，必须有退出入口）。
Linux 例外：AppIndicator 只显示菜单、不报告点击，因此在 Linux 上菜单是常驻的。

## 目录

```text
main.go        装配：应用名、生命周期监听、托盘、悬浮窗
mygo.json      应用名、identifier、版本、产物目录
resources/     icon.png：应用图标，由 mygo build 打进应用包
internal/
  app/         Presentation：视图状态、托盘、悬浮窗、View（含视图测试）
  assets/      嵌入的托盘图标（编译期需要，不能运行时读盘）
                tray.png 是 resources/icon.png 的副本：go:embed 不能引用父目录
```

规划中但**尚未创建**的包（见 `docs/tasks/README.md` 的「待做」表）：

```text
internal/
  usecase/     UseCase：面板读取、缓存、single-flight；值格式化
  domain/      实体、接口、错误码、ID / 时间规则
  tools/       无状态纯函数工具（JSON、magnet…）
  data/        唯一引入 I/O 的包（sqlite / secret / httpx / provider / providers / adapters）
```

依赖方向（当前只有前两行真实存在）：

```text
main → app
app  → assets
app → usecase → domain
data → domain
tools → 无
```

不得反向，也不得出现 `app → data`、`domain → net/http`、`usecase → mygo/ui`。

托盘与悬浮窗的四条硬约束（都由 `docs/desktop.md` 规定）：

1. 默认不挂托盘菜单，否则左键永远点不出悬浮窗。
2. 每次开合都新建 flyout，复用 `Hidden` 的窗口不会被重新定位到托盘旁。
3. 必须注册 `App.OnWindowAllClosed`，否则悬浮窗一关进程就退出。
4. 托盘图标用 `go:embed`，不运行时读盘。

## 命令

在 `apps/desktop` 目录执行。Go 需要 1.27 或更新。

```bash
go tool mygo dev        # 开发：构建 dev 版本、启动、改动 .go 后重建重启
go tool mygo build      # 生产构建与打包，产物在 build/<os>-<arch>/
go test ./...           # 全部测试
go vet ./...            # 静态检查
go tool mygo vet .      # 检查把构建期 element 存进状态或 goroutine 的写法
gofmt -l -w .           # 格式化
go tool mygo doctor     # 检查本机环境
```

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

改动悬浮窗 / 设置窗口的布局时，把两种窗口渲染成 PNG 人工比对
（`internal/app/render_manual_test.go`，不设变量就跳过，不写任何文件）：

```powershell
$env:BOKU_RENDER_DIR='C:\Users\11762\repos\boku\.render'
go test ./internal/app/ -run TestRenderScreensForInspection
```

## 数据与凭据

- 数据库与凭据都在 `mygo.App.Path(mygo.PathUserData)` 之下。
- 数据库只保存凭据的引用（`secretRef`），**不保存明文**。
- 凭据当前存明文 `secrets.json`（权限 0600）。Windows 上这不等于 ACL，
  因此不是安全边界；加密密钥库见 `docs/tasks/todo/008-secret-vault.md`。
- 测试用 `fixtures/vault/` 与 `fixtures/providers/` 的向量，不得使用真实凭据。

## 网络环境

MyGo 的模块与预编译 SQLite 库从 Go 模块代理下载，默认 `GOPROXY` 即可。
国内网络下可设置：

```bash
go env -w GOPROXY=https://goproxy.cn,direct
```
