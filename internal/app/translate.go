package app

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	"boku/internal/kit"
	"boku/internal/translate"
)

// The translation window is the tool's face: what was selected, what it
// became, and the few things that can be done with either.
//
// It is a card rather than a flyout: it appears where the pointer was when
// the shortcut was pressed, it stays on top of what the user was reading,
// and it closes itself when they look away — unless they pin it, which is
// what the pin is for.
//
// The window is created once and reused. It is repositioned for each
// translation, which is cheaper than a window per press and keeps the shape
// of the window stable: the answer replaces the placeholder in the same
// card, rather than a second card appearing.
const (
	// translateWidth is the card's width: wide enough for a sentence of
	// Japanese with its readings above the words and for the translation
	// under it without wrapping at every third word, and no wider. The
	// design's own card is narrower still; this is that card at a size that
	// keeps a sentence on one line.
	translateWidth = 478
	// translateMinHeight is the shortest the card may be: a line of notice, a
	// hairline and the foot.
	translateMinHeight = 84
	// translateMaxHeight is the tallest: past this the body scrolls, because a
	// card taller than the screen is a window rather than a card.
	translateMaxHeight = 620
	// translateHeight is what the window starts at, before a frame has
	// measured what is in it.
	translateHeight = 240
	// translateGap is how far the card sits from the pointer: far enough not
	// to be under it, close enough to be where the user is looking.
	translateGap = 12
	// copiedMarkDuration is how long a copy action says what it did. It is
	// the clipboard history's own pause, because it is the same gesture.
	copiedMarkDuration = 700 * time.Millisecond
)

// translateStatus is what the card is showing. It is a state rather than a
// set of booleans because the card draws exactly one of them, and which one
// is the whole of what it has to say.
type translateStatus int

const (
	// translateIdle is before anything was asked for.
	translateIdle translateStatus = iota
	// translateLoading is while the translator is being asked.
	translateLoading
	// translateReady is a translation.
	translateReady
	// translateQRReady is the text of a QR code, read locally. It is not a
	// translation: it has no target language, no service and no annotation.
	translateQRReady
	// translateOff is the tool switched off in the settings.
	translateOff
	// translateUnconfigured is the tool with no usable translator.
	translateUnconfigured
	// translateNoText is a clipboard with nothing to translate in it.
	translateNoText
	// translateNeedsScreenRecording is macOS without the permission the
	// screenshot read needs. It is not a translation failure: the fix is a
	// switch in System Settings, and the card says where it is.
	translateNeedsScreenRecording
	// translateFailed is a translator that failed, with the reason kept.
	translateFailed
)

// translationState is the window's state, on the main thread with the rest of
// the view state.
type translationState struct {
	status translateStatus
	// image says the card is about a screenshot, which is what the foot says
	// and what a failure tells the user to try again with.
	image bool
	// qr says the card is about a QR code. The capture is the same one the
	// screenshot tool uses, but the answer is text read on this machine
	// rather than a translation: there is no service and no network.
	qr      bool
	preview *ui.Bitmap
	// imageTaskID is non-zero only for a screenshot task. It lets a completed
	// background task update the card that is already showing that task without
	// reopening a card the user closed.
	imageTaskID uint64
	// source is what was translated; result is what came back.
	source string
	result translate.Result
	// failure is why the last attempt failed, in the words the window
	// shows. It is set only with translateFailed.
	failure string
	// pinned is whether the card stays where it is when the user clicks
	// elsewhere. It starts from the setting and the pin action flips it.
	pinned bool
	// readings is whether the annotation under the translation is open. It
	// starts closed: the translation is what the card is for, and the words
	// behind it are what a reader asks for.
	readings bool
	// annotation is the second request's answer: the words of the source and
	// how they are read. It is asked for when the annotation is opened, and
	// kept until the card is about something else.
	annotation annotationState
	// copied is which side was copied, and when: the action says so for a
	// moment. "source" or "target", empty when nothing was copied.
	copied   string
	copiedAt time.Time
	// dragging is whether the pointer is moving the card by its foot,
	// dragFrom is where the window was when the press started, and
	// dragPointer is where the pointer was then. The card is moved by the
	// distance the pointer has travelled since that press, so it stays
	// under the point the user grabbed rather than jumping by the pointer's
	// position.
	dragging    bool
	dragFrom    mygo.Rectangle
	dragPointer mygo.Point
}

// annotationStatus is where the request for the words of the source is.
type annotationStatus uint8

const (
	// annotationIdle is the state the card is in until someone opens the
	// annotation: nothing has been asked for, and nothing has been paid for.
	annotationIdle annotationStatus = iota
	annotationLoading
	annotationReady
	annotationFailed
)

// annotationState is the answer to that request: the words of the source, and
// how each one is read.
type annotationState struct {
	status   annotationStatus
	segments []domain.Segment
	failure  string
}

