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

// findLoginCommand looks the CLI up the way a terminal would, not the way a
// GUI process can.
//
// A process launched from Finder or a LaunchAgent inherits launchd's PATH,
// which usually has none of the places a developer installs a CLI in:
// Homebrew (/opt/homebrew/bin), npm, pnpm, Volta and Bun all live outside it.
// Looking only at that PATH is why the login button used to say "not
// installed" on a machine where `codex` worked in a terminal.
//
// The order is: the process PATH (a terminal launch, or an explicit override),
// the user's login shell (the PATH they actually use), then the usual install
// locations. The shell is asked with a timeout because a shell profile can be
// slow or interactive.
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

// commandInLoginShell asks the user's login shell where a command is. The
// output can contain profile noise, so only an absolute path that exists is
// accepted; the last such line wins, because a profile may print something
// that looks like a path before the shell answers.
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

// commonCommandPaths are the places a CLI installed by the usual macOS
// package managers lives. They are a fallback for a shell that cannot be
// asked; a path that does not exist is simply skipped.
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

// startLogin opens the CLI's login in Terminal. The command runs from a
// .command script rather than from the executable itself: Terminal would get
// the binary and no arguments, and what the user needs is `codex login`.
//
// The script is left in the system temporary directory: Terminal reads it
// after `open` returns, and removing it at once would race that read. macOS
// cleans the directory.
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

// writeLoginScript writes the temporary .command Terminal opens.
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

// loginScript is the shell script Terminal runs. The command is quoted as one
// word so that a path with a space in it still works.
func loginScript(command string) string {
	return "#!/bin/zsh\nexec " + shellQuote(command) + " login\n"
}

// shellQuote quotes one word for a POSIX shell.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
