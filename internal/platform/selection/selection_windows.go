//go:build windows

package selection

import (
	"context"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"boku/internal/domain"
)

// 选中内容通过 UI Automation（屏幕阅读器读取控件的接口）读取。直接调用其 vtable：本构建
// 没有 cgo，而客户端 API 是 COM。
//
// 下面的 vtable 槽位来自 uiautomationclient.h。COM vtable 按顺序而非名字排列，槽号是
// 访问方法的唯一方式；这里随所属接口一并记下，布局靠「槽位返回什么」来校验而非信任——
// 差一位的槽会调到参数错误的方法，因此用 get_CurrentClassName（槽 30）对编辑控件返回
// "Edit" 来核对头文件的读取。
const (
	slotGetFocusedElement = 8  // IUIAutomation::GetFocusedElement
	slotGetCurrentPattern = 16 // IUIAutomationElement::GetCurrentPattern
	slotGetSelection      = 5  // IUIAutomationTextPattern::GetSelection
	slotRangeArrayLength  = 3  // IUIAutomationTextRangeArray::get_Length
	slotRangeArrayElement = 4  // IUIAutomationTextRangeArray::GetElement
	slotRangeText         = 12 // IUIAutomationTextRange::GetText
	slotRelease           = 2  // IUnknown::Release
)

// textPatternID 是 UIA_TextPatternId：暴露控件文本及其中选中内容的控件模式。不发布文本
// 的控件（画布、游戏、普通编辑控件经桥接暴露的）没有它，也就是「只能靠剪贴板回答」的常见情况。
const textPatternID = 10014

// comPtr 是 COM 接口指针。用 unsafe.Pointer 而非 uintptr，这样 vtable 和 BSTR 指向的字符串
// 无需把整数转回指针就能读取，而这种转换是不允许的。
type comPtr = unsafe.Pointer

// New 返回设备：UI Automation 读取发布了文本的控件，合成复制则去询问什么都不发布的应用。
func New(opts Options) domain.SelectionDevice {
	sequence := opts.Sequence
	if sequence == nil {
		sequence = clipboardSequence
	}
	return Device{clipboard: opts.Clipboard, beforeCopy: opts.BeforeCopy, sequence: sequence}
}

// Device 通过 Windows 的 UI Automation 读取选中内容，UI Automation 无话可说时请应用复制。
type Device struct {
	// clipboard 是复制方案读取并还原的剪贴板，未提供时为 nil。
	clipboard domain.ClipboardDevice
	// beforeCopy 在合成复制前调用，以便剪贴板工具把复制的值排除在历史之外。
	beforeCopy func()
	// sequence 是剪贴板的写计数器。除非装配时另给，否则用平台自带的；这也让同一套复制
	// 逻辑能跑在别的平台上。
	sequence func() uint32
}

// SelectedText 询问系统用户当前应用里选中了什么。
//
// 这不是一次调用。被读取的应用首次被询问时才开启辅助功能——浏览器在第一个查询到达后
// 才建树——所以对浏览器读一次会在屏幕上明明有选中时回答「没有选中」。因此会重试几次
// （DefaultAttempts、DefaultRetryGap），且每次尝试跑在独立线程上并带时限等待：调用另一个
// 应用的 UI Automation provider 可能挂起，而工具还有一个更旧的答案——剪贴板——可回退，
// 不必让用户干等。被放弃的读取会自行结束并归还其线程。
//
// UI Automation 无话可说时请应用复制（见 readWithACopy）：这能触达不发布文本的应用
// （聊天窗口、Windows 11 记事本），也是这里唯一用户能察觉的路径，因为它借用剪贴板再还回去。
func (d Device) SelectedText(ctx context.Context) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	deadline := time.Now().Add(DefaultTimeout)
	if text := readUntilAnswered(ctx, readWithin, DefaultAttempts, DefaultRetryGap, deadline); text != "" {
		return text, nil
	}
	if d.clipboard != nil && d.sequence != nil && safeToCopy() {
		if text := readWithACopy(ctx, d.clipboard, d.beforeCopy, d.sequence, pressCopy); text != "" {
			return text, nil
		}
	}
	// 每次尝试都是同样的回答，这是不发布文本的应用和什么都没选中的常见答案。这不是失败：
	// 调用方会去问剪贴板。
	return "", domain.ErrSelectionUnavailable
}

