//go:build darwin

package selection

import (
	"context"
	"os"
	"testing"
)

// TestByHand 读取前台应用的选中文本。需要桌面会话、已选中的文本和辅助功能权限，
// 因此不属于常规测试；用 BOKU_SELECTION_PROBE=1 手动运行。
func TestByHand(t *testing.T) {
	if os.Getenv("BOKU_SELECTION_PROBE") == "" {
		t.Skip("set BOKU_SELECTION_PROBE=1 and select some text in another application")
	}
	// 只用辅助功能：复制回退需要运行中的应用和它的剪贴板，测试进程没有。
	// 真实应用通过快捷键走那条路径。
	text, err := New(Options{}).SelectedText(context.Background())
	if err != nil {
		t.Fatalf("read the selection: %v", err)
	}
	if text == "" {
		t.Fatal("the platform answered nothing")
	}
	t.Logf("selected text: %q", text)
}

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

// 无权限或无选中时必须返回空而不是崩溃：属性可能不是字符串类型的 CFTypeRef。
func TestReadingTheSelectionAnswersNothingRatherThanPanicking(t *testing.T) {
	if !loadAX() {
		t.Skip("the Accessibility framework is not available")
	}
	_ = readSelectionAX()
}
