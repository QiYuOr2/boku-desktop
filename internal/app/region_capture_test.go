package app

import (
	"fmt"
	"image"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
)

func TestNormalizedDIPRectAcceptsEveryDragDirection(t *testing.T) {
	got := normalizedDIPRect(mygo.Point{X: 80, Y: 45}, mygo.Point{X: 20, Y: 10})
	want := image.Rect(20, 10, 80, 45)
	if got != want {
		t.Fatalf("normalizedDIPRect() = %v, want %v", got, want)
	}
}

func TestCapturingSelectionIgnoresQueuedPointerMoves(t *testing.T) {
	session := &regionCaptureSession{
		capturing: true,
		dragging:  true,
		current:   mygo.Point{X: 20, Y: 30},
	}
	session.input(ui.InputEvent{Kind: ui.InputPointerMove, X: 80, Y: 90}, mygo.Display{})
	if got := session.current; got != (mygo.Point{X: 20, Y: 30}) {
		t.Fatalf("queued pointer move changed current point to %v", got)
	}
}

func TestInitialWindowPointUsesTheDisplayOriginInDIPs(t *testing.T) {
	display := mygo.Display{Bounds: mygo.Rectangle{X: -1200, Y: 80, Width: 1200, Height: 800}, ScaleFactor: 1.5}
	x, y := initialWindowPoint(display)
	got := image.Pt(x, y)
	want := image.Pt(-1200, 80)
	if got != want {
		t.Fatalf("initialWindowPoint() = %v, want %v", got, want)
	}
}

func TestInitialWindowPointAvoidsZeroZeroCentering(t *testing.T) {
	x, y := initialWindowPoint(mygo.Display{Bounds: mygo.Rectangle{X: 0, Y: 0}})
	got := image.Pt(x, y)
	want := image.Pt(1, 1)
	if got != want {
		t.Fatalf("initialWindowPoint() = %v, want %v", got, want)
	}
}

func TestDesktopRectScalesTheMonitorOriginAndLocalSelectionSeparately(t *testing.T) {
	display := mygo.Display{Bounds: mygo.Rectangle{X: 1707, Y: -337, Width: 853, Height: 1369}}
	got := desktopRect(display, image.Rect(20, 30, 120, 230), 1.5)
	want := image.Rect(2591, -461, 2741, -161)
	if got != want {
		t.Fatalf("desktopRect() = %v, want %v", got, want)
	}
}

func TestDesktopRectKeepsPointsAtScaleOne(t *testing.T) {
	display := mygo.Display{Bounds: mygo.Rectangle{X: -1200, Y: -80, Width: 1200, Height: 800}, ScaleFactor: 2}
	got := desktopRect(display, image.Rect(10, 20, 110, 80), 1)
	want := image.Rect(-1190, -60, -1090, 0)
	if got != want {
		t.Fatalf("desktopRect() = %v, want %v", got, want)
	}
}

func TestMaskPartsLeavesTheSelectionClear(t *testing.T) {
	full := image.Rect(0, 0, 100, 80)
	got := maskParts(full, image.Rect(10, 20, 30, 40))
	want := []image.Rectangle{
		image.Rect(0, 0, 100, 20),
		image.Rect(0, 20, 10, 40),
		image.Rect(30, 20, 100, 40),
		image.Rect(0, 40, 100, 80),
	}
	if len(got) != len(want) {
		t.Fatalf("maskParts() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("maskParts()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestMaskPartsWithoutASelectionMasksEverything(t *testing.T) {
	full := image.Rect(0, 0, 100, 80)
	for _, hole := range []image.Rectangle{
		image.Rectangle{},
		image.Rect(10, 10, 10, 10),
		image.Rect(-40, -40, -10, -10),
		image.Rect(200, 200, 300, 300),
	} {
		got := maskParts(full, hole)
		if len(got) != 1 || got[0] != full {
			t.Fatalf("maskParts(%v, %v) = %v, want [%v]", full, hole, got, full)
		}
	}
}

func TestMaskPartsDropsTheBandsTheSelectionCovers(t *testing.T) {
	full := image.Rect(0, 0, 100, 80)
	// A selection along the top edge leaves only the right and bottom bands.
	got := maskParts(full, image.Rect(0, 0, 40, 30))
	want := []image.Rectangle{
		image.Rect(40, 0, 100, 30),
		image.Rect(0, 30, 100, 80),
	}
	if len(got) != len(want) {
		t.Fatalf("maskParts() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("maskParts()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestMaskPartsMasksNothingWhenTheSelectionIsTheWholeDisplay(t *testing.T) {
	full := image.Rect(0, 0, 100, 80)
	if got := maskParts(full, full); len(got) != 0 {
		t.Fatalf("maskParts() = %v, want no band", got)
	}
}

func TestSelectionOnlyOpensOnTheDisplayTheDragStartedOn(t *testing.T) {
	session := &regionCaptureSession{
		display:  mygo.Display{ID: 2},
		start:    mygo.Point{X: 10, Y: 10},
		current:  mygo.Point{X: 20, Y: 30},
		dragging: true,
	}
	if got := session.selectionOn(mygo.Display{ID: 1}); !got.Empty() {
		t.Fatalf("selectionOn(other display) = %v, want no selection", got)
	}
	if got, want := session.selectionOn(mygo.Display{ID: 2}), image.Rect(10, 10, 20, 30); got != want {
		t.Fatalf("selectionOn(drag display) = %v, want %v", got, want)
	}
	session.dragging = false
	if got := session.selectionOn(mygo.Display{ID: 2}); !got.Empty() {
		t.Fatalf("selectionOn(before a drag) = %v, want no selection", got)
	}
}

func TestMissingScreenRecordingPermissionIsItsOwnCard(t *testing.T) {
	err := fmt.Errorf("%w: macOS has not allowed Boku", domain.ErrScreenRecordingDenied)
	if got := translationFailureStatus(err); got != translateNeedsScreenRecording {
		t.Fatalf("translationFailureStatus() = %v, want translateNeedsScreenRecording", got)
	}
	if got := friendlyError(err); !strings.Contains(got, "屏幕录制") {
		t.Fatalf("friendlyError() = %q, want it to name the permission", got)
	}
}