// startTranslation is what the shortcut and the settings window's test both
// reach: it shows the card and asks the use case for a translation.
//
// It returns at once. The request runs on a goroutine, and the answer comes
// back through Window.Update, because the caller is the main thread with the
// whole application behind it.
//
// The card appears before the keyboard is taken, and the text to translate is
// read in between: what has to be read is what the user selected in the
// application they were working in, and an application that has just lost the
// keyboard cannot be asked what is selected in it (§2.2 of the tool design). The
// wait is the read — the platform's own call, bounded by the selection device —
// not the translation: the card has the keyboard while the request is in
// flight.
func (s *State) startTranslation(at mygo.Point) {
	if s.translate == nil {
		return
	}
	startedAt := time.Now()
	slog.Info("translation requested", "event", "text_translation_requested", "source", "shortcut")
	// A second press while the card is open is a second translation: what
	// the user copied may have changed, and the card is about what they
	// just selected.
	settings, err := s.translate.Settings()
	if err != nil {
		// The card can only say "看不出来"; the log says which read failed.
		slog.Warn("the translation settings could not be read", "error", err)
		s.showTranslation(at, translationState{status: translateFailed, failure: friendlyError(err)})
		return
	}
	s.translation.pinned = settings.Pinned
	s.showTranslationInactive(at, translationState{status: translateLoading, pinned: settings.Pinned})
	window := s.translateWindow

	service := s.translate
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), translationTimeout)
		defer cancel()
		text, err := service.Source(ctx)
		// The read is done, and the card may take the keyboard. It takes it
		// now rather than when the answer arrives so that it is a card the
		// user can dismiss while they wait — Escape and the close the
		// settings ask for are both about having the keyboard.
		s.onMain(func() { s.focusTranslation(window) })
		var result translate.Result
		if err == nil {
			result, err = service.Translate(ctx, translate.Request{Text: text})
		}
		s.onMain(func() {
			switch {
			case err == nil:
				slog.Info("translation completed", "event", "text_translation_completed", "provider", result.ProviderID, "sourceChars", len([]rune(text)), "duration", time.Since(startedAt))
				// What the source is drawn from is the segments when there
				// are any: the window shows the pieces the translator
				// annotated, so that a reading sits over the word it
				// belongs to.
				s.translation = translationState{
					status:   translateReady,
					source:   result.SourceText,
					result:   result,
					pinned:   s.translation.pinned,
					copied:   s.translation.copied,
					copiedAt: s.translation.copiedAt,
				}
			default:
				// The card shows the user a shortened reason; the error
				// itself is the one that says which way it failed.
				slog.Warn("the translation failed", "event", "text_translation_failed", "sourceChars", len([]rune(text)), "duration", time.Since(startedAt), "error", err)
				s.translation = translationState{
					status:  translationFailureStatus(err),
					result:  translate.Result{},
					failure: friendlyError(err),
					pinned:  s.translation.pinned,
				}
			}
			// The card's content is not what the last frame measured: the
			// frames that measure it are owed. This is the answer arriving,
			// which is the case the card grows or shrinks for.
			s.cardChanged()
			s.fitTranslationWindow()
		})
	}()
}

// startScreenshotTranslation starts native region selection away from the UI
// thread, then sends only the transient PNG to the selected image translator.
// Both the flyout button and the global shortcut enter through this function.
func (s *State) startScreenshotTranslation() {
	requestedAt := s.cursorPoint()
	if s.translate == nil || s.screenshot == nil {
		return
	}
	startedAt := time.Now()
	slog.Info("screenshot translation requested", "event", "screenshot_translation_requested")
	settings, err := s.translate.Settings()
	if err != nil {
		s.showTranslation(requestedAt, translationState{status: translateFailed, failure: friendlyError(err)})
		return
	}
	service, capture := s.translate, s.screenshot
	go func() {
		captureCtx, cancelCapture := context.WithTimeout(context.Background(), screenshotCaptureTimeout)
		image, err := capture.Capture(captureCtx)
		cancelCapture()
		var preview *ui.Bitmap
		if err == nil {
			slog.Info("screenshot captured", "event", "screenshot_capture_completed", "imageBytes", len(image), "duration", time.Since(startedAt))
			preview, err = ui.DecodeBitmap(image)
		}
		if err == nil {
			// The user has finished choosing a region. Only now does the card
			// appear. Its anchor is read on the main thread after the selection,
			// rather than at the time the button or shortcut launched the picker.
			// The task keeps running after that card is closed.
			s.onMain(func() {
				anchor := s.cursorPoint()
				id, ok := s.startImageTranslationTask(service, image, preview, settings.Pinned)
				if !ok {
					s.showTranslation(anchor, translationState{status: translateFailed, image: true, preview: preview, failure: "已有两个图片翻译任务正在进行，请稍后再试。", pinned: settings.Pinned})
					return
				}
				s.showImageTranslationTask(id, anchor)
			})
			return
		}
		// Esc, a click without a usable rectangle, and replacing an in-flight
		// picker are normal ways out of the app-owned selection UI. They are
		// not translation failures and should leave no card behind.
		if errors.Is(err, context.Canceled) {
			slog.Info("screenshot capture cancelled", "event", "screenshot_capture_cancelled", "duration", time.Since(startedAt))
			return
		}
		slog.Warn("screenshot capture failed", "event", "screenshot_capture_failed", "duration", time.Since(startedAt), "error", err)
		s.onMain(func() {
			s.showTranslation(requestedAt, translationState{status: translationFailureStatus(err), image: true, failure: friendlyError(err), pinned: settings.Pinned})
		})
	}()
}

