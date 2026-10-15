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
		settings: domain.ToolSettings{Clipboard: domain.ClipboardSettings{RegionVisible: true, Monitoring: true}},
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

// rowFace is the face a row is drawn on: the colors in the room it keeps
// around its text, which is where the row's background shows and no glyph
// reaches. A single pixel cannot say this: where a letter lands depends on
// the font, and where a row begins depends on the layout around it.
//
// It draws the frame again first, so that a rect read before a pointer moved
// is not used to read pixels after it.
func rowFace(t *testing.T, tt *ui.Tester, label string) map[color.RGBA]int {
	t.Helper()
	rect, ok := tt.Find(label)
	if !ok {
		t.Fatalf("the row for %q was not found; the window shows %q", label, tt.Texts())
	}
	img := tt.Image()
	y := int(rect.Y + rect.H/2)
	counts := map[color.RGBA]int{}
	for x := int(rect.X) - 12; x < int(rect.X); x++ {
		counts[img.RGBAAt(x, y)]++
	}
	for x := int(rect.X+rect.W) + 4; x < historyWidth; x++ {
		counts[img.RGBAAt(x, y)]++
	}
	return counts
}

// sameFace reports whether two bands were drawn the same.
func sameFace(a, b map[color.RGBA]int) bool {
	if len(a) != len(b) {
		return false
	}
	for c, n := range a {
		if b[c] != n {
			return false
		}
	}
	return true
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

	away := rowFace(t, tt, "another copy")
	otherAway := rowFace(t, tt, "a copy that can be clicked")

	// The pointer onto one row, and only that one changes.
	x, y := rowCentre(t, tt, "another copy")
	tt.Move(x, y)

	if hovered := rowFace(t, tt, "another copy"); sameFace(hovered, away) {
		t.Errorf("the row is the same face under the pointer: %v", hovered)
	}
	if other := rowFace(t, tt, "a copy that can be clicked"); !sameFace(other, otherAway) {
		t.Errorf("the row the pointer is not on changed face: %v, was %v", other, otherAway)
	}
}

// A row that was clicked is marked while the copy is written, and the mark
// goes away by itself: a click needs an answer that is not the next poll.
func TestAClickedRowIsMarkedAndThenCleared(t *testing.T) {
	device := &testDevice{}
	state := clipboardState(t, device, &testToolStore{
		settings: domain.ToolSettings{Clipboard: domain.ClipboardSettings{RegionVisible: true, Monitoring: true}},
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
	markedX, markedY = rowCentre(t, tt, entry.Line())
	plain := colourAt(tt.Image(), markedX, markedY)
	if marked == plain {
		t.Error("the mark does not change the row it is on")
	}
	if state.copiedEntry != "" {
		t.Errorf("the mark outlived its moment: %q", state.copiedEntry)
	}
}
