//go:build darwin

package codex

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"boku/internal/domain"
)

// findLoginCommand 以终端的方式查找 CLI，而不是以 GUI 进程能做到的方式。
//
// 从 Finder 或 LaunchAgent 启动的进程继承 launchd 的 PATH，其中通常不含开发者安装
// CLI 的位置：Homebrew（/opt/homebrew/bin）、npm、pnpm、Volta、Bun 都在其外。只看
// 这个 PATH 会导致在 codex 终端可用的机器上，登录按钮报「未安装」。
//
// 查找顺序：进程 PATH（终端启动或显式覆盖）、用户登录 shell（实际使用的 PATH）、再是
// 常见安装位置。询问 shell 时加超时，因为 shell 配置可能很慢或交互式。
func findLoginCommand(name string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	for _, flag := range []string{"-lc", "-lic"} {
		if path := commandInLoginShell(name, flag); path != "" {
			return path, nil
		}
	}
	for _, path := range commonCommandPaths(name) {
		if isExecutable(path) {
			return path, nil
		}
	}
	return "", exec.ErrNotFound
}

// commandInLoginShell 询问用户登录 shell 某命令的位置。输出可能夹带 shell 配置的
// 杂讯，因此只接受存在且为绝对路径的行；取最后一条，因为配置可能在 shell 回答前先
// 打印出看似路径的内容。
func commandInLoginShell(name, flag string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/bin/zsh", flag, "command -v "+shellQuote(name)).Output()
	if err != nil {
		return ""
	}
	found := ""
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if filepath.IsAbs(line) && isExecutable(line) {
			found = line
		}
	}
	return found
}

// commonCommandPaths 是常见 macOS 包管理器安装 CLI 的位置，作为无法询问 shell 时的
// 兜底；不存在的路径直接跳过。
func commonCommandPaths(name string) []string {
	paths := []string{
		filepath.Join("/opt/homebrew/bin", name),
		filepath.Join("/usr/local/bin", name),
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths,
			filepath.Join(home, ".local", "bin", name),
			filepath.Join(home, ".bun", "bin", name),
			filepath.Join(home, ".volta", "bin", name),
			filepath.Join(home, ".npm-global", "bin", name),
			filepath.Join(home, "Library", "pnpm", name),
		)
	}
	return paths
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

// startLogin 在 Terminal 中打开 CLI 的登录。命令经由 .command 脚本执行，而不是
// 直接执行二进制：直接执行会让 Terminal 拿到二进制却没有参数，而用户需要的是
// `codex login`。
//
// 脚本留在系统临时目录：Terminal 是在 `open` 返回后才读取它，立即删除会与这次读取
// 竞争。macOS 会清理该目录。
func startLogin(command string) error {
	script, err := writeLoginScript(command)
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrLoginUnsupported, err)
	}
	out, err := exec.Command("/usr/bin/open", "-a", "Terminal", script).CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("%w: %s", domain.ErrLoginUnsupported, message)
	}
	return nil
}

// writeLoginScript 写出 Terminal 打开的临时 .command 脚本。
func writeLoginScript(command string) (string, error) {
	dir, err := os.MkdirTemp("", "boku-codex-login-")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "codex-login.command")
	if err := os.WriteFile(path, []byte(loginScript(command)), 0o700); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return path, nil
}

// loginScript 是 Terminal 运行的 shell 脚本。命令作为单个词加引号，使含空格的路径
// 也能工作。
func loginScript(command string) string {
	return "#!/bin/zsh\nexec " + shellQuote(command) + " login\n"
}

// shellQuote 为 POSIX shell 引用单个词。
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
