package app

import (
	"context"
	"fmt"
	"image"
	"log/slog"
	"math"
	"runtime"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
)

// nativeRegionScreenshot is the ScreenshotDevice adapter used in production.
// Selection is intentionally presentation work: its transparent window is a
// MyGo view. Pixel access remains behind RegionCapture in platform.
type nativeRegionScreenshot struct {
	state   *State
	capture RegionCapture
}

func (d *nativeRegionScreenshot) Capture(ctx context.Context) ([]byte, error) {
	if d == nil || d.state == nil || d.capture == nil {
		return nil, fmt.Errorf("%w: region capture is unavailable", domain.ErrImageTranslateUnavailable)
	}
	slog.Info("screenshot region selector opened", "event", "screenshot_region_selector_opened")
	// Windows owns a native virtual-desktop selector. It works in physical
	// pixels, exactly like its GDI reader, so a mixed-DPI secondary display
	// cannot be positioned or captured through a primary-display coordinate
	// system. Other platforms keep the app-owned selector below.
	if selector, ok := d.capture.(interface {
		Select(context.Context) ([]byte, error)
	}); ok {
		return selector.Select(ctx)
	}
	answer := make(chan regionCaptureAnswer, 1)
	d.state.onMain(func() { d.state.openRegionCapture(ctx, d.capture, answer) })
	select {
	case result := <-answer:
		return result.png, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type regionCaptureAnswer struct {
	png []byte
	err error
}

type regionCaptureSession struct {
	state          *State
	ctx            context.Context
	capture        RegionCapture
	answer         chan<- regionCaptureAnswer
	display        mygo.Display
	windows        []*mygo.Window
	inputWindows   map[int64]*mygo.Window
	dimmers        map[int64][]*mygo.Window
	start, current mygo.Point
	dragging       bool
	capturing      bool
	closing        int
	selected       image.Rectangle
	once           sync.Once
}

const regionCaptureMinSide = 4

func (s *State) openRegionCapture(ctx context.Context, capture RegionCapture, answer chan<- regionCaptureAnswer) {
	// A global shortcut cannot create two competing overlays. The previous
	// request gets a deterministic cancellation rather than taking a stale
	// screenshot from a later drag.
	if s.regionSession != nil {
		s.regionSession.cancel()
	}
	displays := mygo.Screen.Displays()
	if len(displays) == 0 {
		answer <- regionCaptureAnswer{err: fmt.Errorf("%w: no display", domain.ErrImageTranslateUnavailable)}
		return
	}
	session := &regionCaptureSession{state: s, ctx: ctx, capture: capture, answer: answer, inputWindows: make(map[int64]*mygo.Window), dimmers: make(map[int64][]*mygo.Window)}
	s.regionSession = session
	for _, display := range displays {
		session.openOverlay(s, display)
	}
	go func() {
		<-ctx.Done()
		s.onMain(session.cancel)
	}()
}

func (session *regionCaptureSession) openOverlay(state *State, display mygo.Display) {
	if display.Bounds.Width <= 0 || display.Bounds.Height <= 0 {
		return
	}
	// Zero/zero means "centre" to WindowOptions. Build hidden at one point
	// inside the target monitor, then set its exact content bounds before it
	// becomes visible. This fixes the primary-monitor upward offset and covers
	// the taskbar because Bounds, not WorkArea, is used.
	x, y := initialWindowPoint(display)
	// MyGo's native surface has no per-pixel alpha on Windows. The input
	// window is therefore almost transparent, while separate native dimmer
	// windows create the four real holes around a selection.
	session.setDimmers(display, image.Rectangle{})
	window := mygo.NewWindow(mygo.WindowOptions{
		Title: appName + " 截图翻译", X: x, Y: y, Width: 1, Height: 1,
		Hidden: true, UseContentSize: true, Frameless: true, Transparent: true,
		AlwaysOnTop: true, SkipTaskbar: true, DisableResize: true,
		Opacity: 0.01, Panel: runtime.GOOS == "darwin", Content: ui.View(func(c *ui.Context) { session.view(c, display) }),
	})
	window.SetContentBounds(display.Bounds)
	window.OnClosed(func() {
		if session.capturing {
			session.closing--
			if session.closing == 0 {
				if state.regionSession == session {
					state.regionSession = nil
				}
				session.readSelected()
			}
			return
		}
		if state.regionSession == session {
			state.regionSession = nil
		}
		session.finish(nil, context.Canceled)
	})
	session.windows = append(session.windows, window)
	session.inputWindows[display.ID] = window
	window.Show()
}

func (session *regionCaptureSession) view(c *ui.Context, display mygo.Display) {
	root := c.Root()
	root.Background(ui.Transparent)
	area := ui.Box(c).Fill().Focusable().FocusRing(false).Background(ui.Transparent).HandleInput(func(ev ui.InputEvent) bool {
		return session.input(ev, display)
	})
	// This is an overlay shortcut: unlike a focused element shortcut it sees
	// Escape even when another monitor's overlay owns the keyboard focus.
	if area.OverlayShortcut(0, ui.KeyEscape) {
		session.cancel()
		return
	}
}

func (session *regionCaptureSession) input(ev ui.InputEvent, display mygo.Display) bool {
	// Closing the native windows is asynchronous. Pointer moves already queued
	// behind the mouse-up must not rebuild dimmers after captureSelected has
	// removed them, or they remain above the translated page.
	if session.capturing {
		return true
	}
	switch ev.Kind {
	case ui.InputKeyDown:
		if ev.Key == ui.KeyEscape {
			session.cancel()
			return true
		}
	case ui.InputPointerDown:
		if ev.Button == 0 {
			session.display = display
			session.start = mygo.Point{X: int(ev.X), Y: int(ev.Y)}
			session.current = session.start
			session.dragging = true
			session.setDimmers(display, normalizedDIPRect(session.start, session.current))
			return true
		}
	case ui.InputPointerMove:
		if session.dragging {
			session.current = mygo.Point{X: int(ev.X), Y: int(ev.Y)}
			session.setDimmers(display, normalizedDIPRect(session.start, session.current))
			return true
		}
	case ui.InputPointerUp:
		if session.dragging && ev.Button == 0 {
			session.current = mygo.Point{X: int(ev.X), Y: int(ev.Y)}
			r := normalizedDIPRect(session.start, session.current)
			if r.Dx() < regionCaptureMinSide || r.Dy() < regionCaptureMinSide {
				session.cancel()
				return true
			}
			session.captureSelected(r)
			return true
		}
	}
	return false
}

func (session *regionCaptureSession) captureSelected(dip image.Rectangle) {
	// Destroy the overlay first: the pixel reader must see the desktop below,
	// not Boku's dimmer or selection border.
	session.capturing = true
	session.dragging = false
	// The close callback starts the capture. Set the selected rectangle first:
	// MyGo reports Closed synchronously on Windows, so assigning it afterwards
	// used to send image.Rect(0, 0, 0, 0) to GDI and produced a blank result.
	session.selected = physicalDesktopRect(session.display, dip)
	slog.Info("screenshot region selected", "event", "screenshot_region_selected", "width", session.selected.Dx(), "height", session.selected.Dy())
	session.closeDimmers()
	// MyGo may queue a borderless Window.Close behind the input event that
	// created it. Retry after that event has returned, retaining the window
	// references so a deferred native close cannot leave a dead dimmer over
	// the desktop.
	if session.state != nil {
		time.AfterFunc(100*time.Millisecond, func() {
			session.state.onMain(session.closeDimmers)
		})
	}
	session.closing = len(session.windows)
	for _, window := range session.windows {
		window.Close()
	}
	if session.closing == 0 {
		session.readSelected()
	}
}

func (session *regionCaptureSession) readSelected() {
	go func() {
		png, err := session.capture.Capture(session.ctx, session.selected)
		session.finish(png, err)
	}()
}

func (session *regionCaptureSession) cancel() {
	if session.capturing {
		return
	}
	slog.Info("screenshot region selection cancelled", "event", "screenshot_region_selection_cancelled")
	session.closeDimmers()
	for _, window := range session.windows {
		window.Close()
	}
	session.finish(nil, context.Canceled)
}

func (session *regionCaptureSession) finish(png []byte, err error) {
	session.once.Do(func() { session.answer <- regionCaptureAnswer{png: png, err: err} })
}

func normalizedDIPRect(a, b mygo.Point) image.Rectangle {
	return image.Rect(min(a.X, b.X), min(a.Y, b.Y), max(a.X, b.X), max(a.Y, b.Y))
}

func initialWindowPoint(display mygo.Display) (int, int) {
	scale := display.ScaleFactor
	if scale <= 0 {
		scale = 1
	}
	x, y := int(math.Round(float64(display.Bounds.X)*scale)), int(math.Round(float64(display.Bounds.Y)*scale))
	if x == 0 && y == 0 {
		return 1, 1
	}
	return x, y
}

// physicalDesktopRect converts a monitor-local MyGo DIP selection to the
// physical virtual desktop consumed by Win32 GDI and CoreGraphics. The
// monitor origin is also in that monitor's DIPs; scaling a global rectangle
// as one unit breaks mixed-DPI layouts.
func physicalDesktopRect(display mygo.Display, local image.Rectangle) image.Rectangle {
	scale := display.ScaleFactor
	if scale <= 0 {
		scale = 1
	}
	x := int(math.Round(float64(display.Bounds.X) * scale))
	y := int(math.Round(float64(display.Bounds.Y) * scale))
	return image.Rect(
		x+int(math.Round(float64(local.Min.X)*scale)),
		y+int(math.Round(float64(local.Min.Y)*scale)),
		x+int(math.Round(float64(local.Max.X)*scale)),
		y+int(math.Round(float64(local.Max.Y)*scale)),
	)
}

func (session *regionCaptureSession) setDimmers(display mygo.Display, selection image.Rectangle) {
	for _, window := range session.dimmers[display.ID] {
		// Dimmers have no close semantics of their own. Destroy them directly:
		// Windows may defer a normal Close while another borderless overlay owns
		// the pointer, leaving an invisible top-level window over the desktop.
		dismissDimmer(window)
	}
	session.dimmers[display.ID] = nil
	full := image.Rect(0, 0, display.Bounds.Width, display.Bounds.Height)
	parts := []image.Rectangle{full}
	if !selection.Empty() {
		parts = []image.Rectangle{
			image.Rect(0, 0, full.Max.X, selection.Min.Y), image.Rect(0, selection.Min.Y, selection.Min.X, selection.Max.Y),
			image.Rect(selection.Max.X, selection.Min.Y, full.Max.X, selection.Max.Y), image.Rect(0, selection.Max.Y, full.Max.X, full.Max.Y),
		}
	}
	for _, part := range parts {
		if part.Empty() {
			continue
		}
		session.dimmers[display.ID] = append(session.dimmers[display.ID], newDimmerWindow(display, part))
	}
	if input := session.inputWindows[display.ID]; input != nil {
		input.SetAlwaysOnTop(true)
	}
}

func newDimmerWindow(display mygo.Display, local image.Rectangle) *mygo.Window {
	x, y := initialWindowPoint(display)
	window := mygo.NewWindow(mygo.WindowOptions{Title: appName + " 截图翻译遮罩", X: x, Y: y, Width: 1, Height: 1, Hidden: true, UseContentSize: true, Frameless: true, AlwaysOnTop: true, SkipTaskbar: true, DisableResize: true, Opacity: 0.62, Panel: runtime.GOOS == "darwin", Content: ui.View(func(c *ui.Context) { c.Root().Background(ui.Hex("#000000")) })})
	window.SetContentBounds(mygo.Rectangle{X: display.Bounds.X + local.Min.X, Y: display.Bounds.Y + local.Min.Y, Width: local.Dx(), Height: local.Dy()})
	window.Show()
	return window
}

func (session *regionCaptureSession) closeDimmers() {
	for _, windows := range session.dimmers {
		for _, window := range windows {
			dismissDimmer(window)
		}
	}
}

// dismissDimmer removes a dimmer from the desktop before asking the native
// backend to dispose it. Hide is intentional redundancy for Windows: a
// borderless always-on-top window can otherwise outlive its asynchronous close
// notification for a frame and intercept the next interaction.
func dismissDimmer(window *mygo.Window) {
	if window == nil {
		return
	}
	window.Hide()
	window.Destroy()
}
