//go:build windows

package autostart

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// Windows 把登录启动存为用户 Run 键下的一个值，并悄悄在另一个键里把它关掉。
//
// 任务管理器启动页在用户禁用条目时并不删除命令：它会在 StartupApproved\Run 里设一个标记，
// 启动就此不再发生，而 Run 值仍在。因此「是否开启」是两个问题——值是否存在且指向本程序，
// 以及它是否未被关闭；而「开启它」必须清除该标记，否则曾经禁用过的用户将再也无法从 Boku 打开它。
//
// 注册表经 advapi32 直接访问，与 platform/httpc 的代理读取器做法一致：本构建无 cgo，
// 用户自己键里的几个值不值得引入库。一切都读写在 HKCU（用户自己的 hive）：
// 登录启动属于用户，应用从不写机器的 hive。
const (
	runKeyPath      = `Software\Microsoft\Windows\CurrentVersion\Run`
	approvedKeyPath = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`

	hkeyCurrentUser = 0x80000001
	keyRead         = 0x20019
	keyWrite        = 0x20006

	regSZ     = 1
	regBinary = 3

	// errnoFileNotFound 是 ERROR_FILE_NOT_FOUND，RegOpenKeyEx 对不存在的键返回它。
	errnoFileNotFound = syscall.Errno(2)
	// errnoMoreData 是 ERROR_MORE_DATA，固定缓冲区装不下值时返回它：命令行分两步读取。
	errnoMoreData = syscall.Errno(234)
)

var (
	advapi32        = syscall.NewLazyDLL("advapi32.dll")
	regOpenKeyExW   = advapi32.NewProc("RegOpenKeyExW")
	regCreateKeyExW = advapi32.NewProc("RegCreateKeyExW")
	regQueryValueEx = advapi32.NewProc("RegQueryValueExW")
	regSetValueExW  = advapi32.NewProc("RegSetValueExW")
	regDeleteValueW = advapi32.NewProc("RegDeleteValueW")
	regCloseKey     = advapi32.NewProc("RegCloseKey")
)

// errKeyNotFound 与 errValueNotFound 是两种普通的「这里什么都没有」结果，
// 与真实失败区分开，使从未写入的登录启动只是「关」。
var (
	errKeyNotFound   = errors.New("autostart: the registry key is not there")
	errValueNotFound = errors.New("autostart: the registry value is not there")
)

// Windows 是当前用户注册表上的 Manager。构建它时不碰任何东西：注册表在提问时才读取，
// 那是应用运行之后。
type Windows struct{}

// NewWindows 返回 Windows 的 manager。
func NewWindows() *Windows { return &Windows{} }

// Document 是 Run 值：其完整名称，以及其中的命令行。
func (w *Windows) Document(l Launcher) (Document, error) {
	if strings.TrimSpace(l.Name) == "" {
		return Document{}, errors.New("autostart: the login start needs a name")
	}
	return Document{
		Key:      runKeyPath + `\` + l.Name,
		Contents: []byte(l.CommandLine()),
	}, nil
}

// Location 是 Run 值的完整名称，即设置页在开关下方展示的内容。
func (w *Windows) Location(l Launcher) (string, error) {
	doc, err := w.Document(l)
	if err != nil {
		return "", err
	}
	return doc.Key, nil
}

// Read 报告 Run 值是否指向本程序，且任务管理器未将其关闭。
//
// 指向应用另一份副本的值读作关：该启动会运行一个并非为此登录启动而设的程序。
func (w *Windows) Read(l Launcher) (bool, error) {
	doc, err := w.Document(l)
	if err != nil {
		return false, err
	}
	command, kind, ok, err := readRegistryString(runKeyPath, l.Name)
	if err != nil {
		return false, err
	}
	if !ok || kind != regSZ || !strings.EqualFold(command, string(doc.Contents)) {
		return false, nil
	}
	state, kind, ok, err := readRegistryBytes(approvedKeyPath, l.Name)
	if err != nil {
		return false, err
	}
	if !ok || kind != regBinary || len(state) == 0 {
		return true, nil
	}
	// 任务管理器关闭该条目时，首字节为奇数。
	return state[0]&1 == 0, nil
}

// Start 写入 Run 值并清除任务管理器关于它的记录。
//
// 该标记属于曾在任务管理器关掉它的用户。应用再次请求登录启动就是用户的回应，因此一并清除标记。
// 审批键并非每台机器都存在，其缺失不是失败：没有可恢复的东西。
func (w *Windows) Start(l Launcher) error {
	doc, err := w.Document(l)
	if err != nil {
		return err
	}
	if err := writeRegistryString(runKeyPath, l.Name, string(doc.Contents)); err != nil {
		return err
	}
	if err := deleteRegistryValue(approvedKeyPath, l.Name); err != nil && !errors.Is(err, errValueNotFound) {
		return err
	}
	return nil
}

// Stop 移除两个值。只删 Run 值而留下标记，会是一次什么也不做的启动——
// 而之后重新写入该值的 Boku 会发现自己处于关闭状态，屏幕上却无任何解释。
func (w *Windows) Stop(l Launcher) error {
	if err := deleteRegistryValue(runKeyPath, l.Name); err != nil && !errors.Is(err, errValueNotFound) {
		return err
	}
	if err := deleteRegistryValue(approvedKeyPath, l.Name); err != nil && !errors.Is(err, errValueNotFound) {
		return err
	}
	return nil
}

// regError 把两种「这里什么都没有」结果与真实失败区分开。
func regError(errno syscall.Errno) error {
	if errno == errnoFileNotFound {
		return errKeyNotFound
	}
	return errno
}

// missingValue 把 RegQueryValueEx 的「什么都没有」读作正常值而非错误：键缺失与值缺失都意味着启动为关。
func missingValue(errno syscall.Errno) error {
	if errno == errnoFileNotFound || errno == errnoMoreData {
		return nil
	}
	return errno
}

// openKey 打开当前用户的一个键，把不存在的键报告为 errKeyNotFound：
// 从未写入的登录启动就是从未创建过的键。
func openKey(path string) (syscall.Handle, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var key syscall.Handle
	if rc, _, _ := regOpenKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(name)), 0,
		keyRead|keyWrite, uintptr(unsafe.Pointer(&key))); rc != 0 {
		return 0, regError(syscall.Errno(rc))
	}
	return key, nil
}

// readRegistryString 读取键下的一个值。键或值不存在时 ok 为 false，这不是错误：启动即视为关。
func readRegistryString(path, name string) (string, uint32, bool, error) {
	key, err := openKey(path)
	if err != nil {
		if errors.Is(err, errKeyNotFound) {
			return "", 0, false, nil
		}
		return "", 0, false, err
	}
	defer regCloseKey.Call(uintptr(key))

	value, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return "", 0, false, err
	}
	var kind, size uint32
	if rc, _, _ := regQueryValueEx.Call(uintptr(key), uintptr(unsafe.Pointer(value)), 0,
		uintptr(unsafe.Pointer(&kind)), 0, uintptr(unsafe.Pointer(&size))); rc != 0 {
		return "", 0, false, missingValue(syscall.Errno(rc))
	}
	if size == 0 {
		return "", kind, true, nil
	}
	// size 计字节，且值以 NUL 结尾：所需单元数多一个才容纳两者。
	buffer := make([]uint16, size/2+1)
	if rc, _, _ := regQueryValueEx.Call(uintptr(key), uintptr(unsafe.Pointer(value)), 0,
		uintptr(unsafe.Pointer(&kind)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size))); rc != 0 {
		return "", 0, false, missingValue(syscall.Errno(rc))
	}
	return syscall.UTF16ToString(buffer), kind, true, nil
}

// readRegistryBytes 读取非字符串的值，任务管理器的标记就是以这种方式存储的。
func readRegistryBytes(path, name string) ([]byte, uint32, bool, error) {
	key, err := openKey(path)
	if err != nil {
		if errors.Is(err, errKeyNotFound) {
			return nil, 0, false, nil
		}
		return nil, 0, false, err
	}
	defer regCloseKey.Call(uintptr(key))

	value, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, 0, false, err
	}
	var kind, size uint32
	if rc, _, _ := regQueryValueEx.Call(uintptr(key), uintptr(unsafe.Pointer(value)), 0,
		uintptr(unsafe.Pointer(&kind)), 0, uintptr(unsafe.Pointer(&size))); rc != 0 {
		return nil, 0, false, missingValue(syscall.Errno(rc))
	}
	if size == 0 {
		return []byte{}, kind, true, nil
	}
	buffer := make([]byte, size)
	if rc, _, _ := regQueryValueEx.Call(uintptr(key), uintptr(unsafe.Pointer(value)), 0,
		uintptr(unsafe.Pointer(&kind)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size))); rc != 0 {
		return nil, 0, false, missingValue(syscall.Errno(rc))
	}
	return buffer[:size], kind, true, nil
}

// writeRegistryString 写入一个字符串值，键不存在时创建。键自身的权限交给 Windows。
func writeRegistryString(path, name, text string) error {
	keyPath, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	var key syscall.Handle
	var disposition uint32
	if rc, _, _ := regCreateKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(keyPath)), 0, 0, 0,
		keyWrite|keyRead, 0, uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&disposition))); rc != 0 {
		return syscall.Errno(rc)
	}
	defer regCloseKey.Call(uintptr(key))

	value, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	data := syscall.StringToUTF16(text)
	if rc, _, _ := regSetValueExW.Call(uintptr(key), uintptr(unsafe.Pointer(value)), 0, regSZ,
		uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)*2)); rc != 0 {
		return syscall.Errno(rc)
	}
	return nil
}

// deleteRegistryValue 移除一个值，把不存在的键或值报告为 errValueNotFound，
// 以便调用方说它无事可做。
func deleteRegistryValue(path, name string) error {
	key, err := openKey(path)
	if err != nil {
		if errors.Is(err, errKeyNotFound) {
			return errValueNotFound
		}
		return err
	}
	defer regCloseKey.Call(uintptr(key))
	value, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	if rc, _, _ := regDeleteValueW.Call(uintptr(key), uintptr(unsafe.Pointer(value))); rc != 0 {
		if syscall.Errno(rc) == errnoFileNotFound {
			return errValueNotFound
		}
		return syscall.Errno(rc)
	}
	return nil
}

// BinaryPath 是登录启动要运行的程序。
//
// 它不是 os.Executable。安装器的快捷方式才是程序的家：更新会替换快捷方式指向的文件，
// 下次启动即运行它们。登录启动若指向别处，就成了第二份副本。因此优先取快捷方式的目标，
// 在没有可读快捷方式时（开发构建、手工解包的应用）回退到当前运行的程序。
func BinaryPath() (string, error) {
	if link := applicationShortcutPath(); link != "" {
		if target := CleanShortcutTarget(ShortcutTarget(link)); target != "" {
			return target, nil
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate the executable: %w", err)
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return exe, nil
}