// readWithin 是一次读取选中内容的尝试，供 readUntilAnswered 调用：读取跑在独立线程上，
// 这里带时限等待，因为它发起的调用属于另一个应用。
func readWithin(ctx context.Context, within time.Duration) string {
	done := make(chan string, 1)
	go func() {
		text, _ := readSelection()
		done <- text
	}()
	timer := time.NewTimer(within)
	defer timer.Stop()
	select {
	case text := <-done:
		return text
	case <-ctx.Done():
		return ""
	case <-timer.C:
		return ""
	}
}

// readSelection 在已为 COM 初始化的线程上取得答案：COM 属于调用 CoInitializeEx 的线程，
// 而翻译的 goroutine 可能落在任意线程。
func readSelection() (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	uninitialize, ok := comInit()
	if !ok {
		return "", domain.ErrSelectionUnavailable
	}
	defer uninitialize()

	automation := createAutomation()
	if automation == nil {
		return "", domain.ErrSelectionUnavailable
	}
	defer release(automation)

	// 持有键盘的元素就是用户正在用的元素，其选中内容即为所求。
	focused, _ := out(automation, slotGetFocusedElement)
	return selectionOf(focused)
}

// selectionOf 读取一个元素的选中文本：发布文本的控件模式、它报告的选中范围，以及第一个
// 范围的文本。
//
// 这里没有调用方需要区分的错误。元素为 nil、控件不发布文本、空选中和失败的调用都是
// 「平台无法回答」，一律返回 ErrSelectionUnavailable 以便调用方回退到剪贴板。
func selectionOf(element comPtr) (string, error) {
	if element == nil {
		// 没有持有键盘的元素：用户没在操作任何应用，也就没有选中内容。
		return "", domain.ErrSelectionUnavailable
	}
	defer release(element)

	pattern, _ := out(element, slotGetCurrentPattern, textPatternID)
	if pattern == nil {
		// 前台控件不发布文本。这正是剪贴板必须回答的情况。
		return "", domain.ErrSelectionUnavailable
	}
	defer release(pattern)

	ranges, _ := out(pattern, slotGetSelection)
	if ranges == nil {
		return "", domain.ErrSelectionUnavailable
	}
	defer release(ranges)

	var count int32
	if hr := call(ranges, slotRangeArrayLength, uintptr(unsafe.Pointer(&count))); failed(hr) || count <= 0 {
		// 空选中：用户只有光标没有范围，这是快捷键在无选中时按下应用给出的回答。
		return "", domain.ErrSelectionUnavailable
	}
	span, _ := out(ranges, slotRangeArrayElement, 0)
	if span == nil {
		return "", domain.ErrSelectionUnavailable
	}
	defer release(span)

	// -1 请求整个范围而非前 N 个字符：选中内容值得翻译的是它的全部。
	var text comPtr
	if hr := call(span, slotRangeText, ^uintptr(0), uintptr(unsafe.Pointer(&text))); failed(hr) {
		return "", domain.ErrSelectionUnavailable
	}
	selected := strings.TrimSpace(bstr(text))
	if selected == "" {
		return "", domain.ErrSelectionUnavailable
	}
	return selected, nil
}

// --- COM 与 UI Automation，以直接调用方式使用 ---------------------------------

var (
	ole32    = syscall.NewLazyDLL("ole32.dll")
	oleaut32 = syscall.NewLazyDLL("oleaut32.dll")

	coInitializeEx   = ole32.NewProc("CoInitializeEx")
	coUninitialize   = ole32.NewProc("CoUninitialize")
	coCreateInstance = ole32.NewProc("CoCreateInstance")
	sysFreeString    = oleaut32.NewProc("SysFreeString")
)

