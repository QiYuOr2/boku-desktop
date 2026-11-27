//go:build !windows && !darwin

package codex

import (
	"fmt"
	"os/exec"

	"boku/internal/domain"
)

// findLoginCommand 在桌面会话给进程的 PATH 上查找 CLI。
func findLoginCommand(name string) (string, error) { return exec.LookPath(name) }

// startLogin 在终端窗口中打开 CLI 的登录。它依次尝试桌面 Linux 常见的终端；一台都没有
// 的机器会被告知，而不是空等一个不会出现的窗口。
func startLogin(command string) error {
	candidates := [][]string{
		{"x-terminal-emulator", "-e", command},
		{"gnome-terminal", "--", command},
		{"konsole", "-e", command},
		{"xterm", "-e", command},
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
