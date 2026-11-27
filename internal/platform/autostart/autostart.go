package autostart

import (
	"errors"
)

// Launcher 描述一个登录启动：运行的程序、告知程序是系统启动它的参数，以及该启动记录所用的名字。
type Launcher struct {
	// Label 是应用的标识符。macOS 不再用它命名登录启动（系统登录项就是应用本身），
	// 但旧机制写下的 launch agent 仍以它为标签，管理器首次读取时会清除它。
	Label string
	// Name 是人可读的应用名。
	Name string
	// Exe 是启动时运行的程序，绝对路径。Windows 用它指定程序；macOS 注册的是运行中的应用包，忽略该路径。
	Exe string
	// Arg 追加到命令行，是应用判断这次启动并非用户发起的方式。
	Arg string
}

// CommandLine 返回系统实际执行的命令：加引号的程序路径加参数。加引号是为了让含空格的路径（每个 macOS bundle 都有）保持为一个参数。
func (l Launcher) CommandLine() string {
	if l.Arg == "" {
		return `"` + l.Exe + `"`
	}
	return `"` + l.Exe + `" ` + l.Arg
}

// Document 是 Windows 登录启动所在的位置及其内容。
type Document struct {
	// Key 是 Windows 注册表值的完整名称。
	Key string
	// Contents 是其中的命令行。
	Contents []byte
}

// Manager 是某平台的登录启动。
//
// 这里不会启动进程，在未被询问前也不碰磁盘：manager 可以在应用运行前构建，
// 每个问题都从平台自身的记录作答，而非本进程的记忆。
type Manager interface {
	// Read 报告该 launcher 的启动是否已记录并生效。
	//
	// 指向其它程序的启动不算这个：应用被更新移到新位置后旧启动仍会残留，
	// 此时"应用是否开机启动"的答案就是否，直到重新拨动开关。
	//
	// 返回错误表示无法作答。ErrUnsupported 表示该平台没有 Boku 能注册的登录启动；
	// ErrNeedsApproval 表示系统已记录该启动，但用户须在系统设置中允许后才生效。
	Read(Launcher) (bool, error)
	// Location 以平台自身的说法给出启动所在位置：注册表值，或列出登录项的系统设置页。
	// 设置页把它显示在开关下方；它来自询问而非假设，以确保显示行与平台不会不一致。
	Location(Launcher) (string, error)
	// Start 记录启动。再次调用也能把被平台关闭的启动重新打开（Windows 任务管理器中的禁用项），
	// 以及把指向旧副本的启动更新到当前版本。
	Start(Launcher) error
	// Stop 移除启动。不存在也不算错误：什么都没留下，正是所求。
	Stop(Launcher) error
}

// manager 报告的问题，以便调用方区分"该机器无法有登录启动"与"写入失败"。
var (
	// ErrUnsupported 表示该桌面没有 Boku 能注册的登录启动，机器本身没有问题。
	ErrUnsupported = errors.New("autostart: this desktop cannot start the application at login")
	// ErrNotInstalled 表示平台能注册登录启动，但应用不在下次登录时仍会存在的运行位置
	// （`go run`、构建目录、即将被替换的副本）。这样的启动会指向不存在的程序，故拒绝写入。
	ErrNotInstalled = errors.New("autostart: the application is not running from a location it will still be in at the next login")
	// ErrNeedsApproval 表示系统已有该登录项，但用户在系统设置里关掉了（或仍需其允许）。
	// 这不是写入失败：页面会提示去哪里允许，而不是错误地显示开关为关。
	ErrNeedsApproval = errors.New("autostart: the login item needs approval in System Settings")
)

// OpenedAtLoginArg 是 Windows 登录启动追加到命令行的参数：应用据此知道自己由系统启动且无人操作。
// 框架会在 main 看到命令行前移除它并据此回答 WasOpenedAtLogin，因此 Windows 管理器与框架
// 必须使用完全相同的字符串——所以只在这里定义一次。
//
// macOS 的登录项启动应用包时不带参数；应用启动时不打开窗口，故那边不依赖它。
const OpenedAtLoginArg = "--mygo-opened-at-login"

// Available 报告 manager 是否有登录启动，设置页据此决定是否绘制开关：一个什么也不做的开关比没有更糟。
func Available(m Manager, l Launcher) bool {
	if m == nil {
		return false
	}
	_, err := m.Read(l)
	return !errors.Is(err, ErrUnsupported)
}