// COINIT_APARTMENTTHREADED 和 CLSCTX_INPROC_SERVER，来自 objbase.h。
const (
	coinitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1
	rpcEChangedMode         = 0x80010106
)

// CLSID_CUIAutomation 和 IID_IUIAutomation，来自 uiautomationclient.h。
var (
	clsidCUIAutomation = guid{0xFF48DBA4, 0x60EF, 0x4201, [8]byte{0xAA, 0x87, 0x54, 0x10, 0x3E, 0xEF, 0x59, 0x4E}}
	iidIUIAutomation   = guid{0x30CBE57D, 0xD9D0, 0x452A, [8]byte{0xAB, 0x13, 0x7A, 0xC5, 0xAC, 0x48, 0x25, 0xEE}}
)

// guid 是 COM 全局唯一标识符，按平台布局：三个字段，再接按顺序排列的八个字节。
type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// comInit 在当前线程初始化 COM 并返回还原操作。已被初始化过的线程（由应用自身或另一个
// 单元）不再初始化，保持原样：这次读取选中内容不拥有该线程的单元。
func comInit() (func(), bool) {
	hr, _, _ := coInitializeEx.Call(0, coinitApartmentThreaded)
	switch uint32(hr) {
	case 0: // S_OK: this call initialized the apartment.
		return func() { coUninitialize.Call() }, true
	case 1: // S_FALSE: already initialized here.
		return func() {}, true
	case rpcEChangedMode:
		// 线程已在另一个单元里，UI Automation 同样可以从那里工作。
		return func() {}, true
	default:
		return func() {}, false
	}
}

// createAutomation 构造 UI Automation 客户端，失败返回 nil。
func createAutomation() comPtr {
	var automation comPtr
	hr, _, _ := coCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidCUIAutomation)),
		0,
		clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIUIAutomation)),
		uintptr(unsafe.Pointer(&automation)),
	)
	if failed(hr) {
		return nil
	}
	return automation
}

// call 调用接口指针 vtable 某槽位的方法。
func call(iface comPtr, slot int, args ...uintptr) uintptr {
	vtable := *(*comPtr)(iface)
	method := *(*uintptr)(unsafe.Add(vtable, uintptr(slot)*unsafe.Sizeof(uintptr(0))))
	result, _, _ := syscall.SyscallN(method, append([]uintptr{uintptr(iface)}, args...)...)
	return result
}

// out 调用通过指针返回结果的方法，返回结果和它的 HRESULT。
func out(iface comPtr, slot int, args ...uintptr) (comPtr, uintptr) {
	var result comPtr
	callArgs := append(append([]uintptr{}, args...), uintptr(unsafe.Pointer(&result)))
	return result, call(iface, slot, callArgs...)
}

// release 释放对接口的引用，表示不再需要该 COM 对象。UI Automation 每次返回都给一个引用，
// 不释放就会让另一进程里的 provider 一直存活。
func release(iface comPtr) {
	if iface != nil {
		call(iface, slotRelease)
	}
}

// failed 报告 HRESULT 是否为失败。
func failed(hr uintptr) bool { return int32(uint32(hr)) < 0 }

// bstr 把 BSTR——COM 返回的字符串类型——读成 Go 字符串并释放它。nil 指针为空串，释放它
// 也允许：SysFreeString 对它不做任何事。
func bstr(value comPtr) string {
	defer sysFreeString.Call(uintptr(value))
	return decodeBSTR(value)
}

// decodeBSTR 读取 BSTR 的布局：指针前四个字节是字节长度，字符是 UTF-16。
//
// 与 bstr 分开以便不依赖 COM 测试：测试自己摆放这些字节再读回。
func decodeBSTR(value comPtr) string {
	if value == nil {
		return ""
	}
	bytes := *(*int32)(unsafe.Add(value, -4))
	if bytes <= 0 {
		return ""
	}
	return string(utf16.Decode(unsafe.Slice((*uint16)(value), bytes/2)))
}

