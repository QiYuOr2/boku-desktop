//go:build darwin

package selection

import (
	"bytes"
	"context"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"

	"boku/internal/domain"
)

// 优先通过辅助功能 API 读取选中文本：焦点元素会发布其选中文本，读取它既不干扰应用
// 也不碰剪贴板。该接口需要在「系统设置 > 隐私与安全性 > 辅助功能」中授权；没有权限时
// 回退到下面的复制方案，所以两者放在一起。
//
// 复制方案与 Windows 相同（selection.go）：按下平台的复制组合键，监视剪贴板的写计数器
// 判断是否有选中内容，读取文本，再把剪贴板还原。平台相关部分只有按键和计数器。
//
// 两者都不可用（权限被拒且应用不响应复制）属于「平台无法回答」的常见情况，调用方回退到
// 已有的剪贴板。
func New(opts Options) domain.SelectionDevice {
	return Device{
		clipboard:  opts.Clipboard,
		beforeCopy: opts.BeforeCopy,
		sequence:   opts.Sequence,
	}
}

// Device 通过 macOS 的辅助功能 API 读取选中文本，读不到时请应用复制。
type Device struct {
	// clipboard 是复制方案读取并还原的剪贴板。
	clipboard domain.ClipboardDevice
	// beforeCopy 在合成复制前调用，以便剪贴板工具把借来的值排除在历史之外。
	beforeCopy func()
	// sequence 是剪贴板的写计数器。没有它的设备能读辅助功能发布的内容，
	// 但无法区分「复制了空」和「又复制了同样的文本」。
	sequence func() uint32
}

// SelectedText 询问 macOS 用户当前应用里选中了什么。
//
// 与 Windows 一样会重试几次：浏览器首次被询问时才开启辅助功能树，第一次可能返回空，
// 而选中内容就在屏幕上。挂起的调用在读取时限后被放弃；复制方案是更旧的答案，始终可用。
func (d Device) SelectedText(ctx context.Context) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	deadline := time.Now().Add(DefaultTimeout)
	if text := readUntilAnswered(ctx, readAXWithin, DefaultAttempts, DefaultRetryGap, deadline); text != "" {
		return text, nil
	}
	if d.clipboard != nil && d.sequence != nil && canPostEvents() && !secureInput() {
		if text := readWithACopy(ctx, d.clipboard, d.beforeCopy, d.sequence, pressCopy); text != "" {
			return text, nil
		}
	}
	return "", domain.ErrSelectionUnavailable
}

