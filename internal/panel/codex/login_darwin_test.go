//go:build darwin

package codex

import (
	"os"
	"testing"
)

// The script Terminal runs must start the CLI's login, not the CLI itself:
// `open -a Terminal <binary>` would give it no arguments.
func TestLoginScriptRunsTheLoginCommand(t *testing.T) {
	got := loginScript("/opt/homebrew/bin/codex")
	want := "#!/bin/zsh\nexec '/opt/homebrew/bin/codex' login\n"
	if got != want {
		t.Errorf("loginScript = %q, want %q", got, want)
	}
}

// A path with a space is still one word to the shell.
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

// The process PATH is still the first answer: a terminal launch, or a test,
// does not need the login shell.
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
