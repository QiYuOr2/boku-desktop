package app

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	"boku/internal/domain/deepseek"
)

// TestRenderScreensForInspection writes the two screens as PNGs, so a
// layout change can be looked at. It is skipped unless BOKU_RENDER_DIR is
// set: nothing is written during an ordinary test run.
func TestRenderScreensForInspection(t *testing.T) {
	dir := os.Getenv("BOKU_RENDER_DIR")
	if dir == "" {
		t.Skip("set BOKU_RENDER_DIR to write the screens")
	}

	write := func(name string, view func(*ui.Context), width, height int) {
		t.Helper()
		img := ui.Render(view, width, height, 2)
		file, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		defer file.Close()
		if err := png.Encode(file, img); err != nil {
			t.Fatalf("encode %s: %v", name, err)
		}
	}

	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	loaded(t, state)
	write("flyout.png", state.View, panelWidth, panelHeight)
	state.detailsOpen = true
	write("flyout-details.png", state.View, panelWidth, panelHeight)
	state.detailsOpen = false
	state.loadCredential()
	write("settings.png", state.settingsView, settingsWidth, settingsHeight)

	// The quick window without a credential.
	missing := testState(t, newTestProvider(), newTestStore(), "0.1.0")
	_, _ = missing.panelService.Refresh(context.Background(), deepseek.PanelID)
	write("flyout-missing.png", missing.View, panelWidth, panelHeight)

	// The quick window after a failed refresh: the old numbers stay, and
	// are marked as old.
	failing := newTestProvider()
	stale := testState(t, failing, withCredential(newTestStore()), "0.1.0")
	loaded(t, stale)
	failing.fail(domain.ErrProviderTimeout)
	_, _ = stale.panelService.Refresh(context.Background(), deepseek.PanelID)
	write("flyout-stale.png", stale.View, panelWidth, panelHeight)

	// The settings window with a credential already stored.
	write("settings-stored.png", state.settingsView, settingsWidth, settingsHeight)
}

// dumpRectsEnv makes the manual test print where every text of both windows
// sits. A layout assertion in the tests is written against these numbers.
const dumpRectsEnv = "BOKU_DUMP_RECTS"

// TestDumpRectsForInspection prints the rectangles of both windows. It is
// skipped unless BOKU_DUMP_RECTS is set, and prints nothing during an
// ordinary test run.
func TestDumpRectsForInspection(t *testing.T) {
	if os.Getenv(dumpRectsEnv) == "" {
		t.Skip("set BOKU_DUMP_RECTS to print the rectangles")
	}

	dump := func(name string, view func(*ui.Context), width, height int) {
		tt := ui.NewTester(view, width, height)
		fmt.Printf("---- %s ----\n", name)
		for _, text := range tt.Texts() {
			if rect, ok := tt.Find(text); ok {
				fmt.Printf("%-40q x=%6.1f y=%6.1f w=%6.1f h=%5.1f\n", text, rect.X, rect.Y, rect.W, rect.H)
			}
		}
		img := ui.Render(view, width, height, 2)
		fmt.Printf("divider rows: %v\n", dividerRows(img, ui.LightTheme().Border, width*2))
	}

	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	loaded(t, state)
	state.loadCredential()
	dump("flyout", state.View, panelWidth, panelHeight)
	dump("settings", state.settingsView, settingsWidth, settingsHeight)
}
