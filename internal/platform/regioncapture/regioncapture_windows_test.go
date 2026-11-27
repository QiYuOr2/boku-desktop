//go:build windows

package regioncapture

import (
	"bytes"
	"context"
	"image"
	_ "image/png"
	"os"
	"testing"
	"time"
	"unsafe"
)

func TestEncodeBGRAProducesAnOpaquePNGWithCorrectChannels(t *testing.T) {
	// 两个像素按 Windows 原生 BGRA 字节序排列：先红后蓝。
	pngBytes, err := encodeBGRA(2, 1, []byte{0, 0, 255, 255, 255, 0, 0, 255})
	if err != nil {
		t.Fatalf("encodeBGRA() error = %v", err)
	}
	decoded, _, err := image.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("decode generated PNG: %v", err)
	}
	if got := decoded.Bounds(); got != image.Rect(0, 0, 2, 1) {
		t.Fatalf("bounds = %v, want 2x1", got)
	}
	if r, g, b, a := decoded.At(0, 0).RGBA(); r != 0xffff || g != 0 || b != 0 || a != 0xffff {
		t.Errorf("first pixel = (%#x, %#x, %#x, %#x), want opaque red", r, g, b, a)
	}
	if r, g, b, a := decoded.At(1, 0).RGBA(); r != 0 || g != 0 || b != 0xffff || a != 0xffff {
		t.Errorf("second pixel = (%#x, %#x, %#x, %#x), want opaque blue", r, g, b, a)
	}
}

func TestEncodeBGRARejectsIncompletePixels(t *testing.T) {
	if _, err := encodeBGRA(1, 1, []byte{0, 0, 0}); err == nil {
		t.Fatal("encodeBGRA() error = nil, want invalid bitmap pixels")
	}
}

// TestCaptureDesktopByHand 是实际 GDI 路径的按需冒烟测试：不落盘、不打印像素。
// 仅在交互式桌面会话中设置 BOKU_REGION_CAPTURE_PROBE=1 时运行。
func TestCaptureDesktopByHand(t *testing.T) {
	if os.Getenv("BOKU_REGION_CAPTURE_PROBE") != "1" {
		t.Skip("set BOKU_REGION_CAPTURE_PROBE=1 to test GDI against the live desktop")
	}
	type point struct{ X, Y int32 }
	var cursor point
	getCursorPos := user32.NewProc("GetCursorPos")
	if ok, _, err := getCursorPos.Call(uintptr(unsafe.Pointer(&cursor))); ok == 0 {
		t.Fatalf("GetCursorPos: %v", err)
	}
	got, err := (&Device{}).Capture(context.Background(), image.Rect(int(cursor.X), int(cursor.Y), int(cursor.X)+16, int(cursor.Y)+16))
	if err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	decoded, _, err := image.Decode(bytes.NewReader(got))
	if err != nil {
		t.Fatalf("decode captured PNG: %v", err)
	}
	if bounds := decoded.Bounds(); bounds != image.Rect(0, 0, 16, 16) {
		t.Fatalf("captured bounds = %v, want 16x16", bounds)
	}
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			_, _, _, alpha := decoded.At(x, y).RGBA()
			if alpha != 0xffff {
				t.Fatalf("captured pixel (%d,%d) alpha = %#x, want opaque", x, y, alpha)
			}
		}
	}
}

// TestSelectDesktopByHand 是原生 Snipaste 式选择器的按需端到端探针，需操作者框选
// 一块可见且颜色不均匀的桌面区域。选择器必须先消失，GDI 才能返回可解码、非单色的
// 图像；这能捕捉「截到浮层而非桌面」这一历史回归。
func TestSelectDesktopByHand(t *testing.T) {
	if os.Getenv("BOKU_REGION_SELECTOR_PROBE") != "1" {
		t.Skip("set BOKU_REGION_SELECTOR_PROBE=1 to test the native selector against the live desktop")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pngBytes, err := (&Device{}).Select(ctx)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	decoded, _, err := image.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("decode selected PNG: %v", err)
	}
	bounds := decoded.Bounds()
	if bounds.Dx() < 4 || bounds.Dy() < 4 {
		t.Fatalf("selected bounds = %v, want at least 4x4", bounds)
	}
	first := decoded.At(0, 0)
	var distinct bool
	for y := bounds.Min.Y; y < bounds.Max.Y && !distinct; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if decoded.At(x, y) != first {
				distinct = true
				break
			}
		}
	}
	if !distinct {
		t.Fatal("selected PNG is a single colour; expected desktop pixels after the selector closed")
	}
}
