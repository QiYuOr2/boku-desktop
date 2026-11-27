//go:build windows

package sysclip

import "syscall"

// user32 提供剪贴板的写计数器（GetClipboardSequenceNumber），
// 也是所有 Windows 剪贴板读取方比较的序列号来源。
var (
	user32                         = syscall.NewLazyDLL("user32.dll")
	procGetClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber")
)

// clipboardChangeCount 返回剪贴板写计数器：系统每次写入都会递增，无论谁写。
// 返回 0 表示系统没有给出答案，而不是剪贴板从未变化。
func clipboardChangeCount() uint32 {
	value, _, _ := procGetClipboardSequenceNumber.Call()
	return uint32(value)
}