// onMain runs a piece of work that changes the view state on the thread that
// owns it: through an existing window when there is one — that is what asks
// for the frame which shows the change — or through Application.WhenReady
// when all windows are closed. The latter is essential for screenshot tasks:
// their image can arrive after the tray flyout was dismissed, but creating a
// translation card still has to happen on the UI thread.
//
// A caller is a goroutine, so the update body may touch the state only inside
// this call.
func (s *State) onMain(update func()) {
	if window := s.translateWindow; window != nil {
		window.Update(update)
		return
	}
	if s.panel != nil {
		s.panel.Update(update)
		return
	}
	if s.settingsWindow != nil {
		s.settingsWindow.Update(update)
		return
	}
	if s.historyWindow != nil {
		s.historyWindow.Update(update)
		return
	}
	if s.app != nil {
		s.app.WhenReady(update)
		return
	}
	// A small state constructed by a unit test has no running application.
	// Running synchronously is then the only meaningful way to exercise the
	// state transition, and is never a production-thread escape hatch.
	update()
}

// translationTimeout bounds one translation end to end. The HTTP client has
// its own, shorter timeout; this is the ceiling for everything around it,
// including the clipboard read.
const translationTimeout = 20 * time.Second

const (
	// screenshotCaptureTimeout bounds an abandoned Boku region selector.
	screenshotCaptureTimeout = 45 * time.Second
	// screenshotTranslationTimeout is deliberately independent of capture.
	// Vision endpoints expose no task/progress API, so an open HTTP request
	// cannot prove whether a model is queued, reasoning, or stuck. Two minutes
	// is a user-cancellable upper bound for a background task, not a claim of
	// server-side progress.
	screenshotTranslationTimeout = 2 * time.Minute
)

// translationFailureStatus maps an error to what the card shows. The four
// answers the tool has are different things to the user, and each one is a
// different sentence: the switch is off, there is nowhere to send the text,
// there is nothing to translate, and something went wrong.
//
// The switch is checked first and on its own error, because it is the one
// answer that is not a problem: a user who turned the tool off asked for
// exactly what they got, and telling them a service is down would send them
// looking for a fault that is not there.
func translationFailureStatus(err error) translateStatus {
	switch {
	case errors.Is(err, domain.ErrTranslateDisabled):
		return translateOff
	case errors.Is(err, domain.ErrTranslateUnavailable):
		return translateUnconfigured
	case errors.Is(err, domain.ErrCredentialMissing):
		return translateUnconfigured
	case errors.Is(err, domain.ErrInvalidArgument):
		return translateNoText
	case errors.Is(err, domain.ErrScreenRecordingDenied):
		return translateNeedsScreenRecording
	default:
		return translateFailed
	}
}

// showTranslation puts the card where the pointer is, shows it and gives it the
// keyboard.
func (s *State) showTranslation(at mygo.Point, state translationState) {
	s.showTranslationInactive(at, state)
	s.focusTranslation(s.translateWindow)
}

// showTranslationInactive puts the card where the pointer is and shows it
// without taking the keyboard.
//
// It is what the shortcut asks for, and the card keeps the keyboard out of it
// only until the text to translate has been read: the read is what the
// application the user was working in still answers, and this call is the one
// that would stop it answering (§2.2 of the tool design). focusTranslation is the
// other half, and the two are always a pair — the card is on screen the moment
// the shortcut is pressed, the keyboard follows the read.
func (s *State) showTranslationInactive(at mygo.Point, state translationState) {
	s.translation = state
	// Whatever the card drew last is not what it draws now, so the frames
	// that measure the change are owed.
	s.cardChanged()
	window := s.translateWindow
	if window == nil {
		window = s.newCardWindow(
			func() {
				if s.translateWindow == window {
					s.translateWindow = nil
					s.translationVisible = false
				}
				s.cancelTranslationClose()
			},
			func() {
				// The keyboard went elsewhere: whatever the user was
				// dragging with it, and the close it may have started.
				s.endCardDrag()
				s.scheduleTranslationClose(window)
			},
			func() { s.cancelTranslationClose() },
		)
		s.translateWindow = window
	}
	s.placeTranslation(window, at)
	s.fitTranslationWindow()
	window.ShowInactive()
	s.translationVisible = true
}

// focusTranslation gives the card the keyboard: it is the second half of
// showTranslationInactive, called when the text the card is about has been
// read.
//
// The card a caller read for may be gone by then — closed while the read ran,
// or replaced by a newer one — and a request that is over must not put an old
// card back in front of the user.
func (s *State) focusTranslation(window cardWindow) {
	if window == nil || window != s.translateWindow {
		return
	}
	window.Focus()
}

