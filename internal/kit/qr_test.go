package kit

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fixtures 是用二维码编码器一次性生成的像素图并保留下来：测试时重新生成会让包依赖
// 编码器，而本测试关心的是解码器从图片里读出了什么，不是另一个库能写出什么。
const fixtureText = "https://example.com/hello?name=测试&x=1"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestDecodeQRCodePNGReadsTheCode(t *testing.T) {
	text, err := DecodeQRCodePNG(fixture(t, "code.png"))
	if err != nil {
		t.Fatalf("DecodeQRCodePNG: %v", err)
	}
	if text != fixtureText {
		t.Fatalf("text = %q, want %q", text, fixtureText)
	}
}

func TestDecodeQRCodePNGReadsAnInvertedCode(t *testing.T) {
	text, err := DecodeQRCodePNG(fixture(t, "inverted.png"))
	if err != nil {
		t.Fatalf("DecodeQRCodePNG on a light-on-dark code: %v", err)
	}
	if text != fixtureText {
		t.Fatalf("text = %q, want %q", text, fixtureText)
	}
}

func TestDecodeQRCodePNGSaysWhenThereIsNoCode(t *testing.T) {
	_, err := DecodeQRCodePNG(fixture(t, "no-code.png"))
	if !errors.Is(err, ErrQRCodeNotFound) {
		t.Fatalf("err = %v, want ErrQRCodeNotFound", err)
	}
}

func TestDecodeQRCodePNGRejectsBytesThatAreNotAnImage(t *testing.T) {
	_, err := DecodeQRCodePNG(fixture(t, "not-an-image.bin"))
	if !errors.Is(err, ErrInvalidQRImage) {
		t.Fatalf("err = %v, want ErrInvalidQRImage", err)
	}
}

func TestDecodeQRCodePNGRejectsAnEmptyImage(t *testing.T) {
	_, err := DecodeQRCodePNG(nil)
	if !errors.Is(err, ErrInvalidQRImage) {
		t.Fatalf("err = %v, want ErrInvalidQRImage", err)
	}
}
