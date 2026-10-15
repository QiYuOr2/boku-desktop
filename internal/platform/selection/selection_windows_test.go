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

// The platform half of this package needs a desktop session and another
// application to read from, which a test process does not have. What can be
// tested here is the reading of the one type COM answers with, and the
// HRESULT test every call is judged by; the reading of a real selection is
// checked by hand, with TestReadTheSelectionByHand.

// TestDecodeBSTRReadsThePlatformsLayout lays a BSTR out the way COM does —
// its length in bytes in the four bytes before the pointer, then UTF-16 — and
// reads it back: the length is honoured, an empty string is not a missing
// one, and characters outside the basic plane survive.
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

// TestFailedReadsHRESULTs: a COM call reports failure in the sign of the top
// bit, and every call in this package is judged by that.
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

// TestReadTheSelectionByHand asks the system what is selected in whatever
// application has the keyboard and prints it.
//
// It is a manual check, not a test: it needs a desktop session, a second
// application and a human to select something in it, so it is skipped unless
// its environment variable is set. Select a sentence in a browser or an
// editor, then run:
//
//	BOKU_SELECTION_PROBE=1 go test ./internal/data/selection -run ByHand -v
func TestReadTheSelectionByHand(t *testing.T) {
	if os.Getenv("BOKU_SELECTION_PROBE") == "" {
		t.Skip("set BOKU_SELECTION_PROBE=1, select text in another window, then run this")
	}
	text, err := New(Options{}).SelectedText(context.Background())
	fmt.Printf("selection: %q\nerror:     %v\n", text, err)
}