// cardWindow is the translation card as the state machine drives it: show it,
// take the keyboard, hide it, and hear about the three things that happen to
// it.
//
// It is an interface rather than the framework's window so that what the card
// does — its states, its pin, its delayed close — can be exercised where no
// window can exist. Creating one before App.Run does not fail; it waits for
// the event loop, which is a test that never returns.
//
// Geometry is the *content* rectangle, not the window's outer one: what the
// card measures is what it draws in, and a window that carries a frame draws
// in something smaller than what it was given. On Windows a window that can be
// resized keeps an 8-point frame even with no title bar — measured with
// `DisableResize: false`, a window set to 478×222 draws in 462×214 — and a
// card sized by its outer rectangle would have its last eight points cut off,
// which is exactly where its foot is.
type cardWindow interface {
	// ShowInactive puts the window on screen without taking the keyboard,
	// and Focus gives it the keyboard. They are separate because the text
	// a translation is about is read out of the application the user is
	// working in, and the card must not take that application's keyboard
	// until the read is done (§2.2 of the tool design). The framework's own
	// Show is not part of this: it shows and focuses in one step, which is
	// the pair this is meant to keep apart.
	ShowInactive()
	Focus()
	Hide()
	Update(func())
	SetContentBounds(mygo.Rectangle)
	ContentBounds() mygo.Rectangle
	OnClosed(func()) func()
	OnBlur(func()) func()
	OnFocus(func()) func()
}

// newCardWindow builds the real card. A state that was given a factory uses
// that instead, which is what a test does.
func (s *State) newCardWindow(onClosed, onBlur, onFocus func()) cardWindow {
	if s.cardWindows != nil {
		return s.cardWindows(onClosed, onBlur, onFocus)
	}
	window := mygo.NewWindow(mygo.WindowOptions{
		Title:     appName + " 划词翻译",
		Width:     translateWidth,
		Height:    translateHeight,
		MinWidth:  translateWidth,
		MinHeight: translateMinHeight,
		// The window is created hidden and shown by the state machine. A
		// window created without this is shown *and focused* by the
		// framework as it is built, which is the keyboard taken before the
		// selection has been read — the card would be shown by the very
		// call that makes it unreadable (§2.2 of the tool design).
		Hidden:    true,
		Frameless: true,
		// No resize: the card is exactly as tall as what is in it — measured
		// every frame — so a dragged edge would be undone immediately. What
		// it would leave behind is a frame: a resizable window keeps one even
		// without a title bar, and a frame is eight points of the card's
		// bottom that never get drawn.
		DisableResize: true,
		Transparent:   true,
		AlwaysOnTop:   true,
		SkipTaskbar:   true,
		// On macOS a Panel is a non-activating NSPanel: it floats on every
		// Space and over full-screen apps, and taking its keyboard does not
		// activate Boku, so the application the user was working in stays
		// active. A regular window would pull the foreground away when the
		// card takes the keyboard, which is exactly the wrong behaviour for a
		// card about the user's selection. Windows keeps the regular window:
		// its ShowInactive/Focus pair already has the wanted meaning there.
		Panel: runtime.GOOS == "darwin",
		// The platform's own window shadow, which is what the design's
		// shadow-xl is the design-system spelling of (`bg-white rounded-xl
		// border border-stone-200/80 shadow-xl`). It is deliberately not
		// drawn in the view: a shadow drawn here needs the window to be
		// larger than the card to have room, and that transparent margin
		// takes clicks — measured, a click thirty points outside the card
		// still lands in its window — so clicking beside the card would no
		// longer dismiss it. The window's own shadow is drawn outside the
		// window: nothing is taken from the card, and nothing from the
		// desktop's clicks. MyGo asks the platform for it with
		// `DwmExtendFrameIntoClientArea` and one point of frame, which its
		// own source calls "a frame the size of a pixel brings the shadow
		// back"; the drawn area stays exactly the card's (measured: outer
		// 478×150 and drawn 478×150).
		DisableShadow: false,
		Content:       ui.View(s.translateView),
	})
	window.OnClosed(onClosed)
	window.OnBlur(onBlur)
	window.OnFocus(onFocus)
	return window
}

// placeTranslation puts the card near the pointer: below and to the right of
// it, then flipped or slid back when there is no room, as a menu does.
func (s *State) placeTranslation(window cardWindow, at mygo.Point) {
	s.translationHeight = translateHeight
	window.SetContentBounds(cardBounds(at, s.screenNear(at), s.translationHeight))
}

