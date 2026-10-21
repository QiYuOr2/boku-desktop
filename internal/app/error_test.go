package app

import (
	"fmt"
	"strings"
	"testing"

	"boku/internal/domain"
)

func TestFriendlyErrorHidesUnstructuredBadResponses(t *testing.T) {
	err := fmt.Errorf("%w: the translation is from %q", domain.ErrProviderBadResponse, "th")
	got := friendlyError(err)
	if got != "服务返回了无法识别的数据。" {
		t.Errorf("friendlyError() = %q", got)
	}
	if strings.Contains(got, "th") || strings.Contains(got, "provider") {
		t.Errorf("friendly error leaks implementation details: %q", got)
	}
}

func TestFriendlyErrorExplainsStructuredImageFailures(t *testing.T) {
	got := friendlyError(&domain.ImageTranslationError{Code: domain.ImageFailureNoReadableText})
	if got != "图片中没有可识别的文字。" {
		t.Errorf("friendlyError() = %q", got)
	}
}
