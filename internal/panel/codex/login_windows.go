//go:build windows

package codex

import (
	"fmt"
	"os/exec"

	"boku/internal/domain"
)

// findLoginCommand looks the CLI up on the PATH Windows gives the process.
func findLoginCommand(name string) (string, error) { return exec.LookPath(name) }

// startLogin opens the CLI's login in a console window of its own. The window
// is kept open (`cmd /k`) so that the user can read what the login said; the
// quotes around the title are what `start` expects before the command.
func startLogin(command string) error {
	if err := exec.Command("cmd", "/c", "start", "", "cmd", "/k", command).Start(); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrLoginUnsupported, err)
	}
	return nil
}