// cardBounds is where the card goes, as arithmetic: a rectangle the size the
// content wants, next to the pointer and inside the room it has.
//
// It is a function of its arguments rather than of the screen, so that the
// flipping and the sliding can be tested without one.
//
// Everything here is in the points the window API takes, which is the same unit
// the design's sizes are written in. There is deliberately no scale factor:
// `SetContentBounds` and `ContentBounds` are DIP already, and multiplying by the
// display's scale — which is what this did until 2026-10-15 — asked for a window
// `scale` times too wide (478 points became 717 on a 150% display), because the
// scale was applied on the way in and the value compared on the way out was the
// unscaled one.
func cardBounds(at mygo.Point, work mygo.Rectangle, height int) mygo.Rectangle {
	x := at.X + translateGap
	y := at.Y + translateGap
	if x+translateWidth > work.X+work.Width {
		x = at.X - translateGap - translateWidth
	}
	if y+height > work.Y+work.Height {
		y = at.Y - translateGap - height
	}
	return mygo.Rectangle{
		X:      clampInt(x, work.X, max(work.X+work.Width-translateWidth, work.X)),
		Y:      clampInt(y, work.Y, max(work.Y+work.Height-height, work.Y)),
		Width:  translateWidth,
		Height: height,
	}
}

// screenNear is the room the card has to live in: every display's work area,
// united.
//
// It used to be the work area of the display the pointer is on, and that is
// where the other half of the 2026-10-15 bug was. MyGo reports each display's
// rectangle in points converted by that display's *own* scale, so two displays
// whose physical rectangles are merely side by side overlap once converted: on
// the machine this was reported from, the primary display's right third (its
// last 853 of 2560 points) lies inside the 150 % display's rectangle. A card
// placed there was clamped into that other display's work area — it jumped to
// its left edge and then did not move at all while the pointer kept travelling,
// which is the "position changes unnaturally" the user saw.
//
// The union answers the same thing on a single display, and on several it only
// means the card may sit across the seam — which is where the user put it.
func (s *State) screenNear(at mygo.Point) mygo.Rectangle {
	if s.cursorSet {
		// A state that was given a pointer in advance has no screen to ask —
		// the screen module needs a running application — so it answers with
		// a work area wide enough for the arithmetic to be exercised.
		return mygo.Rectangle{X: 0, Y: 0, Width: 1920, Height: 1080}
	}
	return workAreaOf(mygo.Screen.Displays())
}

// workAreaOf is every display's work area, united: the room the card may be
// placed in, whichever display the framework attributes the pointer to.
func workAreaOf(displays []mygo.Display) mygo.Rectangle {
	if len(displays) == 0 {
		return mygo.Rectangle{}
	}
	work := displays[0].WorkArea
	for _, display := range displays[1:] {
		area := display.WorkArea
		left, top := min(work.X, area.X), min(work.Y, area.Y)
		right := max(work.X+work.Width, area.X+area.Width)
		bottom := max(work.Y+work.Height, area.Y+area.Height)
		work = mygo.Rectangle{X: left, Y: top, Width: right - left, Height: bottom - top}
	}
	return work
}

// measureCard is how the card comes to be as tall as what is in it.
//
// A frame builds the card and reads back how tall it turned out — the
// framework lays a column out at its content height, and the card asks for
// nothing more — and the window is then sized to that, inside its bounds. A
// card taller than the tallest is cut off rather than scrolled: there is no
// scrolling container in a card whose height is the thing being measured.
//
// What a frame can read is the box the frame *before* it laid out: MyGo's
// Element.Bounds is the previous frame's rectangle, so the card is always
// measured as it was. That is what makes the frames below necessary rather
// than merely tidy. The answer arriving changes the card's content in frame
// one; frame one therefore measures the card as it was — the loading card,
// which is exactly the height the window already has — nothing resizes, and
// with nothing to show it the window never asks for another frame. The card
// would stay at its old height until something else moved it, which is what a
// click on it did.
//
// So the card asks for the frames its own measurement needs: one after its
// content changes, so that the frame which drew the change is measured, and
// one after the window has been given a new size, so that the card is laid
// out at it. Both are paid out of cardSettle, which is how many frames the
// card owes itself, and every request is made from inside a frame — so it
// schedules the frame after this one rather than being folded into this one.
func (s *State) measureCard(c *ui.Context, card ui.Element) {
	if s.cardMeasuring || s.translation.dragging {
		return
	}
	if s.translateWindow == nil {
		// The card is being drawn somewhere other than its window — a test's
		// frame, an offscreen render — and there is nothing to resize.
		return
	}
	if c.Vibrancy() {
		// A popover's material draws its own shape, and a card that changes
		// size under it is a card that flickers.
		return
	}
	box := card.Bounds()
	if box.H <= 0 {
		// The card was not in the frame before this one: there is nothing to
		// measure yet. It is in this one.
		s.nextCardFrame(c)
		return
	}
	work := s.screenNear(s.cursorPoint())
	// A window is a whole number of points and the card is not: rounding up
	// is what keeps the card inside the window, and never adds more than the
	// fraction it has to. Adding a point outright would leave a strip of
	// window under the foot — the card's own bottom edge with a hairline of
	// whatever is behind it.
	measured := int(math.Ceil(float64(box.H)))
	s.cardHeight = measured
	if s.fitCard(work, measured) {
		// The window has just been given a size: the next frame is the one
		// that lays the card out at it.
		s.nextCardFrame(c)
		return
	}
	s.nextCardFrame(c)
}

