//go:build darwin

package regioncapture

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"unsafe"

	"github.com/ebitengine/purego"
)

// CoreGraphics is used directly instead of the screencapture command.  That
// keeps the captured pixels transient and leaves region selection under Boku's
// control. Screen Recording permission is still enforced by macOS.
var (
	libCG                   = openFramework("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics")
	libCF                   = openFramework("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")
	cgWindowListCreateImage func(cgRect, uint32, uint32, uint32) uintptr
	cgImageGetWidth         func(uintptr) uintptr
	cgImageGetHeight        func(uintptr) uintptr
	cgImageGetBytesPerRow   func(uintptr) uintptr
	cgImageGetDataProvider  func(uintptr) uintptr
	cgDataProviderCopyData  func(uintptr) uintptr
	cfDataGetLength         func(uintptr) uintptr
	cfDataGetBytePtr        func(uintptr) unsafe.Pointer
	cfRelease               func(uintptr)
)

type cgRect struct{ X, Y, Width, Height float64 }

func openFramework(path string) uintptr {
	lib, err := purego.Dlopen(path, purego.RTLD_LAZY|purego.RTLD_LOCAL)
	if err != nil {
		panic(fmt.Sprintf("region capture: load %s: %v", path, err))
	}
	return lib
}

func init() {
	purego.RegisterLibFunc(&cgWindowListCreateImage, libCG, "CGWindowListCreateImage")
	purego.RegisterLibFunc(&cgImageGetWidth, libCG, "CGImageGetWidth")
	purego.RegisterLibFunc(&cgImageGetHeight, libCG, "CGImageGetHeight")
	purego.RegisterLibFunc(&cgImageGetBytesPerRow, libCG, "CGImageGetBytesPerRow")
	purego.RegisterLibFunc(&cgImageGetDataProvider, libCG, "CGImageGetDataProvider")
	purego.RegisterLibFunc(&cgDataProviderCopyData, libCG, "CGDataProviderCopyData")
	purego.RegisterLibFunc(&cfDataGetLength, libCF, "CFDataGetLength")
	purego.RegisterLibFunc(&cfDataGetBytePtr, libCF, "CFDataGetBytePtr")
	purego.RegisterLibFunc(&cfRelease, libCF, "CFRelease")
}

func (*Device) Capture(ctx context.Context, rect image.Rectangle) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if rect.Dx() <= 0 || rect.Dy() <= 0 {
		return nil, fmt.Errorf("region capture: empty rectangle")
	}
	// kCGWindowListOptionOnScreenOnly, kCGNullWindowID,
	// kCGWindowImageDefault. CoreGraphics returns nil when Screen Recording
	// was denied or the requested display region cannot be read.
	cgImage := cgWindowListCreateImage(cgRect{X: float64(rect.Min.X), Y: float64(rect.Min.Y), Width: float64(rect.Dx()), Height: float64(rect.Dy())}, 1, 0, 0)
	if cgImage == 0 {
		return nil, fmt.Errorf("region capture: macOS denied Screen Recording permission or could not read the display")
	}
	defer cfRelease(cgImage)
	width, height := int(cgImageGetWidth(cgImage)), int(cgImageGetHeight(cgImage))
	stride := int(cgImageGetBytesPerRow(cgImage))
	if width <= 0 || height <= 0 || stride < width*4 {
		return nil, fmt.Errorf("region capture: invalid CoreGraphics image")
	}
	data := cgDataProviderCopyData(cgImageGetDataProvider(cgImage))
	if data == 0 {
		return nil, fmt.Errorf("region capture: copy CoreGraphics pixels")
	}
	defer cfRelease(data)
	length := int(cfDataGetLength(data))
	if length < stride*height {
		return nil, fmt.Errorf("region capture: incomplete CoreGraphics pixels")
	}
	raw := unsafe.Slice((*byte)(cfDataGetBytePtr(data)), length)
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			src, dst := y*stride+x*4, y*img.Stride+x*4
			// Quartz's normal desktop image is little-endian premultiplied BGRA.
			img.Pix[dst], img.Pix[dst+1], img.Pix[dst+2], img.Pix[dst+3] = raw[src+2], raw[src+1], raw[src], raw[src+3]
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, fmt.Errorf("region capture: encode png: %w", err)
	}
	return out.Bytes(), nil
}
