//go:build !windows && !darwin

package sysclip

// clipboardChangeCount answers zero: this build has no clipboard write
// counter, so the picture cache falls back to its time window.
func clipboardChangeCount() uint32 { return 0 }
