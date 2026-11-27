package autostart

import (
	"errors"
	"testing"
)

// 这些是每个平台共用的测试：登录启动中与具体机制无关的部分；各平台写出的文档在写它的代码旁测试。

// 命令行就是系统实际执行的字符串，含空格的路径必须保持为一个参数（每个 macOS bundle、半数 Windows 安装都如此），且参数要放在引号外、位于路径之后。
func TestCommandLineQuotesTheProgram(t *testing.T) {
	l := Launcher{Exe: "/Applications/Boku.app/Contents/MacOS/Boku", Arg: "--mygo-opened-at-login"}
	if got, want := l.CommandLine(), `"/Applications/Boku.app/Contents/MacOS/Boku" --mygo-opened-at-login`; got != want {
		t.Errorf("CommandLine = %q, want %q", got, want)
	}
	l.Arg = ""
	if got, want := l.CommandLine(), `"/Applications/Boku.app/Contents/MacOS/Boku"`; got != want {
		t.Errorf("CommandLine without an argument = %q, want %q", got, want)
	}
}

// 没有登录启动的桌面必须对每个问题返回同一个错误，Available 据此不绘制开关。这是 Linux/BSD 分支，所以能在构建本包的机器上运行。
func TestUnsupportedHasNoLoginStart(t *testing.T) {
	var manager Unsupported
	launcher := Launcher{Label: "com.boku.app", Name: "Boku", Exe: "/usr/local/bin/boku"}
	if _, err := manager.Read(launcher); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Read = %v, want ErrUnsupported", err)
	}
	if _, err := manager.Location(launcher); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Location = %v, want ErrUnsupported", err)
	}
	if err := manager.Start(launcher); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Start = %v, want ErrUnsupported", err)
	}
	if err := manager.Stop(launcher); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Stop = %v, want ErrUnsupported", err)
	}
	if Available(manager, launcher) {
		t.Error("Available said a desktop with no login start has one")
	}
}

// 无法回答的 manager 也不能被当作可绘制：未装配的状态里就是 nil，绝不能 panic。
func TestAvailableNeedsAManager(t *testing.T) {
	if Available(nil, Launcher{}) {
		t.Error("Available said a nil manager has a login start")
	}
}

// 登录启动传给应用的参数是框架自己的标记，应用据此知道这次启动不是用户发起的；它只在这里定义一次。
func TestOpenedAtLoginArgument(t *testing.T) {
	if OpenedAtLoginArg != "--mygo-opened-at-login" {
		t.Errorf("OpenedAtLoginArg = %q, want the framework's own marker", OpenedAtLoginArg)
	}
}
