//go:build darwin

package selection

import (
	"context"
	"os"
	"testing"
)

// TestByHand reads the selection of the application the user has in front.
// It needs a desktop session, a selected text and the Accessibility
// permission, so it is not part of the ordinary suite:
//
//	BOKU_SELECTION_PROBE=1 go test ./internal/platform/selection -run ByHand -v
//
// It is the macOS counterpart of the Windows probe: the platform half cannot
// run in a test process that has no session, and a manual check is what says
// the vtable or the accessibility attribute is the one this build thinks it
// is.
func TestByHand(t *testing.T) {
	if os.Getenv("BOKU_SELECTION_PROBE") == "" {
		t.Skip("set BOKU_SELECTION_PROBE=1 and select some text in another application")
	}
	// Accessibility only: the copy fallback needs a running application
	// and its clipboard, which a test process does not have. The real app
	// exercises that path through the shortcut.
	text, err := New(Options{}).SelectedText(context.Background())
	if err != nil {
		t.Fatalf("read the selection: %v", err)
	}
	if text == "" {
		t.Fatal("the platform answered nothing")
	}
	t.Logf("selected text: %q", text)
}

// The accessibility and Core Graphics frameworks are loaded through purego;
// a missing symbol or a wrong signature would fail here rather than in front
// of the user.
func TestAccessibilityFrameworksLoad(t *testing.T) {
	if !loadAX() {
		t.Fatal("the Accessibility framework did not load")
	}
	if !loadCoreGraphics() {
		t.Fatal("Core Graphics did not load")
	}
	if axIsProcessTrustedWithOptionsFunc == nil {
		t.Fatal("AXIsProcessTrustedWithOptions did not load")
	}
	if axTrustedCheckOptionPrompt == 0 || cfBooleanTrue == 0 ||
		cfDictionaryCreate == nil || cfDictionaryKeyCallBacks == 0 || cfDictionaryValueCallBacks == 0 {
		t.Fatal("the Accessibility prompt dictionary is incomplete")
	}
}

func TestCoreFoundationStringRoundTrip(t *testing.T) {
	if !loadAX() {
		t.Skip("the Accessibility framework is not available")
	}
	value := newCFString("hello 世界")
	if value == 0 {
		t.Fatal("CFStringCreateWithCString answered nothing")
	}
	defer cfRelease(value)
	if got := cfString(value); got != "hello 世界" {
		t.Errorf("cfString = %q, want the string that was made", got)
	}
}

// Reading with no permission or no selection must answer nothing rather than
// crash on a CFTypeRef that is not a string.
func TestReadingTheSelectionAnswersNothingRatherThanPanicking(t *testing.T) {
	if !loadAX() {
		t.Skip("the Accessibility framework is not available")
	}
	_ = readSelectionAX()
}
