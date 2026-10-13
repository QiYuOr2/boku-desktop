package assets

import (
	"bytes"
	"image/png"
	"testing"
)

func TestIconHasRequestedSize(t *testing.T) {
	for _, size := range []int{16, 32, 48} {
		data, err := Icon(size)
		if err != nil {
			t.Fatalf("Icon(%d): %v", size, err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("Icon(%d) is not a PNG: %v", size, err)
		}
		if got := img.Bounds().Dx(); got != size {
			t.Errorf("Icon(%d) is %d pixels wide", size, got)
		}
		if got := img.Bounds().Dy(); got != size {
			t.Errorf("Icon(%d) is %d pixels tall", size, got)
		}
	}
}

func TestIconIsNotBlank(t *testing.T) {
	data, err := Icon(32)
	if err != nil {
		t.Fatalf("Icon(32): %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	opaque := 0
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a > 0 {
				opaque++
			}
		}
	}
	if opaque == 0 {
		t.Error("the icon is fully transparent")
	}
}

func TestIconRejectsBadSize(t *testing.T) {
	for _, size := range []int{0, -1} {
		if _, err := Icon(size); err == nil {
			t.Errorf("Icon(%d) returned no error", size)
		}
	}
}
