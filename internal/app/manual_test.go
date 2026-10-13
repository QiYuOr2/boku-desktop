package app

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	"boku/internal/domain/deepseek"
)

// This file is the package's manual harness: what is here needs a human to
// look at the frames it writes, so every check is skipped unless its
// environment variable is set, and nothing is written during `go test ./...`.
// It is named for that role rather than for a production file, because the
// frames come from app.go, card.go, settings.go and clipboard.go at once.

// TestRenderScreensForInspection writes the two screens as PNGs, so a
// layout change can be looked at. It is skipped unless BOKU_RENDER_DIR is
// set: nothing is written during an ordinary test run.
func TestRenderScreensForInspection(t *testing.T) {
	dir := os.Getenv("BOKU_RENDER_DIR")
	if dir == "" {
		t.Skip("set BOKU_RENDER_DIR to write the screens")
	}

	writeImage := func(name string, img *image.RGBA) {
		t.Helper()
		file, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		defer file.Close()
		if err := png.Encode(file, img); err != nil {
			t.Fatalf("encode %s: %v", name, err)
		}
	}
	write := func(name string, view func(*ui.Context), width, height int) {
		t.Helper()
		writeImage(name, ui.Render(view, width, height, 2))
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

	// The quick window while a fetch runs: the refresh mark turns, and one
	// frame shows it at the angle it was drawn at. The frame is taken
	// through a tester after a moment, so it is not the angle at t = 0 that
	// every ui.Render frame shows.
	held := newTestProvider()
	fetching := testState(t, held, withCredential(newTestStore()), "0.1.0")
	loaded(t, fetching)
	gate := held.hold()
	go func() { _, _ = fetching.panelService.Refresh(context.Background(), deepseek.PanelID) }()
	for deadline := time.Now().Add(2 * time.Second); held.count() < 2 && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	turning := ui.NewTester(fetching.View, panelWidth, panelHeight)
	time.Sleep(spinPeriod / 8)
	turning.Frame()
	writeImage("flyout-refreshing.png", turning.Image())
	close(gate)

	// The settings window with a credential already stored.
	write("settings-stored.png", state.settingsView, settingsWidth, settingsHeight)

	// The quick window with its clipboard line, and the history window: the
	// two faces the clipboard tool adds.
	device := &testDevice{}
	device.set(domain.ClipContent{
		Kind: domain.ClipKindText,
		Text: "https://example.com/some/rather/long/path?with=query&params=1",
	})
	withClipboard := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	withClipboard.clipboard = testClipboard(t, device, &testToolStore{
		settings: domain.ToolSettings{ClipboardRegionVisible: true, ClipboardMonitoring: true},
	})
	loaded(t, withClipboard)
	withClipboard.clipboard.Read(context.Background())
	write("flyout-clipboard.png", withClipboard.View, panelWidth, panelHeight)

	for _, text := range []string{"first copy", "second copy", `{"kind":"json"}`} {
		device.set(domain.ClipContent{Kind: domain.ClipKindText, Text: text})
		withClipboard.clipboard.Read(context.Background())
	}
	device.set(domain.ClipContent{Kind: domain.ClipKindFile, Files: []string{"/tmp/report.pdf", "/tmp/notes.txt"}})
	withClipboard.clipboard.Read(context.Background())
	write("history.png", withClipboard.historyView, historyWidth, historyHeight)
	write("settings-clipboard.png", withClipboard.settingsView, settingsWidth, settingsHeight)

	// A history longer than its window: the list scrolls and the footer
	// stays where it is.
	for i := 0; i < 10; i++ {
		device.set(domain.ClipContent{
			Kind: domain.ClipKindText,
			Text: fmt.Sprintf("copy %d: a value long enough to be noticed", i),
		})
		withClipboard.clipboard.Read(context.Background())
	}
	write("history-long.png", withClipboard.historyView, historyWidth, historyHeight)

	// The list answering the pointer: a row under it, and a row that was
	// just clicked. These are hard to judge from a single frame, so both
	// states are written out.
	tt := ui.NewTester(withClipboard.historyView, historyWidth, historyHeight)
	entries := withClipboard.clipboardHistory()
	if len(entries) > 1 {
		if row, ok := tt.Find(entries[1].Line()); ok {
			tt.Move(row.X+row.W-4, row.Y+row.H/2)
			file, err := os.Create(filepath.Join(dir, "history-hover.png"))
			if err != nil {
				t.Fatalf("create history-hover.png: %v", err)
			}
			if err := png.Encode(file, tt.Image()); err != nil {
				t.Fatalf("encode history-hover.png: %v", err)
			}
			file.Close()
		}
	}
	withClipboard.markCopied(entries[0])
	write("history-copied.png", withClipboard.historyView, historyWidth, historyHeight)
	withClipboard.clearCopied()
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
