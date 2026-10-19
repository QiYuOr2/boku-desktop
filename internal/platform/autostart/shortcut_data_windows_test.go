//go:build windows

package autostart

import (
	"os"
	"path/filepath"
	"testing"
)

// The shortcut reader, against a real shortcut.
//
// testdata/shortcut.lnk was made by Windows itself: a shortcut on a machine
// with no space in its account name, pointing at C:\Windows\System32\
// notepad.exe through the system's ANSI code page. It is the case that a
// reader written from the specification alone gets wrong, so it is a fixture
// rather than a document assembled in the test.

func TestLnkTargetReadsARealShortcut(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "shortcut.lnk"))
	if err != nil {
		t.Fatalf("read the fixture: %v", err)
	}
	target, err := lnkTarget(data)
	if err != nil {
		t.Fatalf("lnkTarget failed: %v", err)
	}
	if want := `C:\Windows\System32\notepad.exe`; !equalPath(target, want) {
		t.Errorf("target = %q, want %q", target, want)
	}
}

// equalPath compares two Windows paths the way Windows does: case-insensitively,
// and without the whitespace the shell can leave around the halves of one.
func equalPath(a, b string) bool {
	return len(a) == len(b) && (a == b || lowerPath(a) == lowerPath(b))
}

// lowerPath is the ASCII part of case folding, which is all a path needs.
func lowerPath(path string) string {
	path = trimLnkPath(path)
	lower := make([]byte, len(path))
	for index := 0; index < len(path); index++ {
		char := path[index]
		if char >= 'A' && char <= 'Z' {
			char += 'a' - 'A'
		}
		lower[index] = char
	}
	return string(lower)
}
