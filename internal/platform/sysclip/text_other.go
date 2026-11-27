//go:build !windows && !darwin

package sysclip

import "github.com/egoist/mygo"

// readClipboardText 是无需特殊处理的平台上的文本实现：旧版 helper 一直是这些
// 构建所用的。
func readClipboardText() string { return mygo.Clipboard.ReadText() }
