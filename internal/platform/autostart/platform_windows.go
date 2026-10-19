//go:build windows

package autostart

// New returns this desktop's login start. It is the one place main asks
// which mechanism the platform has, and the answer is a value rather than a
// behaviour switch in the assembly.
func New() Manager { return NewWindows() }