// nextCardFrame asks for one more frame while the card owes itself one.
func (s *State) nextCardFrame(c *ui.Context) {
	if s.cardSettle <= 0 {
		return
	}
	s.cardSettle--
	c.Invalidate()
}

// cardChanged says the card now holds what the last frame did not draw, so
// the frame that draws it has to be followed by one that measures it.
//
// Two frames are owed: the one that draws the change, and the one that lays
// the card out at the size that measurement asked for.
func (s *State) cardChanged() { s.cardSettle = 2 }

// fitCard sizes the window to what the card drew, keeping it where it is and
// inside the room it has. It reports whether the window was actually given a new
// size.
//
// Both sizes are in points, like everything else here: see cardBounds for why
// nothing is multiplied by a scale factor.
func (s *State) fitCard(work mygo.Rectangle, measured int) bool {
	window := s.translateWindow
	if window == nil || measured <= 0 {
		return false
	}
	height := clampInt(measured, translateMinHeight, translateMaxHeight)
	if height == s.translationHeight {
		return false
	}
	s.translationHeight = height
	bounds := window.ContentBounds()
	if bounds.Width == translateWidth && bounds.Height == height {
		return false
	}
	// The card keeps its top-left corner and grows downwards, unless that
	// would run it off the screen: then it slides up, as the menu it looks
	// like would.
	y := clampInt(bounds.Y, work.Y, max(work.Y+work.Height-height, work.Y))
	window.SetContentBounds(mygo.Rectangle{
		X:      clampInt(bounds.X, work.X, max(work.X+work.Width-translateWidth, work.X)),
		Y:      y,
		Width:  translateWidth,
		Height: height,
	})
	return true
}

// translationHeight is the room the card wants right now: what it measured,
// or what it starts at before the first frame.

// fitTranslationWindow sizes the card the way it was placed: the width is the
// design's, the height is what the card measured. It is what a card that has
// not drawn a frame yet is given — a new answer, a new pin — rather than a
// height chosen for the state.
func (s *State) fitTranslationWindow() {
	window := s.translateWindow
	if window == nil {
		return
	}
	work := s.screenNear(s.cursorPoint())
	s.fitCard(work, s.translationHeight)
}

// scheduleTranslationClose closes the card once it has been ignored for as
// long as the setting says.
//
// The wait is why the card has a delay at all: a translation is read, and a
// user who clicks the page behind it to read on may still want the card. A
// pinned card never closes itself, and neither does one that has been pinned
// while the timer was running.
func (s *State) scheduleTranslationClose(window cardWindow) {
	if s.translation.pinned {
		return
	}
	delay := translate.DefaultCloseDelay
	if s.translate != nil {
		if settings, err := s.translate.Settings(); err == nil {
			delay = settings.CloseDelayOr()
		}
	}
	s.cancelTranslationClose()
	// The timer only asks for the close; the state it reads is touched on
	// the main thread, inside Update, like every other piece of view state.
	timer := time.AfterFunc(delay, func() {
		window.Update(func() {
			if s.translateWindow != window || s.translation.pinned {
				return
			}
			s.closeTranslation()
		})
	})
	s.translateCloseTimer = timer
}

// cancelTranslationClose forgets a close that was asked for: the user came
// back to the card, or dismissed it themselves.
func (s *State) cancelTranslationClose() {
	if s.translateCloseTimer != nil {
		s.translateCloseTimer.Stop()
		s.translateCloseTimer = nil
	}
}

// closeTranslation hides the card. It is hidden rather than destroyed: the
// next press reuses it, and a window that was never created costs nothing.
//
// Hiding also ends whatever drag was in progress. A card that is off screen
// and still thinks the pointer is holding it would jump the next time the
// pointer crossed where it used to be.
func (s *State) closeTranslation() {
	s.cancelTranslationClose()
	s.endCardDrag()
	s.translationVisible = false
	if s.translateWindow != nil {
		s.translateWindow.Hide()
	}
}

// toggleTranslationPin flips whether the card closes itself, and asks for the
// frame that draws the pin in its new state.
func (s *State) toggleTranslationPin() {
	s.translation.pinned = !s.translation.pinned
	slog.Info("translation card pin changed", "event", "translation_card_pin_changed", "pinned", s.translation.pinned, "image", s.translation.image)
	if s.translation.imageTaskID != 0 {
		if task := s.imageTranslationTask(s.translation.imageTaskID); task != nil {
			task.pinned = s.translation.pinned
		}
	}
	if s.translation.pinned {
		s.cancelTranslationClose()
	}
	if s.translateWindow != nil {
		s.cardChanged()
		s.translateWindow.Update(func() {})
	}
}

// toggleReadings opens and closes the annotation under the translation, and
// asks for the frames that draw the card at its new height: what it opens is
// taller than what it replaces, so the frame that draws it has to be measured
// and followed by one that sizes the window to it.
//
// Opening it the first time is what asks the translator for the words: the
// translation did not wait for them, and neither does the card.
func (s *State) toggleReadings() {
	s.translation.readings = !s.translation.readings
	if s.translation.readings && s.translation.annotation.status == annotationIdle {
		s.requestAnnotation()
		return
	}
	if s.translateWindow != nil {
		s.cardChanged()
		s.translateWindow.Update(func() {})
	}
}

