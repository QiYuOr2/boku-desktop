//go:build windows

package codex

import (
	"fmt"
	"os/exec"

	"boku/internal/domain"
)

// findLoginCommand 在 Windows 给进程的 PATH 上查找 CLI。
func findLoginCommand(name string) (string, error) { return exec.LookPath(name) }

// startLogin 在独立的控制台窗口中打开 CLI 的登录。窗口用 `cmd /k` 保持打开，方便
// 用户阅读登录输出；标题参数的两对引号是 `start` 在命令前所要求的。
func startLogin(command string) error {
	if err := exec.Command("cmd", "/c", "start", "", "cmd", "/k", command).Start(); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrLoginUnsupported, err)
	}
	return nil
}
