//go:build windows

package selection

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"testing"
	"unicode/utf16"
	"unsafe"
)

// 本包的平台部分是读取真实选中内容，需要桌面会话和另一个应用，测试进程没有。这里能测的
// 是 COM 返回的唯一类型的解析和判定每次调用的 HRESULT 测试；真实选中的读取由
// TestReadTheSelectionByHand 手动检查。

// TestDecodeBSTRReadsThePlatformsLayout 按 COM 的布局摆放 BSTR——指针前四个字节是字节长度，
// 随后是 UTF-16——再读回：长度被遵循，空串不等于缺失，基本平面外的字符也能保留。
func TestDecodeBSTRReadsThePlatformsLayout(t *testing.T) {
	build := func(s string) []byte {
		units := utf16.Encode([]rune(s))
		buffer := make([]byte, 4+2*len(units)+2)
		binary.LittleEndian.PutUint32(buffer, uint32(2*len(units)))
		for index, unit := range units {
			binary.LittleEndian.PutUint16(buffer[4+2*index:], unit)
		}
		return buffer
	}
	for _, want := range []string{"", "hello", "コンパイラはソフトです。", "a😀b", "with space"} {
		buffer := build(want)
		if got := decodeBSTR(unsafe.Pointer(&buffer[4])); got != want {
			t.Errorf("decodeBSTR = %q, want %q", got, want)
		}
	}
	if got := decodeBSTR(nil); got != "" {
		t.Errorf("decodeBSTR(nil) = %q, want nothing", got)
	}
}

// TestFailedReadsHRESULTs：COM 调用以最高位的符号表示失败，本包每次调用都据此判定。
func TestFailedReadsHRESULTs(t *testing.T) {
	cases := []struct {
		hr   uintptr
		want bool
	}{
		{0x00000000, false}, // S_OK
		{0x00000001, false}, // S_FALSE
		{0x80070057, true},  // E_INVALIDARG
		{0x80004002, true},  // E_NOINTERFACE
		{0x80010106, true},  // RPC_E_CHANGED_MODE
	}
	for _, tc := range cases {
		if got := failed(tc.hr); got != tc.want {
			t.Errorf("failed(0x%08X) = %v, want %v", uint32(tc.hr), got, tc.want)
		}
	}
}

// TestReadTheSelectionByHand 询问系统当前占据键盘的应用里选中了什么并打印出来。
//
// 这是手动检查而非测试：需要桌面会话、第二个应用和一个人在其中选中内容，因此仅在设置了
// 环境变量时运行。在浏览器或编辑器里选中一句话，然后运行：
//
//	BOKU_SELECTION_PROBE=1 go test ./internal/data/selection -run ByHand -v
func TestReadTheSelectionByHand(t *testing.T) {
	if os.Getenv("BOKU_SELECTION_PROBE") == "" {
		t.Skip("set BOKU_SELECTION_PROBE=1, select text in another window, then run this")
	}
	text, err := New(Options{}).SelectedText(context.Background())
	fmt.Printf("selection: %q\nerror:     %v\n", text, err)
}