// requestAnnotation asks the translator for the words of the source and how
// they are read.
//
// It is a request of its own, made when the user opens the annotation, and it
// is why the annotation costs nothing until then: the translation goes out
// asking for a paragraph, and the pieces behind it — which are the slow part —
// are asked for by the person who wants them. It runs on a goroutine, like the
// translation, and comes back through onMain.
func (s *State) requestAnnotation() {
	service := s.translate
	text := s.translation.source
	if service == nil || strings.TrimSpace(text) == "" {
		return
	}
	s.translation.annotation = annotationState{status: annotationLoading}
	if s.translateWindow != nil {
		s.cardChanged()
		s.translateWindow.Update(func() {})
	}

	request := translate.Request{Text: text, Target: s.translation.result.TargetLang, Annotate: true}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), translationTimeout)
		defer cancel()
		result, err := service.Translate(ctx, request)
		s.onMain(func() {
			if s.translation.source != text {
				// The card is about something else now: this answer is for a
				// translation that is gone.
				return
			}
			switch {
			case err == nil:
				s.translation.annotation = annotationState{
					status:   annotationReady,
					segments: result.Segments,
					failure:  result.Note,
				}
			default:
				// The readings are a second request, and it can fail on its
				// own after the translation itself worked.
				slog.Warn("the readings could not be fetched", "error", err)
				s.translation.annotation = annotationState{
					status:  annotationFailed,
					failure: friendlyError(err),
				}
			}
			// The annotation changed how tall the card is: the frames that
			// measure it are owed, as they are for a translation.
			s.cardChanged()
			s.fitTranslationWindow()
		})
	}()
}

// readingsAvailable is whether the card offers the annotation at all: a source
// in a language whose words carry readings or loanword glosses, under a
// setting that asks for one of the two.
//
// It is what decides whether the card shows the row that opens them: a control
// that opens nothing is worse than no control.
func (s *State) readingsAvailable() bool {
	if s.translation.status != translateReady ||
		s.translation.result.SourceLang != domain.LangJapanese ||
		strings.TrimSpace(s.translation.source) == "" {
		return false
	}
	settings, err := s.translateSettings()
	if err != nil {
		return true
	}
	return settings.Furigana || settings.LoanwordGloss
}

// annotatedWords is what the open annotation shows: one entry per word that
// carries something — a reading for the ones written with kanji, the word it
// came from for the loanwords — in the order they appear.
//
// It is not the sentence. A reader who opens this wants the words they cannot
// read, and the sentence is already above them; what a piece in kana has to
// show is nothing at all.
//
// A word the source uses twice is one entry, not two: this is a list of the
// words the sentence is made of, and the same word twice in it tells a reader
// nothing they did not already know. A word that says two different things —
// the same kanji read two ways in one text — is two entries, because those are
// two words that happen to be written alike.
func (s *State) annotatedWords() []domain.Segment {
	showReading, showGloss := true, true
	if settings, err := s.translateSettings(); err == nil {
		showReading, showGloss = settings.Furigana, settings.LoanwordGloss
	}
	segments := s.translation.annotation.segments
	words := make([]domain.Segment, 0, len(segments))
	seen := make(map[string]struct{}, len(segments))
	for _, segment := range segments {
		note, _ := annotationNote(segment, showReading, showGloss)
		if note == "" {
			continue
		}
		key := segment.Text + "\x00" + note
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		words = append(words, segment)
	}
	return words
}

// annotationNote is what a word has to say about itself, and whether it is a
// loanword's own word — which is the one note the design writes in the brand
// green.
//
// It is one function because the chip and the list have to agree: what the
// list counts as a word's note is what the chip puts over it, and what it
// dedupes on is that same thing.
func annotationNote(segment domain.Segment, showReading, showGloss bool) (string, bool) {
	switch {
	case showGloss && segment.Gloss != "":
		return segment.Gloss, true
	case showReading && segment.Reading != "":
		return segment.Reading, false
	}
	return "", false
}

