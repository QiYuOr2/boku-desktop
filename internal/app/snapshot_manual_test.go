package app

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	deepseekpanel "boku/internal/panel/deepseek"
)

// TestSnapshotScreensForReadme renders the application's windows offscreen and
// writes them as PNGs, for the README's preview images. It is skipped unless
// BOKU_SNAPSHOT_DIR names a directory, so an ordinary `go test ./...` writes
// nothing.
//
// This is not a test: it has no pass condition beyond "the frames rendered".
// Look at the files it writes.
func TestSnapshotScreensForReadme(t *testing.T) {
	dir := os.Getenv("BOKU_SNAPSHOT_DIR")
	if dir == "" {
		t.Skip("set BOKU_SNAPSHOT_DIR to write the README screenshots")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}

	// pad leaves room outside the window on every side. A screenshot cropped
	// to the window's own edge puts the reader right against the frame — and
	// the flyout draws its own rounded rectangle, which is invisible when the
	// picture ends where the window does.
	const pad = 24

	t.Run("flyout", func(t *testing.T) {
		state := snapshotState(t)
		// The balance section's numbers, opened: the window is a fixed height
		// and closed it is mostly empty space, which photographs as a bug.
		state.detailsOpen = true
		img := ui.Render(state.View, panelWidth+pad*2, panelHeight+pad*2, 2)
		writePNG(t, filepath.Join(dir, "flyout.png"), img)
	})

	t.Run("settings", func(t *testing.T) {
		state := snapshotState(t)
		img := ui.Render(state.settingsView, settingsWidth+pad*2, settingsHeight+pad*2, 2)
		writePNG(t, filepath.Join(dir, "settings.png"), img)
	})
}

// snapshotState builds a state holding the things the windows are meant to
// show: a fetched panel whose numbers are in, and a few clipboard entries, so
// neither window is photographed empty or mid-load.
func snapshotState(t *testing.T) *State {
	t.Helper()

	device := &testDevice{}
	tools := newTestToolStore()
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.clipboard = testClipboard(t, device, tools)
	loaded(t, state)

	// A copied history: each write lands on the device, and a read picks it
	// up the way the poll does.
	if err := state.clipboard.SetRegionVisible(context.Background(), true); err != nil {
		t.Fatalf("show the clipboard region: %v", err)
	}
	for _, text := range []string{
		"https://github.com/egoist/mygo",
		"go tool mygo build",
		"func (s *State) View(c *ui.Context)",
	} {
		if err := device.WriteText(context.Background(), text); err != nil {
			t.Fatalf("write %q: %v", text, err)
		}
		state.clipboard.Read(context.Background())
	}

	// The panel's numbers, from the provider the test owns.
	state.refreshAsync(false)
	deadline := time.Now().Add(2 * time.Second)
	for state.panelService.View(deepseekpanel.PanelID).Loading && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if state.panelService.View(deepseekpanel.PanelID).Loading {
		t.Fatal("the panel never finished loading")
	}
	return state
}

// writePNG encodes img at path.
func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
	t.Logf("wrote %s", path)
}
