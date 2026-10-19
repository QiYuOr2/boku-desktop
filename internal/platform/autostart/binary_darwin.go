//go:build darwin

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
)

// BinaryPath is the program the application was started from.
//
// The macOS login item registers the application bundle rather than this
// path, but the settings page still asks for the program to answer "where am
// I", and the legacy launch agent cleanup compares it with the old document.
func BinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate the executable: %w", err)
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return exe, nil
}
