//go:build windows

package regioncapture

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"syscall"
	"unsafe"
)

var (
	user32       = syscall.NewLazyDLL("user32.dll")
	gdi32        = syscall.NewLazyDLL("gdi32.dll")
	getDC        = user32.NewProc("GetDC")
	relDC        = user32.NewProc("ReleaseDC")
	compatDC     = gdi32.NewProc("CreateCompatibleDC")
	delDC        = gdi32.NewProc("DeleteDC")
	dib          = gdi32.NewProc("CreateDIBSection")
	selectObject = gdi32.NewProc("SelectObject")
	delObject    = gdi32.NewProc("DeleteObject")
	bitBlt       = gdi32.NewProc("BitBlt")
)

const (
	biRGB        = 0
	dibRGBColors = 0
	srccopy      = 0x00CC0020
	capblt       = 0x40000000
)

type bitmapInfoHeader struct {
	Size                         uint32
	Width, Height                int32
	Planes, BitCount             uint16
	Compression, SizeImage       uint32
	XPelsPerMeter, YPelsPerMeter int32
	ClrUsed, ClrImportant        uint32
}
type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

func (*Device) Capture(ctx context.Context, rect image.Rectangle) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if rect.Dx() <= 0 || rect.Dy() <= 0 {
		return nil, fmt.Errorf("region capture: empty rectangle")
	}
	if rect.Dx() > 16384 || rect.Dy() > 16384 {
		return nil, fmt.Errorf("region capture: rectangle too large")
	}
	screen, _, err := getDC.Call(0)
	if screen == 0 {
		return nil, fmt.Errorf("region capture: get desktop DC: %w", err)
	}
	defer relDC.Call(0, screen)
	mem, _, err := compatDC.Call(screen)
	if mem == 0 {
		return nil, fmt.Errorf("region capture: create memory DC: %w", err)
	}
	defer delDC.Call(mem)
	info := bitmapInfo{Header: bitmapInfoHeader{Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: int32(rect.Dx()), Height: -int32(rect.Dy()), Planes: 1, BitCount: 32, Compression: biRGB}}
	var bits unsafe.Pointer
	bmp, _, err := dib.Call(mem, uintptr(unsafe.Pointer(&info)), dibRGBColors, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		return nil, fmt.Errorf("region capture: create bitmap: %w", err)
	}
	defer delObject.Call(bmp)
	old, _, _ := selectObject.Call(mem, bmp)
	defer selectObject.Call(mem, old)
	if copied, _, err := bitBlt.Call(mem, 0, 0, uintptr(rect.Dx()), uintptr(rect.Dy()), screen, uintptr(rect.Min.X), uintptr(rect.Min.Y), srccopy|capblt); copied == 0 {
		return nil, fmt.Errorf("region capture: read desktop pixels: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	n := rect.Dx() * rect.Dy() * 4
	raw := unsafe.Slice((*byte)(bits), n)
	return encodeBGRA(rect.Dx(), rect.Dy(), raw)
}

// encodeBGRA is deliberately separate from GDI so the native byte order and
// PNG conversion can be verified without reading a user's desktop.
func encodeBGRA(width, height int, raw []byte) ([]byte, error) {
	if width <= 0 || height <= 0 || len(raw) < width*height*4 {
		return nil, fmt.Errorf("region capture: invalid bitmap pixels")
	}
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < width*height*4; i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = raw[i+2], raw[i+1], raw[i], raw[i+3]
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, fmt.Errorf("region capture: encode png: %w", err)
	}
	return out.Bytes(), nil
}
