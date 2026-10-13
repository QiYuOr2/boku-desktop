package app

import (
	"context"
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
)

// The history list answers the pointer: a row under it has a face, and a row
// that was clicked says so before the next poll.

// historyState is a state whose history holds one copy, with the pointer
// where the test puts it.
func historyState(t testing.TB, copies ...string) *State {
	t.Helper()
	device := &testDevice{}
	state := clipboardState(t, device, &testToolStore{
		settings: domain.ToolSettings{ClipboardRegionVisible: true, ClipboardMonitoring: true},
	})
	for _, copy := range copies {
		device.set(domain.ClipContent{Kind: domain.ClipKindText, Text: copy})
		state.clipboard.Read(context.Background())
	}
	return state
}

// waitForWrite waits for the copy to reach the clipboard. The write is on its
// own goroutine, so it lands after the click returns; waiting on a goroutine
// is the one place a test may sleep.
func waitForWrite(t *testing.T, device *testDevice, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		written := device.written()
		if len(written) > 0 {
			if written[0] != want {
				t.Errorf("the click wrote %q, want %q", written[0], want)
			}
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("the click never reached the clipboard")
}

// rowCentre is the middle of a row, one point in from its right edge so that
// the sample lands on the row's face rather than on the text.
func rowCentre(t *testing.T, tt *ui.Tester, label string) (float32, float32) {
	t.Helper()
	row, ok := tt.Find(label)
	if !ok {
		t.Fatalf("the row for %q was not found; the window shows %q", label, tt.Texts())
	}
	return row.X + row.W - 3, row.Y + row.H/2
}

// colourAt is the pixel the frame drew at a point.
func colourAt(img *image.RGBA, x, y float32) color.RGBA {
	return img.RGBAAt(int(x), int(y))
}

// A row under the pointer has a face; the same row without the pointer does
// not. Without this the list is ten lines of text with no sign that any of
// them can be clicked.
func TestARowDarkensUnderThePointer(t *testing.T) {
	state := historyState(t, "a copy that can be clicked", "another copy")
	tt := ui.NewTester(state.historyView, historyWidth, historyHeight)

	// Where the pointer is not.
	x, y := rowCentre(t, tt, "another copy")
	away := colourAt(tt.Image(), x, y)

	// The pointer onto the row.
	tt.Move(x, y)
	hovered := colourAt(tt.Image(), x, y)

	if hovered == away {
		t.Errorf("the row is the same colour under the pointer: %v", hovered)
	}
	// And the row that is away keeps its own face.
	otherX, otherY := rowCentre(t, tt, "a copy that can be clicked")
	if got := colourAt(tt.Image(), otherX, otherY); got != away {
		t.Errorf("the row the pointer is not on changed colour: %v, was %v", got, away)
	}
}

// A row that was clicked is marked while the copy is written, and the mark
// goes away by itself: a click needs an answer that is not the next poll.
func TestAClickedRowIsMarkedAndThenCleared(t *testing.T) {
	device := &testDevice{}
	state := clipboardState(t, device, &testToolStore{
		settings: domain.ToolSettings{ClipboardRegionVisible: true, ClipboardMonitoring: true},
	})
	device.set(domain.ClipContent{Kind: domain.ClipKindText, Text: "a copy that can be clicked"})
	state.clipboard.Read(context.Background())

	entry := state.clipboardHistory()[0]
	state.copyEntry(entry)

	if state.copiedEntry != entry.ID {
		t.Errorf("the clicked row is not marked: %q, want %q", state.copiedEntry, entry.ID)
	}
	// The write is sent, not waited for: it lands a moment later.
	waitForWrite(t, device, entry.Content)

	// The mark is drawn: the row's face is not the plain one.
	tt := ui.NewTester(state.historyView, historyWidth, historyHeight)
	markedX, markedY := rowCentre(t, tt, entry.Line())
	marked := colourAt(tt.Image(), markedX, markedY)

	// And it is taken back once it has been seen: the state the timer
	// changes is the state the next frame reads.
	state.clearCopied()
	tt.Frame()
	plain := colourAt(tt.Image(), markedX, markedY)
	if marked == plain {
		t.Error("the mark does not change the row it is on")
	}
	if state.copiedEntry != "" {
		t.Errorf("the mark outlived its moment: %q", state.copiedEntry)
	}
}
