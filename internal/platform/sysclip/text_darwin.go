//go:build darwin

package sysclip

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
)

// readClipboardText 在 macOS 上协商平台的文本格式。有些应用只把内容以
// public.utf16-plain-text（或其它旧式字符串类型）放进粘贴板，而旧版 ReadText
// 只请求 public.utf8-plain-text，看不到它们。通用读取会把这些类型映射为
// transfer.Text 并转成字符串。
func readClipboardText() string {
	data, err := mygo.Clipboard.Read(transfer.Text)
	if err != nil {
		return ""
	}
	text, err := data.Read(transfer.Text)
	if err != nil {
		return ""
	}
	return string(text)
}
