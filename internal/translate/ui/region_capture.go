package ui

import (
	"context"
	"fmt"
	"image"
	"log/slog"
	"math"
	"runtime"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	"boku/internal/platform/screenoverlay"
	"boku/internal/platform/screenrecording"
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
	// macOS answers a screen read without the Screen Recording permission with
	// black pixels, not an error. The selector must not open in that case: the
	// user would pick a region that could never be read. Asking here, on the
	// main thread, is also the one moment the system can put the request in
	// front of a user who just asked for a screenshot; the startup request may
	// have been suppressed for an accessory app with no window.
	if !screenrecording.Granted() {
		granted := false
		mygo.RunOnMain(func() { granted = screenrecording.Request() })
		if !granted {
			return nil, fmt.Errorf("%w: macOS has not allowed Boku to record the screen", domain.ErrScreenRecordingDenied)
		}
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
	state   *State
	ctx     context.Context
	capture RegionCapture
	answer  chan<- regionCaptureAnswer
	// display is the display the drag started on: only its overlay draws a
	// selection, the others stay fully masked.
	display        mygo.Display
	windows        []*mygo.Window
	start, current mygo.Point
	dragging       bool
	capturing      bool
	selected       image.Rectangle
	once           sync.Once
}

const regionCaptureMinSide = 4

// requestScreenRecording puts Boku in System Settings > Privacy & Security >
// Screen Recording on macOS, and asks the user for it the first time. The
// region capture cannot read the desktop without it, and macOS only lists an
// app that asked: asking at startup gives the user a switch to turn on instead
// of a screenshot that fails with nothing to click. macOS remembers the
// answer, so Boku does not ask again on every launch; a denial shows up as
// the capture's own error.
func requestScreenRecording() {
	if runtime.GOOS != "darwin" {
		return
	}
	granted := screenrecording.Request()
	slog.Info("screen recording access", "granted", granted)
}

// openScreenRecordingSettings takes the user to the switch the screenshot
// needs. The card that offers it is only shown on macOS, where the URL means
// something; elsewhere SettingsURL is empty.
func (s *State) openScreenRecordingSettings() {
	url := screenrecording.SettingsURL()
	if url == "" {
		return
	}
	if err := mygo.Shell.OpenExternal(url); err != nil {
		slog.Warn("cannot open the screen recording settings", "event", "screen_recording_settings_open_failed", "error", err)
	}
}

// screenshotMask is the dim over everything but the selection. It is the same
// 62% black the Windows selector's layered window uses.
var screenshotMask = ui.RGBA(0, 0, 0, 0.62)

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
	session := &regionCaptureSession{state: s, ctx: ctx, capture: capture, answer: answer}
	s.regionSession = session
	for _, display := range displays {
		session.openOverlay(display)
	}
	go func() {
		<-ctx.Done()
		s.onMain(session.cancel)
	}()
}

func (session *regionCaptureSession) openOverlay(display mygo.Display) {
	if display.Bounds.Width <= 0 || display.Bounds.Height <= 0 {
		return
	}
	// Zero/zero means "centre" to WindowOptions. Build hidden at a point
	// inside the target monitor, then set its exact content bounds before it
	// becomes visible; Bounds, not WorkArea, so the overlay covers the taskbar
	// and, on macOS, the menu bar and the Dock.
	x, y := initialWindowPoint(display)
	// The overlay paints the mask itself, leaving the selection clear. The
	// separate dimmer windows this used to create existed for Windows, which
	// has no per-pixel alpha; the Windows selector is native now, and macOS
	// composites a transparent window per pixel. The dimmers were also why a
	// drag never reached this window: creating them on the first press put
	// them above it, and they took every move and the release.
	window := mygo.NewWindow(mygo.WindowOptions{
		Title: appName + " 截图翻译", X: x, Y: y, Width: 1, Height: 1,
		Hidden: true, UseContentSize: true, Frameless: true, Transparent: true,
		AlwaysOnTop: true, SkipTaskbar: true, DisableResize: true,
		// The system shadow follows what the window draws, so it would outline
		// the mask's bands, including the selection hole, which a flat dim does
		// not have.
		DisableShadow: true,
		Panel:         runtime.GOOS == "darwin", Content: ui.View(func(c *ui.Context) { session.view(c, display) }),
	})
	// A titled window is shifted down by AppKit so its title bar stays on
	// screen, and floats below the menu bar and the Dock: the overlay has to
	// be borderless and above both before it is placed and shown.
	if err := screenoverlay.Cover(window.NativeHandle()); err != nil {
		slog.Warn("the screenshot overlay cannot cover the whole display", "event", "screenshot_overlay_cover_failed", "error", err)
	}
	window.SetContentBounds(display.Bounds)
	window.OnClosed(func() {
		if session.capturing {
			// captureSelected closes the overlays itself and reads after them.
			return
		}
		session.clearRegionSession()
		session.finish(nil, context.Canceled)
	})
	session.windows = append(session.windows, window)
	window.Show()
}

