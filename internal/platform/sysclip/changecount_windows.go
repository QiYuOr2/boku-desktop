//go:build windows

package sysclip

import "syscall"

// user32 is the window manager: the clipboard's write counter comes from it,
// and so does the sequence number every Windows clipboard reader compares.
var (
	user32                         = syscall.NewLazyDLL("user32.dll")
	procGetClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber")
)

// clipboardChangeCount is the clipboard's write counter. The system raises it
// every time the clipboard is written to, whoever wrote it; a value of zero
// means the system did not answer, not that the clipboard has never changed.
func clipboardChangeCount() uint32 {
	value, _, _ := procGetClipboardSequenceNumber.Call()
	return uint32(value)
}
