//go:build !windows

package codexlogin

import (
	"fmt"
	"os/exec"
	"runtime"

	"boku/internal/domain"
)

// startLogin opens the CLI's login in a terminal window. It tries the
// terminals a desktop Linux usually has, and on macOS the Terminal
// application; a machine with none of them is told so rather than left
// waiting for a window that will not appear.
func startLogin(command string) error {
	candidates := [][]string{
		{"x-terminal-emulator", "-e", command},
		{"gnome-terminal", "--", command},
		{"konsole", "-e", command},
		{"xterm", "-e", command},
	}
	if runtime.GOOS == "darwin" {
		candidates = [][]string{{"open", "-a", "Terminal", command}}
	}
	for _, candidate := range candidates {
		path, err := exec.LookPath(candidate[0])
		if err != nil {
			continue
		}
		if err := exec.Command(path, candidate[1:]...).Start(); err != nil {
			return fmt.Errorf("%w: %v", domain.ErrLoginUnsupported, err)
		}
		return nil
	}
	return fmt.Errorf("%w: run %s in a terminal", domain.ErrLoginUnsupported, command)
}