func (session *regionCaptureSession) view(c *ui.Context, display mygo.Display) {
	root := c.Root()
	root.Background(ui.Transparent)
	// The mask is painted by the input area itself, not by child elements.
	// MyGo commits absolute children after the in-flow ones and puts them in
	// the hit chain first, so a mask box laid over the area would be the
	// element under the pointer and take the press from the drag. Painting it
	// here keeps one element that owns both the dim and the pointer.
	full := image.Rect(0, 0, display.Bounds.Width, display.Bounds.Height)
	area := ui.Box(c).Fill().Focusable().FocusRing(false).Background(ui.Transparent)
	area.Draw(func(p *ui.Painter, _ ui.Rect) {
		for _, part := range maskParts(full, session.selectionOn(display)) {
			p.Fill(ui.Rect{
				X: float32(part.Min.X), Y: float32(part.Min.Y),
				W: float32(part.Dx()), H: float32(part.Dy()),
			}, screenshotMask, 0)
		}
	})
	area.HandleInput(func(ev ui.InputEvent) bool {
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
	// Pointer events already queued behind the mouse-up must not rebuild the
	// selection after captureSelected has closed the overlays.
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
			return true
		}
	case ui.InputPointerMove:
		if session.dragging {
			session.current = mygo.Point{X: int(ev.X), Y: int(ev.Y)}
			return true
		}
	case ui.InputPointerUp:
		if session.dragging && ev.Button == 0 {
			session.current = mygo.Point{X: int(ev.X), Y: int(ev.Y)}
			r := normalizedDIPRect(session.start, session.current)
			// The selection belongs to the display it started on. A pointer
			// dragged into a neighbouring display keeps sending moves here,
			// and the pixel read stops at this display's edge anyway.
			r = r.Intersect(image.Rect(0, 0, display.Bounds.Width, display.Bounds.Height))
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
	// Destroy the overlays first: the pixel reader must see the desktop below,
	// not Boku's own mask or selection.
	session.capturing = true
	// The drag stays as it is until the window is gone, so a frame that lands
	// between the release and the close keeps the selection a hole instead of
	// dimming it. The capture rectangle is that same hole.
	// The close callbacks must not start the read: Window.Close runs them
	// before the native window is destroyed, so reading there would capture
	// Boku's own mask. Every window is closed on this thread, then the desktop
	// is read. The overlay has no close animation (screenoverlay.Cover), so
	// the window is off screen by the time Close returns.
	session.selected = nativeRegionRect(session.display, dip)
	slog.Info("screenshot region selected", "event", "screenshot_region_selected", "width", session.selected.Dx(), "height", session.selected.Dy())
	for _, window := range session.windows {
		window.Close()
	}
	session.clearRegionSession()
	session.readSelected()
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
	for _, window := range session.windows {
		window.Close()
	}
	session.finish(nil, context.Canceled)
}

func (session *regionCaptureSession) clearRegionSession() {
	if session.state != nil && session.state.regionSession == session {
		session.state.regionSession = nil
	}
}

func (session *regionCaptureSession) finish(png []byte, err error) {
	session.once.Do(func() { session.answer <- regionCaptureAnswer{png: png, err: err} })
}

// selectionOn returns the current selection in this display's DIPs, or an
// empty rectangle when the drag belongs to another display or has not started.
func (session *regionCaptureSession) selectionOn(display mygo.Display) image.Rectangle {
	if !session.dragging || session.display.ID != display.ID {
		return image.Rectangle{}
	}
	return normalizedDIPRect(session.start, session.current)
}

func normalizedDIPRect(a, b mygo.Point) image.Rectangle {
	return image.Rect(min(a.X, b.X), min(a.Y, b.Y), max(a.X, b.X), max(a.Y, b.Y))
}

// maskParts splits full into the rectangles around hole: the bands of the dim
// that stay, so that the selection is a transparent hole in one window rather
// than a separate window that would sit above the pointer. hole is clipped to
// full, and an empty or outside hole leaves the whole rectangle masked.
func maskParts(full, hole image.Rectangle) []image.Rectangle {
	hole = hole.Intersect(full)
	if hole.Empty() {
		return []image.Rectangle{full}
	}
	parts := []image.Rectangle{
		image.Rect(full.Min.X, full.Min.Y, full.Max.X, hole.Min.Y),
		image.Rect(full.Min.X, hole.Min.Y, hole.Min.X, hole.Max.Y),
		image.Rect(hole.Max.X, hole.Min.Y, full.Max.X, hole.Max.Y),
		image.Rect(full.Min.X, hole.Max.Y, full.Max.X, full.Max.Y),
	}
	kept := parts[:0]
	for _, part := range parts {
		if !part.Empty() {
			kept = append(kept, part)
		}
	}
	return kept
}

func initialWindowPoint(display mygo.Display) (int, int) {
	// WindowOptions.X and Y are DIPs, like Bounds. Zero/zero would centre the
	// window on the primary display instead of building it in this one.
	x, y := display.Bounds.X, display.Bounds.Y
	if x == 0 && y == 0 {
		return 1, 1
	}
	return x, y
}

// desktopRect maps a monitor-local DIP selection into the coordinate space a
// platform pixel reader takes. The monitor origin and the selection are
// scaled separately: scaling a global rectangle as one unit breaks mixed-DPI
// layouts whose displays start at different origins. The scale is 1 where the
// reader works in screen points (CoreGraphics) and the display's factor where
// it works in physical pixels (GDI).
func desktopRect(display mygo.Display, local image.Rectangle, scale float64) image.Rectangle {
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