// dragCard is the foot of the card, as the handle that moves it.
//
// The pointer events come from the framework: the element under the press
// takes the moves until the button comes up, so a press that began on the
// foot keeps moving the card even when the pointer leaves it — which is what
// makes a drag feel like a drag rather than like a chase.
//
// What moves the card is the pointer's travel since the press, not the
// pointer's position: the press remembers where the pointer was and where the
// window was, and every move puts the window that far from where it started.
// An event's own X and Y are where the pointer is inside a box that moves
// with the card, so adding them — or adding their differences — makes the
// card jump to the pointer and then chase it; the distance travelled is the
// one number that does not depend on the card having already moved.
//
// A move is only a drag while the button is held: the foot sees the pointer
// pass over it without a press too, and a card on its way out after losing
// the keyboard must not be moved by the pointer merely crossing it.
//
// A pinned card is dragged like any other: the pin is about the card staying
// open when the keyboard goes elsewhere, not about it staying where it was put
// — a card the user has pinned is one they are working with, and moving it out
// of the way is part of that.
func (s *State) dragCard(ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputPointerDown:
		window := s.translateWindow
		if window == nil {
			return false
		}
		s.translation.dragging = true
		s.translation.dragFrom = window.ContentBounds()
		s.translation.dragPointer = s.cursorPoint()
		s.cancelTranslationClose()
		return true
	case ui.InputPointerMove:
		window := s.translateWindow
		if window == nil || !s.translation.dragging || ev.Button != 0 {
			return false
		}
		at := s.cursorPoint()
		work := s.screenNear(at)
		bounds := s.translation.dragFrom
		bounds.X = clampInt(
			bounds.X+int(at.X-s.translation.dragPointer.X),
			work.X, max(work.X+work.Width-bounds.Width, work.X),
		)
		bounds.Y = clampInt(
			bounds.Y+int(at.Y-s.translation.dragPointer.Y),
			work.Y, max(work.Y+work.Height-bounds.Height, work.Y),
		)
		window.SetContentBounds(bounds)
		return true
	case ui.InputPointerUp:
		if !s.translation.dragging {
			return false
		}
		s.translation.dragging = false
		return true
	}
	return false
}

// endCardDrag forgets a drag that was never released.
//
// A press whose release never arrives — the card was hidden, or the keyboard
// went somewhere else and the platform never told the foot about it — would
// otherwise leave the card following the pointer for as long as it lives.
func (s *State) endCardDrag() {
	s.translation.dragging = false
}

// copyTranslation puts one side of the card back on the clipboard.
//
// It goes through the clipboard tool rather than the platform directly, so
// that the write is suppressed from the history exactly as any other write
// the application makes is: the poll must not read the translation back and
// record it as a new copy. The entry itself is the text that was copied, with
// the hash the clipboard tool computes for it, so a translation the user
// copies is an ordinary clipboard entry like any other.
func (s *State) copyTranslation(which string) {
	if s.clipboard == nil {
		return
	}
	value := s.translation.source
	if which == "target" {
		value = s.translation.result.TranslatedText
	}
	if strings.TrimSpace(value) == "" {
		return
	}
	slog.Info("translation copied", "event", "translation_copied", "part", which, "image", s.translation.image, "chars", len([]rune(value)))
	s.translation.copied = which
	s.translation.copiedAt = time.Now()
	window := s.translateWindow
	if window != nil {
		// The mark that says what was copied takes the room of an action:
		// that is a change the card has to measure again.
		s.cardChanged()
		window.Update(func() {})
	}
	entry := domain.ClipboardEntry{
		ID:          "translate-" + which,
		Kind:        domain.ClipKindText,
		Content:     value,
		Display:     value,
		ContentHash: kit.ClipContentHash(value),
		CopiedAt:    time.Now().UTC(),
	}
	// The write runs off the main thread, and the mark is taken back when
	// it is done with: a copy is a platform call, and the frame the click
	// was in must not wait for it.
	s.clipboard.CopyAsync(entry, func(err error) {
		if err != nil {
			// A copy the platform refused is invisible in the card: the
			// mark goes back and nothing is said.
			slog.Warn("the translation could not be copied to the clipboard", "error", err)
		}
		if window == nil {
			return
		}
		time.AfterFunc(copiedMarkDuration, func() {
			window.Update(func() {
				if s.translation.copied == which && time.Since(s.translation.copiedAt) >= copiedMarkDuration {
					s.translation.copied = ""
					// The mark is gone: the card is a little different
					// again.
					s.cardChanged()
				}
			})
		})
	})
}

// copiedLabel is what a copy action says while it is saying it.
func (s *State) copiedLabel(which string) bool {
	return s.translation.copied == which && time.Since(s.translation.copiedAt) < 2*copiedMarkDuration
}

// cursorPoint is where the pointer is, in screen coordinates, which is where
// the card goes. It is read on the main thread, where the shortcut callback
// already runs.
//
// A test has no pointer and no running application — asking the platform for
// one before App.Run is a programming error — so a point may be given in
// advance, and the platform is asked only when none was.
func (s *State) cursorPoint() mygo.Point {
	if s.cursorSet {
		return s.cursor
	}
	point := mygo.Screen.CursorScreenPoint()
	if point.X == 0 && point.Y == 0 {
		// A platform that does not answer gives the middle of the primary
		// screen rather than the top-left corner, which is where a card
		// nobody can see would go.
		display := mygo.Screen.PrimaryDisplay()
		return mygo.Point{
			X: display.WorkArea.X + display.WorkArea.Width/2,
			Y: display.WorkArea.Y + display.WorkArea.Height/2,
		}
	}
	return point
}

// clampInt keeps a value inside a range, whichever end it fell off.
func clampInt(value, low, high int) int {
	if high < low {
		high = low
	}
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
