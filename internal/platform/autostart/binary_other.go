//go:build !windows && !darwin

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
)

// BinaryPath 返回登录启动会运行的程序。
//
// 该桌面没有 Boku 能注册的登录启动，登录时不会运行任何东西——但返回值仍是运行中的程序，
// 因为这是该 section 唯一能说的"应用在哪"。各平台都需要它，以便设置代码保持同一形状。
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
