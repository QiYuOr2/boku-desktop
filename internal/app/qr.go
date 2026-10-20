package app

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	"boku/internal/kit"
)

// startQRRecognition is the quick window's QR action: the same native region
// selection the screenshot translation uses, followed by a local read of the
// QR code in the chosen pixels, shown in the same card.
//
// Nothing leaves the machine. A QR code is often a login code, a payment
// code or a personal link, so the picture is never sent to a translator — it
// is decoded here and only the text is kept, in memory, for as long as the
// card shows it.
func (s *State) startQRRecognition() {
	if s.screenshot == nil {
		return
	}
	requestedAt := s.cursorPoint()
	startedAt := time.Now()
	slog.Info("QR recognition requested", "event", "qr_recognition_requested", "source", "quick_window")
	// The card's pin is the translation card's setting, since it is the same
	// card. A build with no translation tool still scans: the QR read does
	// not depend on a service.
	pinned := false
	if s.translate != nil {
		if settings, err := s.translate.Settings(); err == nil {
			pinned = settings.Pinned
		}
	}
	capture := s.screenshot
	decode := s.qrDecode
	if decode == nil {
		decode = kit.DecodeQRCodePNG
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), screenshotCaptureTimeout)
		image, err := capture.Capture(ctx)
		cancel()
		if err != nil {
			// Esc, a click without a usable rectangle, and replacing an
			// in-flight picker are normal ways out of the app-owned selection
			// UI. They leave no card behind.
			if errors.Is(err, context.Canceled) {
				slog.Info("QR recognition capture cancelled", "event", "qr_recognition_cancelled", "duration", time.Since(startedAt))
				return
			}
			slog.Warn("QR recognition capture failed", "event", "qr_recognition_failed", "duration", time.Since(startedAt), "error", err)
			s.onMain(func() {
				s.showTranslation(requestedAt, translationState{
					status:  translationFailureStatus(err),
					qr:      true,
					failure: qrFailure(err),
					pinned:  pinned,
				})
			})
			return
		}
		slog.Info("QR recognition captured", "event", "qr_captured", "imageBytes", len(image), "duration", time.Since(startedAt))
		// The preview is for the user's eyes; a screen that cannot be drawn
		// as a preview must not cost them the text, so its error is ignored
		// rather than treated as a failed read.
		preview, _ := ui.DecodeBitmap(image)
		text, decodeErr := decode(image)
		s.onMain(func() {
			// The selection took time, so the card is anchored where the
			// pointer is now, like the screenshot translation's.
			anchor := s.cursorPoint()
			if decodeErr != nil || strings.TrimSpace(text) == "" {
				failure := "没有识别到二维码。"
				switch {
				case decodeErr != nil && !errors.Is(decodeErr, kit.ErrQRCodeNotFound):
					slog.Warn("QR code could not be read", "event", "qr_recognition_failed", "duration", time.Since(startedAt), "error", decodeErr)
					failure = "没能读取这张截图。"
				case decodeErr != nil:
					slog.Info("QR code not found", "event", "qr_not_found", "duration", time.Since(startedAt))
				default:
					slog.Info("QR code holds no text", "event", "qr_empty", "duration", time.Since(startedAt))
					failure = "这个二维码里没有文本。"
				}
				s.showTranslation(anchor, translationState{
					status:  translateFailed,
					qr:      true,
					preview: preview,
					failure: failure,
					pinned:  pinned,
				})
				return
			}
			// Only the length is logged. What the code says is the user's
			// data, and the card is the only place it belongs.
			slog.Info("QR code decoded", "event", "qr_recognition_completed", "chars", len([]rune(text)), "duration", time.Since(startedAt))
			s.showTranslation(anchor, translationState{
				status:  translateQRReady,
				qr:      true,
				preview: preview,
				source:  text,
				pinned:  pinned,
			})
		})
	}()
}

// qrFailure says what a failed capture means in the card's own words. The
// shared translator errors already have user-facing sentences; the one case
// that does not is a build whose platform cannot read the screen.
func qrFailure(err error) string {
	switch {
	case errors.Is(err, domain.ErrImageTranslateUnavailable):
		return "这台电脑还无法截取屏幕。"
	case errors.Is(err, context.DeadlineExceeded):
		return "选区等待超时，请重试。"
	default:
		return friendlyError(err)
	}
}
