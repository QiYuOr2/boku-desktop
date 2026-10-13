// Package assets carries the images the application needs at startup.
//
// They are embedded at compile time with go:embed: a packaged app keeps its
// resources in a platform-specific place, so reading them from disk at
// runtime would work in development and fail after packaging.
//
// The package has no dependencies of its own; it decodes and scales with the
// standard library only.
package assets

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/png"
)

// trayIconPNG is the tray icon, a copy of resources/icon.png.
//
// It is a copy because go:embed cannot reach a parent directory: a package
// embeds only files below its own directory. resources/icon.png stays the
// application icon that `mygo build` packages; change one, change the other.
//
//go:embed tray.png
var trayIconPNG []byte

// deepseekSVG is the DeepSeek whale mark, the icon of the provider of the
// same name. It is an SVG, which the application shows with ui.Icon: a
// vector stays sharp at whatever size the screen needs, and unlike a
// bitmap it costs the binary a few kilobytes rather than a few hundred.
//
//go:embed deepseek.svg
var deepseekSVG []byte

// DeepSeek returns the DeepSeek mark as the SVG document it is. The caller
// parses it once and keeps the result: parsing belongs to the framework,
// and this package depends on the standard library alone.
func DeepSeek() []byte { return deepseekSVG }

// Icon returns the application icon as a PNG, scaled to size×size pixels.
//
// It is the same picture in two places: MyGo wants roughly 16×16 points for
// the tray, 32×32 pixels on a Retina display, and a view shows it larger in
// a header. The embedded file is larger than both so that it stays sharp
// wherever it is shown.
func Icon(size int) ([]byte, error) {
	if size <= 0 {
		return nil, fmt.Errorf("assets: icon size %d is not positive", size)
	}
	src, err := png.Decode(bytes.NewReader(trayIconPNG))
	if err != nil {
		return nil, fmt.Errorf("assets: decode icon: %w", err)
	}
	dst := scale(src, size)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, fmt.Errorf("assets: encode icon: %w", err)
	}
	return buf.Bytes(), nil
}

// scale returns src resized to size×size pixels with box filtering: each
// destination pixel averages the source pixels it covers, which keeps an
// icon crisp when it is shrunk a lot.
func scale(src image.Image, size int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	b := src.Bounds()
	for y := 0; y < size; y++ {
		y0 := b.Min.Y + y*b.Dy()/size
		y1 := b.Min.Y + (y+1)*b.Dy()/size
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < size; x++ {
			x0 := b.Min.X + x*b.Dx()/size
			x1 := b.Min.X + (x+1)*b.Dx()/size
			if x1 <= x0 {
				x1 = x0 + 1
			}
			dst.Set(x, y, average(src, x0, y0, x1, y1))
		}
	}
	return dst
}

// average returns the mean of the source pixels in [x0,x1)×[y0,y1),
// weighted by alpha so that transparent pixels do not darken the edges.
func average(src image.Image, x0, y0, x1, y1 int) color.NRGBA {
	var r, g, b, a, n uint64
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			pr, pg, pb, pa := src.At(x, y).RGBA()
			w := uint64(pa >> 8)
			r += uint64(pr>>8) * w
			g += uint64(pg>>8) * w
			b += uint64(pb>>8) * w
			a += w
			n++
		}
	}
	if n == 0 || a == 0 {
		return color.NRGBA{}
	}
	return color.NRGBA{
		R: uint8(clamp(r/a, 255)),
		G: uint8(clamp(g/a, 255)),
		B: uint8(clamp(b/a, 255)),
		A: uint8(clamp(a/n, 255)),
	}
}

func clamp(v, max uint64) uint64 {
	if v > max {
		return max
	}
	return v
}
