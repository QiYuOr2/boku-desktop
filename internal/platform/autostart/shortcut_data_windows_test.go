//go:build windows

package autostart

import (
	"os"
	"path/filepath"
	"testing"
)

// 快捷方式读取器，针对真实快捷方式测试。
//
// testdata/shortcut.lnk 由 Windows 自己生成：来自账户名不含空格的机器，经系统 ANSI 代码页指向
// C:\Windows\System32\notepad.exe。这正是仅按规范写出的读取器会读错的情形，因此它是 fixture，
// 而不是测试里拼出的文档。

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

// equalPath 按 Windows 的方式比较两个路径：不区分大小写，且忽略 shell 可能留在各部分两端的空白。
func equalPath(a, b string) bool {
	return len(a) == len(b) && (a == b || lowerPath(a) == lowerPath(b))
}

// lowerPath 只做 ASCII 部分的大小写折叠，路径够用。
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
