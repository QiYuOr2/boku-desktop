package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The quick window has no header: what it is about is said by the panel's
// own title, and a window this small does not spend its top on repeating
// the application's name. What it keeps is the panel, and the footer with
// the version and the actions.
func TestQuickWindowHasNoHeaderAndKeepsItsFooter(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	loaded(t, state)
	tt := ui.NewTester(state.View, panelWidth, panelHeight)

	for _, want := range []string{"DeepSeek 余额", "刷新", "退出", "设置", "Boku 0.1.0"} {
		if !tt.HasText(want) {
			t.Errorf("the quick window does not show %q; it shows %q", want, tt.Texts())
		}
	}
	// "Boku" itself is in the footer, as the first half of the version
	// line; what is gone is the header's subtitle.
	if tt.HasText("看见状态，用完就走。") {
		t.Errorf("the quick window still shows the header's subtitle: %q", tt.Texts())
	}
	// The panel comes first: the window is its block, not a header's worth
	// of padding followed by one.
	title, ok := tt.Find("DeepSeek 余额")
	if !ok {
		t.Fatalf("the panel title was not found; the window shows %q", tt.Texts())
	}
	if title.Y > 40 {
		t.Errorf("the panel starts at y=%.0f: the header was not taken away", title.Y)
	}
}

func TestFooterSitsBelowTheContentAndIsSmall(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	loaded(t, state)
	tt := ui.NewTester(state.View, panelWidth, panelHeight)

	content, ok := tt.Find("DeepSeek 余额")
	if !ok {
		t.Fatalf("the content was not found; the window shows %q", tt.Texts())
	}
	quit, ok := tt.Find("退出")
	if !ok {
		t.Fatalf("the quit action was not found; the window shows %q", tt.Texts())
	}
	version, ok := tt.Find("Boku 0.1.0")
	if !ok {
		t.Fatalf("the version was not found; the window shows %q", tt.Texts())
	}
	if quit.Y <= content.Y {
		t.Errorf("the quit action at y=%.0f is not below the content at y=%.0f", quit.Y, content.Y)
	}
	if quit.H > 26 {
		t.Errorf("the quit action is %.0f points tall: the footer should keep it small", quit.H)
	}
	if quit.W > 60 {
		t.Errorf("the quit action is %.0f points wide", quit.W)
	}
	if diff := version.Y - quit.Y; diff > 8 || diff < -8 {
		t.Errorf("the version at y=%.1f and the actions at y=%.1f are not on one footer row", version.Y, quit.Y)
	}
	if version.X >= quit.X {
		t.Errorf("the version at x=%.0f is not left of the actions at x=%.0f", version.X, quit.X)
	}
}

func TestViewInNarrowWindow(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	loaded(t, state)
	tt := ui.NewTester(state.View, 160, panelHeight)
	if !tt.HasText("退出") {
		t.Errorf("the quick window does not show the footer when narrow; it shows %q", tt.Texts())
	}
}

// A window marks its sections with dividers, not with boxes: the panel and
// the footer each end at one.
func TestWindowSectionsAreMarkedByDividers(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	loaded(t, state)
	const scale = 2
	img := ui.Render(state.View, panelWidth, panelHeight, scale)

	tt := ui.NewTester(state.View, panelWidth, panelHeight)
	collapsed := findText(t, tt, "明细").Y
	version := findText(t, tt, "Boku 0.1.0").Y

	rows := dividerRows(img, ui.LightTheme().Border, panelWidth*scale)
	if len(rows) == 0 {
		t.Fatal("the window draws no divider at all: the panel is not separated from the footer")
	}
	between := false
	for _, y := range rows {
		if at := float32(y) / scale; at > collapsed && at < version {
			between = true
		}
	}
	if !between {
		t.Errorf("no divider falls between the panel at y=%.0f and the footer at y=%.0f: %v",
			collapsed, version, rows)
	}
}
