//go:build darwin

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
)

// BinaryPath 返回应用启动时的程序路径。
//
// macOS 登录项注册的是应用包而非此路径，但设置页仍需程序路径来回答"我在哪"，旧 launch agent 清理也要用它比对旧文档。
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
