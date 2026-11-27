// 本文件从图片中读出二维码承载的文本，是对字节的纯函数，与其它纯函数放在一起：
// 不涉及网络、文件与屏幕。解码在本地进行，因为二维码内容往往正是用户不希望外发的东西。
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
	// ErrInvalidQRImage 表示字节不是本包可读取的图片。它与「无二维码」不同：
	// 一个是损坏的输入，另一个是能读但没有码的图片。
	ErrInvalidQRImage = errors.New("qr: the image cannot be read")
	// ErrQRCodeNotFound 表示图片可读但不含二维码。
	ErrQRCodeNotFound = errors.New("qr: the image holds no QR code")
)

// DecodeQRCodePNG 返回 PNG 中第一个二维码的文本。
//
// 截图以 PNG 到达，采集设备也保证如此；只接受 PNG 可让输入与应用产生的一致。
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

// DecodeQRCode 返回图片中第一个二维码的文本。
//
// 二维码通常深色在浅色背景上，但深色界面里绘制的码是浅色在深色背景上，人眼同样可读，因此两种都试。
// 深度尝试放在后面：屏幕上大多数码在快速扫描时即可找到，没有码的截图不应先付出该开销。
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

// read 执行一次尝试。任何失败都视为「此次未找到」：读取器自身的错误类型说明了失败原因
// （无定位图案、版本不可读、校验和错误），对用户而言它们都等价于「图片里没有码」。
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
