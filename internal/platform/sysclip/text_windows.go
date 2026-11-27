//go:build windows

package sysclip

import "github.com/egoist/mygo"

// readClipboardText 是 Windows 的文本实现：旧版 helper 读取 CF_UNICODETEXT，
// 这是 Windows 上每次文本复制都会提供的格式。
func readClipboardText() string { return mygo.Clipboard.ReadText() }
