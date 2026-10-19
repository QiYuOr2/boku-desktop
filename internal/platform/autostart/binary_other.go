//go:build !windows && !darwin

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
)

// BinaryPath is the program a login start would run.
//
// This desktop has no login start Boku can register, so nothing ever runs at
// login — but the answer is still the running program, because it is the one
// thing the section can say about where the application is. The package needs
// it on every platform so that the settings code has one shape.
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