// readAXWithin 在独立线程上做一次读取，并带时限等待：该调用属于另一个应用的辅助功能
// 实现，可能不返回。
func readAXWithin(ctx context.Context, within time.Duration) string {
	done := make(chan string, 1)
	go func() { done <- readSelectionAX() }()
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

// readSelectionAX 读取焦点元素的选中文本，或返回空。无权限、元素不发布文本、空选中和
// 调用失败在这里都是同一个答案。
func readSelectionAX() string {
	if !loadAX() || !axIsProcessTrusted() {
		return ""
	}
	system := axCreateSystemWide()
	if system == 0 {
		return ""
	}
	defer cfRelease(system)

	focused := axCopyAttribute(system, axFocusedElement)
	if focused == 0 {
		return ""
	}
	defer cfRelease(focused)

	selected := axCopyAttribute(focused, axSelectedText)
	if selected == 0 {
		return ""
	}
	defer cfRelease(selected)
	return strings.TrimSpace(cfString(selected))
}

// --- 辅助功能与 Core Foundation，以直接调用方式使用 --------------------

const kCFStringEncodingUTF8 = 0x08000100

var (
	axOnce sync.Once
	axOK   bool

	cfRelease                         func(ref uintptr)
	cfDictionaryCreate                func(allocator uintptr, keys, values *uintptr, numValues int64, keyCallBacks, valueCallBacks uintptr) uintptr
	cfGetTypeID                       func(ref uintptr) uintptr
	cfStringGetTypeID                 func() uintptr
	cfStringCreateWithCString         func(allocator uintptr, cStr *byte, encoding uint32) uintptr
	cfStringGetLength                 func(str uintptr) int64
	cfStringGetMaximumSizeForEncoding func(length int64, encoding uint32) int64
	cfStringGetCString                func(str uintptr, buffer *byte, size int64, encoding uint32) uint8
	axCreateSystemWide                func() uintptr
	axCopyAttributeValue              func(element uintptr, attribute uintptr, value *uintptr) int32
	axIsProcessTrustedFunc            func() uint8
	axIsProcessTrustedWithOptionsFunc func(options uintptr) uint8

	axFocusedElement uintptr
	axSelectedText   uintptr

	axTrustedCheckOptionPrompt uintptr
	cfBooleanTrue              uintptr
	cfDictionaryKeyCallBacks   uintptr
	cfDictionaryValueCallBacks uintptr
)

func loadAX() bool {
	axOnce.Do(func() {
		coreFoundation, err := purego.Dlopen(
			"/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation",
			purego.RTLD_NOW|purego.RTLD_GLOBAL,
		)
		if err != nil {
			return
		}
		applicationServices, err := purego.Dlopen(
			"/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices",
			purego.RTLD_NOW|purego.RTLD_GLOBAL,
		)
		if err != nil {
			return
		}
		purego.RegisterLibFunc(&cfRelease, coreFoundation, "CFRelease")
		purego.RegisterLibFunc(&cfDictionaryCreate, coreFoundation, "CFDictionaryCreate")
		purego.RegisterLibFunc(&cfGetTypeID, coreFoundation, "CFGetTypeID")
		purego.RegisterLibFunc(&cfStringGetTypeID, coreFoundation, "CFStringGetTypeID")
		purego.RegisterLibFunc(&cfStringCreateWithCString, coreFoundation, "CFStringCreateWithCString")
		purego.RegisterLibFunc(&cfStringGetLength, coreFoundation, "CFStringGetLength")
		purego.RegisterLibFunc(&cfStringGetMaximumSizeForEncoding, coreFoundation, "CFStringGetMaximumSizeForEncoding")
		purego.RegisterLibFunc(&cfStringGetCString, coreFoundation, "CFStringGetCString")
		purego.RegisterLibFunc(&axCreateSystemWide, applicationServices, "AXUIElementCreateSystemWide")
		purego.RegisterLibFunc(&axCopyAttributeValue, applicationServices, "AXUIElementCopyAttributeValue")
		purego.RegisterLibFunc(&axIsProcessTrustedFunc, applicationServices, "AXIsProcessTrusted")
		purego.RegisterLibFunc(&axIsProcessTrustedWithOptionsFunc, applicationServices, "AXIsProcessTrustedWithOptions")

		axTrustedCheckOptionPrompt = dataReference(applicationServices, "kAXTrustedCheckOptionPrompt")
		cfBooleanTrue = dataReference(coreFoundation, "kCFBooleanTrue")
		cfDictionaryKeyCallBacks = dataAddress(coreFoundation, "kCFTypeDictionaryKeyCallBacks")
		cfDictionaryValueCallBacks = dataAddress(coreFoundation, "kCFTypeDictionaryValueCallBacks")

		axFocusedElement = newCFString("AXFocusedUIElement")
		axSelectedText = newCFString("AXSelectedText")
		axOK = axFocusedElement != 0 && axSelectedText != 0
	})
	return axOK
}

func axIsProcessTrusted() bool {
	return axIsProcessTrustedFunc != nil && axIsProcessTrustedFunc() != 0
}

// RequestSelectionAccess 请系统显示辅助功能授权提示。应用不能自行授予权限，且 macOS
// 在应用请求前不会在系统设置里列出它；这个调用就是发起请求。返回值是当前是否已授权：
// 提示后仍为 false 表示用户还需手动打开开关。
func (d Device) RequestSelectionAccess() bool {
	if !loadAX() {
		return false
	}
	if axIsProcessTrusted() {
		return true
	}
	return requestAccessibilityPrompt()
}

// SelectionAccessGranted 报告当前权限，不发起请求。
func (d Device) SelectionAccessGranted() bool {
	return loadAX() && axIsProcessTrusted()
}

// requestAccessibilityPrompt 是提示那一半，由设置按钮和首次复制回退共用。它返回当前
// 权限，而非用户的选择：系统不会告诉应用用户选了什么。
func requestAccessibilityPrompt() bool {
	if !loadAX() || axIsProcessTrustedWithOptionsFunc == nil {
		return false
	}
	if axTrustedCheckOptionPrompt == 0 || cfBooleanTrue == 0 ||
		cfDictionaryCreate == nil || cfDictionaryKeyCallBacks == 0 || cfDictionaryValueCallBacks == 0 {
		return false
	}
	keys := []uintptr{axTrustedCheckOptionPrompt}
	values := []uintptr{cfBooleanTrue}
	options := cfDictionaryCreate(0, &keys[0], &values[0], 1, cfDictionaryKeyCallBacks, cfDictionaryValueCallBacks)
	if options == 0 {
		return false
	}
	defer cfRelease(options)
	return axIsProcessTrustedWithOptionsFunc(options) != 0
}

// dataReference 读取全局变量里保存的 Core Foundation 对象，如 kAXTrustedCheckOptionPrompt
// 或 kCFBooleanTrue。
func dataReference(handle uintptr, name string) uintptr {
	p, err := purego.Dlsym(handle, name)
	if err != nil || p == 0 {
		return 0
	}
	return **(**uintptr)(unsafe.Pointer(&p))
}

// dataAddress 是全局结构体的地址，如字典 key callbacks；C API 要地址，而不是它持有的字节。
func dataAddress(handle uintptr, name string) uintptr {
	address, err := purego.Dlsym(handle, name)
	if err != nil {
		return 0
	}
	return address
}

func axCopyAttribute(element, attribute uintptr) uintptr {
	var value uintptr
	if axCopyAttributeValue(element, attribute, &value) != 0 {
		return 0
	}
	return value
}

// newCFString 构造 Core Foundation 字符串，带 C 构造器读取的 NUL 结尾。上面两个属性名
// 在整个进程生命期内保留，因为它们永不变。
func newCFString(value string) uintptr {
	bytes := append([]byte(value), 0)
	ref := cfStringCreateWithCString(0, &bytes[0], kCFStringEncodingUTF8)
	runtime.KeepAlive(bytes)
	return ref
}

// cfString 把 Core Foundation 字符串读成 Go 字符串。
func cfString(value uintptr) string {
	if value == 0 || cfStringGetLength == nil || cfGetTypeID == nil || cfStringGetTypeID == nil {
		return ""
	}
	if cfGetTypeID(value) != cfStringGetTypeID() {
		// 辅助功能属性可能不是文本；对它调用 CFString 函数会崩溃而非返回错误值。
		return ""
	}
	length := cfStringGetLength(value)
	if length <= 0 {
		return ""
	}
	size := cfStringGetMaximumSizeForEncoding(length, kCFStringEncodingUTF8) + 1
	if size <= 0 || size > 1<<20 {
		return ""
	}
	buffer := make([]byte, size)
	if cfStringGetCString(value, &buffer[0], size, kCFStringEncodingUTF8) == 0 {
		return ""
	}
	if index := bytes.IndexByte(buffer, 0); index >= 0 {
		buffer = buffer[:index]
	}
	return string(buffer)
}

// --- 请应用执行复制 ----------------------------------------

// 合成 Command+C 的虚拟键码与标志，来自 Carbon 的 Events.h 和 CoreGraphics 的 CGEventTypes.h。
const (
	keyCodeCommand = 0x37
	keyCodeShift   = 0x38
	keyCodeOption  = 0x3A
	keyCodeControl = 0x3B
	keyCodeC       = 0x08

	kCGEventFlagMaskCommand                 = 1 << 20
	kCGHIDEventTap                          = 0
	kCGEventSourceStateCombinedSessionState = 0
)

var (
	cgOnce sync.Once
	cgOK   bool

	cgEventCreateKeyboardEvent func(source uintptr, key uint16, down uint8) uintptr
	cgEventSetFlags            func(event uintptr, flags uint64)
	cgEventPost                func(tap uint32, event uintptr)
	cgEventSourceKeyState      func(state uint32, key uint16) uint8
	cgPreflightPostEventAccess func() uint8
)

func loadCoreGraphics() bool {
	cgOnce.Do(func() {
		coreGraphics, err := purego.Dlopen(
			"/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics",
			purego.RTLD_NOW|purego.RTLD_GLOBAL,
		)
		if err != nil {
			return
		}
		coreFoundation, err := purego.Dlopen(
			"/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation",
			purego.RTLD_NOW|purego.RTLD_GLOBAL,
		)
		if err != nil {
			return
		}
		purego.RegisterLibFunc(&cfRelease, coreFoundation, "CFRelease")
		purego.RegisterLibFunc(&cgEventCreateKeyboardEvent, coreGraphics, "CGEventCreateKeyboardEvent")
		purego.RegisterLibFunc(&cgEventSetFlags, coreGraphics, "CGEventSetFlags")
		purego.RegisterLibFunc(&cgEventPost, coreGraphics, "CGEventPost")
		purego.RegisterLibFunc(&cgEventSourceKeyState, coreGraphics, "CGEventSourceKeyState")
		cgPreflightPostEventAccess = dlsymFunc[func() uint8](coreGraphics, "CGPreflightPostEventAccess")
		cgOK = true
	})
	return cgOK
}

// dlsymFunc 返回可调用的符号，本 macOS 版本没有时返回 nil。用于两个访问检查调用，
// 它们在 10.15 才出现。
func dlsymFunc[T any](handle uintptr, name string) T {
	var zero T
	address, err := purego.Dlsym(handle, name)
	if err != nil {
		return zero
	}
	var fn T
	purego.RegisterFunc(&fn, address)
	return fn
}

// canPostEvents 报告本进程是否允许发送键盘事件。
//
// 这是检查，不是请求。在快捷路径中发起请求时 Boku 还是辅助应用（没开窗口），macOS 不会
// 为这种调用显示辅助功能提示，反而会把应用记为未授权。唯一发起请求的地方是设置按钮，
// 那里设置窗口会让应用变为活跃。
func canPostEvents() bool {
	if !loadCoreGraphics() {
		return false
	}
	return cgPreflightPostEventAccess == nil || cgPreflightPostEventAccess() != 0
}

// secureInput 报告是否有安全输入框占据键盘。把密码复制到剪贴板去翻译不是用户想要的，
// 而且工具还原旧值后剪贴板也可能意外保留密码。
func secureInput() bool {
	if secureEventInputEnabled == nil {
		return false
	}
	return secureEventInputEnabled() != 0
}

var secureEventInputEnabled func() uint8

func init() {
	carbon, err := purego.Dlopen(
		"/System/Library/Frameworks/Carbon.framework/Carbon",
		purego.RTLD_NOW|purego.RTLD_GLOBAL,
	)
	if err != nil {
		return
	}
	secureEventInputEnabled = dlsymFunc[func() uint8](carbon, "IsSecureEventInputEnabled")
}

// pressCopy 按 Command+C，先松开用户仍按着的修饰键：触发本功能的快捷键就是其中之一，
// 在 Command 和 Shift 按下时做的复制，应用可能当成别的东西。
func pressCopy() {
	if !loadCoreGraphics() {
		return
	}
	releaseHeldModifiers()
	postKey(keyCodeCommand, true, kCGEventFlagMaskCommand)
	postKey(keyCodeC, true, kCGEventFlagMaskCommand)
	postKey(keyCodeC, false, kCGEventFlagMaskCommand)
	postKey(keyCodeCommand, false, 0)
}

// releaseHeldModifiers 对每个仍按下的修饰键发送 key-up。
func releaseHeldModifiers() {
	for _, key := range []uint16{keyCodeShift, keyCodeControl, keyCodeOption, keyCodeCommand} {
		if cgEventSourceKeyState(kCGEventSourceStateCombinedSessionState, key) != 0 {
			postKey(key, false, 0)
		}
	}
	time.Sleep(10 * time.Millisecond)
}

// postKey 发送一个键盘事件。事件以 autorelease 等价（+1）创建，发送后释放。
func postKey(key uint16, down bool, flags uint64) {
	value := uint8(0)
	if down {
		value = 1
	}
	event := cgEventCreateKeyboardEvent(0, key, value)
	if event == 0 {
		return
	}
	if flags != 0 {
		cgEventSetFlags(event, flags)
	}
	cgEventPost(kCGHIDEventTap, event)
	cfRelease(event)
}