// --- 请应用执行复制 ----------------------------------------

// 合成复制的键码与记录标志，来自 winuser.h。
const (
	vkShift        = 0x10
	vkControl      = 0x11
	vkMenu         = 0x12
	vkLWin         = 0x5B
	vkRWin         = 0x5C
	vkC            = 0x43
	inputKeyboard  = 1
	keyeventfKeyUp = 0x0002
)

// inputRecord 是 SendInput 的一个 INPUT，按平台布局摆放。用记录而非鼠标、键盘、硬件输入的
// 联合体，因为这里只用键盘部分：联合体按最大成员——64 位构建上 32 字节——而 SendInput
// 对尺寸不符的记录会拒绝。
type inputRecord struct {
	kind  uint32
	_     uint32
	vk    uint16
	scan  uint16
	flags uint32
	time  uint32
	_     uint32
	extra uintptr
	_     [8]byte
}

// user32 是窗口管理器：前台窗口、剪贴板写计数器和合成按键都来自它。
var (
	user32 = syscall.NewLazyDLL("user32.dll")

	procGetForegroundWindow        = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessID   = user32.NewProc("GetWindowThreadProcessId")
	procGetClassNameW              = user32.NewProc("GetClassNameW")
	procGetClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber")
	procSendInput                  = user32.NewProc("SendInput")
)

// clipboardSequence 是剪贴板自带的写计数器：每次剪贴板被写入，无论谁写的，系统都会递增。
// 它判断应用是否真的响应了复制——没有选中的应用返回空，计数器不动。
func clipboardSequence() uint32 {
	value, _, _ := procGetClipboardSequenceNumber.Call()
	return uint32(value)
}

// safeToCopy 报告前台应用是否可以请求复制。
//
// 有两种窗口不行：本应用自己的——请它复制等于复制空，而且它不是用户选中的窗口；以及控制台——
// 在那里复制组合键是中断当前进程的信号，不是复制。
func safeToCopy() bool {
	foreground, _, _ := procGetForegroundWindow.Call()
	if foreground == 0 {
		return false
	}
	var owner uint32
	procGetWindowThreadProcessID.Call(foreground, uintptr(unsafe.Pointer(&owner)))
	if owner == uint32(os.Getpid()) {
		return false
	}
	switch windowClass(foreground) {
	case "ConsoleWindowClass", "CASCADIA_HOSTING_WINDOW_CLASS", "mintty":
		return false
	}
	return true
}

// windowClass 是窗口的类名：据此区分控制台和编辑器。
func windowClass(hwnd uintptr) string {
	buffer := make([]uint16, 256)
	length, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	return syscall.UTF16ToString(buffer[:length])
}

// pressCopy 按 Ctrl+C，先松开用户仍按着的东西：触发本功能的组合键就是其中之一，在 Ctrl 和
// Shift 按下时做的复制，应用可能当成别的东西。
func pressCopy() {
	releaseHeldModifiers()
	sendKey(vkControl, false)
	sendKey(vkC, false)
	sendKey(vkC, true)
	sendKey(vkControl, true)
}

// releaseHeldModifiers 对每个修饰键发送 key-up，这是其他桌面翻译工具在合成复制前做的：
// 否则用户仍按着的修饰键会贯穿整个复制过程。
func releaseHeldModifiers() {
	for _, key := range []uint16{vkShift, vkControl, vkMenu, vkLWin, vkRWin} {
		sendKey(key, true)
	}
	time.Sleep(10 * time.Millisecond)
}

// sendKey 按下或松开一个键。size 是 SendInput 按下前检查的尺寸。
func sendKey(vk uint16, up bool) {
	record := inputRecord{kind: inputKeyboard, vk: vk}
	if up {
		record.flags = keyeventfKeyUp
	}
	procSendInput.Call(1, uintptr(unsafe.Pointer(&record)), unsafe.Sizeof(record))
}
