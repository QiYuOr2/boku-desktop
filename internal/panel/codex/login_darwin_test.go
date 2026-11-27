//go:build darwin

package codex

import (
	"os"
	"testing"
)

// Terminal 运行的脚本必须启动 CLI 的登录，而非 CLI 本身：`open -a Terminal
// <binary>` 不会给它参数。
func TestLoginScriptRunsTheLoginCommand(t *testing.T) {
	got := loginScript("/opt/homebrew/bin/codex")
	want := "#!/bin/zsh\nexec '/opt/homebrew/bin/codex' login\n"
	if got != want {
		t.Errorf("loginScript = %q, want %q", got, want)
	}
}

// 含空格的路径对 shell 而言仍是单个词。
func TestLoginScriptQuotesTheCommandPath(t *testing.T) {
	got := loginScript("/Users/me/My Tools/codex")
	want := "#!/bin/zsh\nexec '/Users/me/My Tools/codex' login\n"
	if got != want {
		t.Errorf("loginScript = %q, want %q", got, want)
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"/opt/homebrew/bin/codex": "'/opt/homebrew/bin/codex'",
		"/Users/o'brien/codex":    `'/Users/o'\''brien/codex'`,
		"":                        "''",
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

// 进程 PATH 仍是第一答案：终端启动或测试不需要登录 shell。
func TestFindLoginCommandUsesPATH(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/codex"
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	got, err := findLoginCommand("codex")
	if err != nil {
		t.Fatalf("findLoginCommand: %v", err)
	}
	if got != path {
		t.Errorf("findLoginCommand = %q, want %q", got, path)
	}
}
