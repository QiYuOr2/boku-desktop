package ui

import (
	"testing"

	"boku/internal/domain"
)

func TestLabelUsesImageSizeAndKind(t *testing.T) {
	if got := Label(domain.ClipboardEntry{Kind: domain.ClipKindImage, ImageBytes: 2048}); got != "图片 2 KB" {
		t.Fatalf("image label = %q", got)
	}
	if got := Label(domain.ClipboardEntry{Kind: domain.ClipKindJSON}); got != "JSON" {
		t.Fatalf("JSON label = %q", got)
	}
}
