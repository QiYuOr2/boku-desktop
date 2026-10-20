package app

import (
	"image"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
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

func TestInitialWindowPointUsesPhysicalDisplayOrigin(t *testing.T) {
	display := mygo.Display{Bounds: mygo.Rectangle{X: -1200, Y: 80, Width: 1200, Height: 800}, ScaleFactor: 1.5}
	x, y := initialWindowPoint(display)
	got := image.Pt(x, y)
	want := image.Pt(-1800, 120)
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

func TestPhysicalDesktopRectScalesTheMonitorOriginAndLocalSelectionSeparately(t *testing.T) {
	display := mygo.Display{Bounds: mygo.Rectangle{X: 1707, Y: -337, Width: 853, Height: 1369}, ScaleFactor: 1.5}
	got := physicalDesktopRect(display, image.Rect(20, 30, 120, 230))
	want := image.Rect(2591, -461, 2741, -161)
	if got != want {
		t.Fatalf("physicalDesktopRect() = %v, want %v", got, want)
	}
}
