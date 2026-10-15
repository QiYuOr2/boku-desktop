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

// clipboardSVG is the clipboard mark, drawn beside the clipboard line in the
// quick window. It is an SVG for the same reason the DeepSeek mark is: it is
// shown at a text size and stays sharp there.
//
//go:embed clipboard.svg
var clipboardSVG []byte

// refreshSVG is the mark of the quick window's refresh action: fetching a
// panel again. It is an SVG because the caller turns it while a fetch runs,
// and a vector stays sharp at every angle.
//
//go:embed refresh.svg
var refreshSVG []byte

// The quick window footer's two actions: the way into the settings window and
// the way out of the application. They are marks rather than words because the
// footer is one line at the bottom of a card, and two words there read as
// content while two marks read as what they are.
var (
	//go:embed settings.svg
	settingsSVG []byte
	//go:embed quit.svg
	quitSVG []byte
)

// The clipboard history's kind marks: what a recorded copy is drawn as, in
// the tile at the head of its row. They are the framework's own drawing
// language (a 24-unit box, stroked in currentColor), so they take the color
// of the kind they stand for.
var (
	//go:embed history-text.svg
	historyTextSVG []byte
	//go:embed history-image.svg
	historyImageSVG []byte
	//go:embed history-link.svg
	historyLinkSVG []byte
	//go:embed history-code.svg
	historyCodeSVG []byte
)

// The translation window's own marks: what it copies, what keeps it open,
// and how it closes. They are drawn as marks rather than words because the
// window is a card the size of a sentence, and a row of words at its foot
// would outweigh what it is showing.
var (
	//go:embed copy.svg
	copySVG []byte
	//go:embed pin.svg
	pinSVG []byte
	//go:embed close.svg
	closeSVG []byte
	//go:embed translate.svg
	translateSVG []byte
)

// The settings window's sidebar marks: what each section of the page is
// reached by. They are the same 24-unit stroked drawings the rest of the
// application uses, so they take the color of the line they sit on. Every
// other entry draws the mark its own section already carries
// (§2.1 of the design system).
var (
	//go:embed key.svg
	keySVG []byte
	//go:embed info.svg
	infoSVG []byte
)

// gaugeSVG is the placeholder mark of a block whose provider has no drawing of
// its own in the repository yet: a gauge, which says "a measure" without
// pretending to be a brand.
//
//go:embed gauge.svg
var gaugeSVG []byte

// DeepSeek returns the DeepSeek mark as the SVG document it is. The caller
// parses it once and keeps the result: parsing belongs to the framework,
// and this package depends on the standard library alone.
func DeepSeek() []byte { return deepseekSVG }

// Copy returns the mark for copying a value.
func Copy() []byte { return copySVG }

// Pin returns the mark for keeping a window open.
func Pin() []byte { return pinSVG }

// Close returns the mark for dismissing a window.
func Close() []byte { return closeSVG }

// Translate returns the mark that heads the translation section: the
// application's own drawing rather than a brand's.
func Translate() []byte { return translateSVG }

// Clipboard returns the clipboard mark as the SVG document it is.
func Clipboard() []byte { return clipboardSVG }

// Key returns the mark that heads the key section and its sidebar entry:
// the drawing of a key the design gives it.
func Key() []byte { return keySVG }

// Info returns the mark of the application itself, drawn beside 关于.
func Info() []byte { return infoSVG }

// Gauge returns the placeholder mark of a block whose provider has no drawing
// of its own: a measure rather than a brand.
func Gauge() []byte { return gaugeSVG }

// Refresh returns the refresh mark as the SVG document it is.
func Refresh() []byte { return refreshSVG }

// Settings returns the settings mark as the SVG document it is.
func Settings() []byte { return settingsSVG }

// Quit returns the quit mark as the SVG document it is.
func Quit() []byte { return quitSVG }

// HistoryText returns the mark for a copy that is text.
func HistoryText() []byte { return historyTextSVG }

// HistoryImage returns the mark for a copy that is an image.
func HistoryImage() []byte { return historyImageSVG }

// HistoryLink returns the mark for a copy that is a link.
func HistoryLink() []byte { return historyLinkSVG }

// HistoryCode returns the mark for a copy that reads as code.
func HistoryCode() []byte { return historyCodeSVG }

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
