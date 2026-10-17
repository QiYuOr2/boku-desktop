//go:build darwin

package sysclip

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
)

// readClipboardText negotiates the platform's text formats on macOS. Some
// applications put a copy on the pasteboard only as public.utf16-plain-text
// (or another legacy string type), which the legacy ReadText helper — it asks
// for public.utf8-plain-text alone — does not see. The generic read maps those
// types to transfer.Text and converts the value to a string.
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
