# Boku Desktop

[![CI](https://github.com/QiYuOr2/boku-desktop/actions/workflows/ci.yml/badge.svg)](https://github.com/QiYuOr2/boku-desktop/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/QiYuOr2/boku-desktop)](https://github.com/QiYuOr2/boku-desktop/blob/main/go.mod)
[![MyGo](https://img.shields.io/badge/MyGo-v0.5.1-00ADD8)](https://github.com/egoist/mygo)
[![License](https://img.shields.io/github/license/QiYuOr2/boku-desktop)](./LICENSE)

<p align="center">
  <img src="./docs/images/boku-promo-desktop.png" width="960" alt="Boku：个人面板与效率工具，展示余额、划词翻译、剪贴板历史与设置">
</p>

<p align="center">
  <strong>你的个人面板与效率工具。</strong><br>
  常驻托盘，点一下看状态，按一下用工具，然后继续专注。
</p>

<p align="center">
  <a href="https://github.com/QiYuOr2/boku-desktop/releases/latest">下载最新版本</a>
  ·
  <a href="https://github.com/QiYuOr2/boku-desktop/issues">反馈问题或建议</a>
</p>

## 为什么是 Boku

每天都会反复打开的网页、命令行和小工具，不应该散在各处。Boku 把它们收进一个轻量的托盘入口：

- **看状态**：在一张悬浮小卡片中查看 API 余额和 Codex 额度。
- **随手处理**：划词即译，复制过的内容随时找回。
- **不打断工作**：没有常驻主窗口；左键点托盘图标开合面板，用完即走。

它是个人入口，不是笔记、记账、日历或团队协作软件。

## 已有能力

| 你想做什么 | Boku 如何完成 |
| --- | --- |
| 查看 DeepSeek 余额 | 填入自己的 API Key，查看余额、赠金与充值余额。 |
| 查看 Codex 额度 | 读取本机已登录的 Codex CLI 状态，无需再次填写 Key。 |
| 快速翻译选中的文字 | 按 <kbd>Ctrl</kbd>/<kbd>⌘</kbd> + <kbd>Shift</kbd> + <kbd>T</kbd>，使用免费翻译或已配置的 AI 服务；日文可按需显示注音。 |
| 找回刚才复制的内容 | 自动保留文本、链接、JSON、代码、文件和图片；点一条即可复制回剪贴板。 |
| 管理设置与凭据 | 在一个设置窗口中管理 API Key、翻译、剪贴板、外观与应用信息。 |

## 三步开始

1. 在 [Releases](https://github.com/QiYuOr2/boku-desktop/releases/latest) 下载适合平台的安装包。Windows 可选择安装包或免安装版；macOS 使用通用 DMG。
2. 启动 Boku。它会安静地驻留在系统托盘，不会抢占桌面。
3. 左键点击托盘图标打开个人面板；选中文字后按默认快捷键即可翻译。

首次使用不需要账号。免费翻译引擎可以直接使用；DeepSeek 余额和 AI 翻译只在你主动配置自己的凭据后才会启用。

> macOS 首次打开未公证的 App 时，系统可能显示安全提示。请从本仓库的 Release 下载，并通过 Finder 中的“右键 → 打开”完成首次确认。

## 数据、网络与隐私

- **没有 Boku 项目服务器，也没有账户系统。** 面板请求只发往你选择的数据服务；更新检查只读取 GitHub Release。
- **凭据保存在本机的明文 `secrets.json` 文件中。** 这是有意的取舍：文件权限 / ACL 是边界，不提供加密、主密码或云端托管。请不要把该文件同步到不可信的位置。
- **不会把凭据显示在界面、日志或错误信息里。** 本地日志保留最近 30 天。
- 外部服务失败只影响对应卡片；Boku 不会用 `0` 冒充失败结果，剪贴板等本地功能照常可用。

## 平台与现状

| 平台 | 状态 |
| --- | --- |
| Windows | 已发布 |
| macOS | 已发布 |
| Linux | 可编译，尚未发布；划词取词不可用 |

Boku 仍处于早期阶段，接口与数据结构可能调整。接下来计划探索的软件订阅、读书 / 游戏时长面板、JSON 格式化与磁力链接预览尚未实现；剪贴板历史的搜索、置顶、删除与清空也还在计划中。

### 使用前知道

- 面板由打开悬浮窗、手动刷新和保存凭据时更新，**暂未提供定时刷新**；重启后首次查看面板需要联网。
- 划词翻译在 macOS 上需要在“系统设置 → 隐私与安全性 → 辅助功能”中授权；未授权时会回退到读取剪贴板。
- 自动更新会在启动约一分钟后检查一次，此后每 24 小时检查；只有 MyGo 可原地替换的安装位置才能实际完成“重启并更新”。
- 全局快捷键目前只有划词翻译；打开个人面板请左键点击托盘图标。

## 从源码构建

需要 **Go 1.27 或更新**。

```bash
git clone https://github.com/QiYuOr2/boku-desktop.git
cd boku-desktop

go tool mygo dev        # 开发：构建并启动；改动 .go 后自动重建重启
go tool mygo build      # 生产构建与打包，产物在 build/<os>-<arch>/
go tool mygo doctor     # 检查本机环境
```

程序可在 `CGO_ENABLED=0` 下编译，不依赖 cgo。若 Go 模块下载较慢，可以设置代理：

```bash
go env -w GOPROXY=https://goproxy.cn,direct
```

提交前请运行与 CI 相同的检查：

```bash
gofmt -l .              # 应无输出
go vet ./...
go tool mygo vet .      # 检查 MyGo 视图与 goroutine 的常见错误
go test ./...
```

## 架构

Boku 是单个 Go 进程，按功能而不是技术层拆分：

```text
internal/domain       实体、值对象、错误与端口接口
internal/kit          无 I/O 的纯函数
internal/platform     HTTP、剪贴板、取词与原子文件等系统适配
internal/panel        余额与额度面板
internal/clipboard    剪贴板历史
internal/translate    划词翻译
internal/credential   明文凭据文件
internal/settings     设置文件
internal/app          原生视图、窗口、托盘与快捷键
main.go                唯一装配点
```

功能包不直接互相依赖；它们通过 `internal/domain` 的接口协作，由 `main.go` 装配。Provider 负责 HTTP 与错误翻译，Adapter 只做可离线测试的数据映射。

## 贡献

欢迎提交 Bug 报告与改进建议，请先到 [Issues](https://github.com/QiYuOr2/boku-desktop/issues) 搜索是否已有相关讨论。开发时请遵循仓库中的 [AGENTS.md](./AGENTS.md)；它说明了架构边界、数据与凭据约束，以及验证要求。

## License

[MIT](./LICENSE)
