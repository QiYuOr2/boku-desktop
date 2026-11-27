//go:build windows

package autostart

import (
	"os"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

// Windows 登录启动：用户自己键下的一个 Run 值，以及任务管理器在其旁维护的标记。
//
// 测试从不写入 Boku 会写的那条值。名为 Boku 的 Run 值会在下次登录时启动，测试不得导致这种启动。
// 文档（值的名字与命令行）是纯函数，会被完整测试；注册表往返测试自行使用一个测试专属的值，
// 并在测试结束时删除它。

// testRunValue 是往返测试写入的名字：应用名加后缀，以便可识别为测试的，而非真实值。
const testRunValue = "Boku autostart test"

// launcher 是一个登录启动项，正如安装器快捷方式会生成的那样：程序位于含空格的路径下，
// 这是多数 Windows 安装的情形。
func launcher() Launcher {
	return Launcher{
		Label: "com.boku.app",
		Name:  "Boku",
		Exe:   `C:\Users\test\AppData\Local\Programs\Boku\Boku.exe`,
		Arg:   OpenedAtLoginArg,
	}
}

// 值以应用命名，命令行是加引号的程序后接参数：不加引号的含空格路径只会启动 "C:" 而已。
func TestWindowsDocument(t *testing.T) {
	doc, err := NewWindows().Document(launcher())
	if err != nil {
		t.Fatalf("Document failed: %v", err)
	}
	if want := runKeyPath + `\Boku`; doc.Key != want {
		t.Errorf("the Run value is %q, want %q", doc.Key, want)
	}
	want := `"C:\Users\test\AppData\Local\Programs\Boku\Boku.exe" --mygo-opened-at-login`
	if string(doc.Contents) != want {
		t.Errorf("the command line is %q, want %q", doc.Contents, want)
	}
}

// 没有名字的启动项没有可记录的值。
func TestWindowsRefusesAStartWithNoName(t *testing.T) {
	if _, err := NewWindows().Document(Launcher{Exe: `C:\Boku.exe`}); err == nil {
		t.Error("Document accepted a start with no name")
	}
}

// 该名字下没写入过任何值，则启动为关——这是一个答案，不是错误。
func TestWindowsReadsAnAbsentValueAsOff(t *testing.T) {
	on, err := NewWindows().Read(launcher())
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if on {
		t.Error("Read said Boku starts at login, but nothing wrote that value")
	}
	if !Available(NewWindows(), launcher()) {
		t.Error("Available said Windows has no login start")
	}
}

// 在注册表上的往返，使用测试专属的值：写入、读回、删除。指向另一个程序的值读作关，
// 因为它会启动的不是本应用。
func TestWindowsStartStopRoundTrip(t *testing.T) {
	l := launcher()
	// 值的名字是测试唯一改动的地方：启动相关的其它一切都用真实的。
	l.Name = testRunValue
	manager := NewWindows()
	t.Cleanup(func() { _ = manager.Stop(l) })

	if err := manager.Start(l); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if on, err := manager.Read(l); err != nil || !on {
		t.Fatalf("Read after Start = %v, %v; want true, nil", on, err)
	}
	other := l
	other.Exe = `D:\Elsewhere\Boku.exe`
	if on, err := manager.Read(other); err != nil || on {
		t.Fatalf("Read of another program = %v, %v; want false, nil", on, err)
	}
	if err := manager.Stop(l); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if on, err := manager.Read(l); err != nil || on {
		t.Fatalf("Read after Stop = %v, %v; want false, nil", on, err)
	}
	// 再次 Stop 不算失败：无论如何开关都是关的。
	if err := manager.Stop(l); err != nil {
		t.Errorf("Stop without a start = %v, want nil", err)
	}
}

// 任务管理器可以不删除条目就把它关掉。那是一次不发生的启动，因此读作关——
// 重新写入启动正是用户重新打开它的方式，这也是 Start 会清除该标记的原因。
func TestWindowsTaskManagerFlagReadsAsOff(t *testing.T) {
	l := launcher()
	l.Name = testRunValue
	manager := NewWindows()
	t.Cleanup(func() { _ = manager.Stop(l) })
	if err := manager.Start(l); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	// 任务管理器写入的标记：一个首字节为奇数的二进制值。
	if err := writeTestFlag(testRunValue, []byte{3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}); err != nil {
		t.Fatalf("writing the flag failed: %v", err)
	}
	if on, err := manager.Read(l); err != nil || on {
		t.Fatalf("Read with Task Manager's flag = %v, %v; want false, nil", on, err)
	}
	// 再次按下开关就是对策，它会清掉该标记。
	if err := manager.Start(l); err != nil {
		t.Fatalf("Start over the flag failed: %v", err)
	}
	if on, err := manager.Read(l); err != nil || !on {
		t.Fatalf("Read after Start again = %v, %v; want true, nil", on, err)
	}
}

// 命令行是 Windows 实际运行的，因此必须保持程序完整：只加引号一次，参数在引号之外。
func TestWindowsCommandLineKeepsThePathWhole(t *testing.T) {
	l := launcher()
	if count := strings.Count(l.CommandLine(), `"`); count != 2 {
		t.Errorf("the command line has %d quotes, want the program quoted once: %s", count, l.CommandLine())
	}
	if !strings.HasSuffix(l.CommandLine(), " "+OpenedAtLoginArg) {
		t.Errorf("the command line has no argument after the program: %s", l.CommandLine())
	}
}

// 登录启动所指的程序必须是真实存在的文件，开关才不会写入一个不存在的东西的启动。
func TestWindowsBinaryPath(t *testing.T) {
	path, err := BinaryPath()
	if err != nil {
		t.Fatalf("BinaryPath failed: %v", err)
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		t.Errorf("BinaryPath = %q, which is not a program: %v", path, err)
	}
}

// 快捷方式的目标是 shell 存下的内容，常是命令行而非纯路径：路径含空格时加引号，
// 两种情况下参数都可能跟在其后。
func TestCleanShortcutTarget(t *testing.T) {
	cases := []struct{ in, want string }{
		{`C:\Programs\Boku\Boku.exe`, `C:\Programs\Boku\Boku.exe`},
		{`"C:\Program Files\Boku\Boku.exe"`, `C:\Program Files\Boku\Boku.exe`},
		{`"C:\Program Files\Boku\Boku.exe" --background`, `C:\Program Files\Boku\Boku.exe`},
		{`C:\Boku\Boku.exe --background`, `C:\Boku\Boku.exe`},
		{``, ``},
	}
	for _, c := range cases {
		if got := CleanShortcutTarget(c.in); got != c.want {
			t.Errorf("CleanShortcutTarget(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// writeTestFlag 为某个值写入任务管理器的「已关闭」标记。
//
// 只有测试会写这个标记：应用只清除它、从不设置它，且它所在的键在从未用过任务管理器的
// 机器上可能不存在，因此缺失时创建。
func writeTestFlag(name string, data []byte) error {
	path, err := syscall.UTF16PtrFromString(approvedKeyPath)
	if err != nil {
		return err
	}
	var key syscall.Handle
	if rc, _, _ := regCreateKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(path)), 0, 0, 0,
		keyWrite|keyRead, 0, uintptr(unsafe.Pointer(&key)), 0); rc != 0 {
		return syscall.Errno(rc)
	}
	defer regCloseKey.Call(uintptr(key))
	value, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	if rc, _, _ := regSetValueExW.Call(uintptr(key), uintptr(unsafe.Pointer(value)), 0, regBinary,
		uintptr(unsafe.Pointer(&data[0])), uintptr(len(data))); rc != 0 {
		return syscall.Errno(rc)
	}
	return nil
}
