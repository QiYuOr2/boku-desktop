// This file reads the text a QR code holds out of an image. It is a pure
// function over bytes, kept with the rest of the application's pure
// functions: no network, no files, no screen. The caller — the application's
// screenshot entry — owns where the image came from and what happens to it
// afterwards. Decoding happens locally because the contents of a QR code are
// often exactly what the user did not want to send anywhere.
package kit

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"

	"github.com/makiuchi-d/gozxing"
	zxingqr "github.com/makiuchi-d/gozxing/qrcode"
)

var (
	// ErrInvalidQRImage means the bytes were not an image this package can
	// read. It is not "no QR code": one is a broken input, the other is a
	// readable picture without a code in it.
	ErrInvalidQRImage = errors.New("qr: the image cannot be read")
	// ErrQRCodeNotFound means the image was readable but held no QR code.
	ErrQRCodeNotFound = errors.New("qr: the image holds no QR code")
)

// DecodeQRCodePNG returns the text of the first QR code in the PNG.
//
// Screenshots arrive as PNG, and the capture device promises exactly that;
// accepting only PNG keeps the input the same shape as the one the
// application produces.
func DecodeQRCodePNG(data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("%w: empty image", ErrInvalidQRImage)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidQRImage, err)
	}
	return DecodeQRCode(img)
}

// DecodeQRCode returns the text of the first QR code in the image.
//
// A QR code is normally dark on light, but a code drawn in a dark interface
// is light on dark and is just as readable to a person. Both are tried. The
// thorough pass is second because most codes on a screen are found on the
// quick one, and a screenshot with no code should not pay for it first.
func DecodeQRCode(img image.Image) (string, error) {
	if img == nil || img.Bounds().Empty() {
		return "", ErrInvalidQRImage
	}
	for _, inverted := range []bool{false, true} {
		for _, thorough := range []bool{false, true} {
			if text, ok := read(img, inverted, thorough); ok {
				return text, nil
			}
		}
	}
	return "", ErrQRCodeNotFound
}

// read makes one attempt. Any failure is "this attempt did not find one":
// the reader's own error types say why an attempt failed (no finder
// pattern, an unreadable version, a bad checksum), and none of them is a
// different answer for the user than "no code in this picture".
func read(img image.Image, inverted, thorough bool) (string, bool) {
	source := gozxing.NewLuminanceSourceFromImage(img)
	if inverted {
		source = gozxing.NewInvertedLuminanceSource(source)
	}
	bitmap, err := gozxing.NewBinaryBitmap(gozxing.NewHybridBinarizer(source))
	if err != nil {
		return "", false
	}
	var hints map[gozxing.DecodeHintType]interface{}
	if thorough {
		hints = map[gozxing.DecodeHintType]interface{}{
			gozxing.DecodeHintType_TRY_HARDER: true,
		}
	}
	result, err := zxingqr.NewQRCodeReader().Decode(bitmap, hints)
	if err != nil {
		return "", false
	}
	return result.GetText(), true
}
