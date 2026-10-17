//go:build !windows && !darwin

package sysclip

import "github.com/egoist/mygo"

// readClipboardText is the text flavour of the platforms that have no
// special case: the legacy helper is what this build has always used.
func readClipboardText() string { return mygo.Clipboard.ReadText() }
