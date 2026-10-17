//go:build windows

package sysclip

import "github.com/egoist/mygo"

// readClipboardText is the Windows text flavour: the legacy helper reads
// CF_UNICODETEXT, which is the one format every text copy on Windows offers.
func readClipboardText() string { return mygo.Clipboard.ReadText() }
