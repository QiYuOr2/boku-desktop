package ui

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"strings"
	"sync"
	"testing"
	"time"

	"boku/internal/domain"
	"boku/internal/kit"
)

// testScreenshot is the platform's region picker as a test drives it: it
// answers with the PNG the test made, or with the error it was given.
type testScreenshot struct {
	png []byte
	err error
	// asked, when set, is signalled once the picker was opened.
	asked chan struct{}
	once  sync.Once
}

func (d *testScreenshot) Capture(context.Context) ([]byte, error) {
	d.once.Do(func() {
		if d.asked != nil {
			close(d.asked)
		}
	})
	return d.png, d.err
}

// qrPNG is a readable minimal PNG: the card decodes the capture for its
// preview before showing it, so a fake capture has to be a real image.
func qrPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode the fake screenshot: %v", err)
	}
	return buf.Bytes()
}

// qrCard is the test card that also says when it appeared. The QR flow
// shows its card only after the capture, and a test has no event loop to
// wait for it: the channel is the synchronisation, and it also makes what
// the state wrote before showing visible to the test.
type qrCard struct {
	*testCard
	shown chan struct{}
	once  sync.Once
}

func (c *qrCard) ShowInactive() {
	c.testCard.ShowInactive()
	c.once.Do(func() { close(c.shown) })
}

// qrState is the card state a QR test drives. It has no running
// application, so onMain runs its update inline.
func qrState(t *testing.T) (*State, *testTranslateFixture, *qrCard) {
	t.Helper()
	state, translate, base := translateState(t)
	card := &qrCard{testCard: base, shown: make(chan struct{})}
	state.cardWindows = func(onClosed, onBlur, onFocus func()) CardWindow {
		base.onClosed, base.onBlur, base.onFocus = onClosed, onBlur, onFocus
		return card
	}

	return state, translate, card
}

// waitQRShown waits for the card, after which the state it was given is the
// state the test should read.
func waitQRShown(t *testing.T, card *qrCard) {
	t.Helper()
	select {
	case <-card.shown:
	case <-time.After(3 * time.Second):
		t.Fatal("the QR card was not shown")
	}
}

func TestQRRecognitionShowsTheTextInTheTranslationCard(t *testing.T) {
	state, _, card := qrState(t)
	capture := qrPNG(t)
	text := "https://example.com/二维码?x=1"
	state.screenshot = &testScreenshot{png: capture}
	state.qrDecode = func(data []byte) (string, error) {
		if !bytes.Equal(data, capture) {
			t.Errorf("decoded %d bytes, want the captured PNG", len(data))
		}
		return text, nil
	}

	state.startQRRecognition()
	waitQRShown(t, card)

	if got := state.translation.source; got != text {
		t.Fatalf("source = %q, want %q", got, text)
	}
	if !state.translation.qr {
		t.Fatal("the card is not marked as a QR result")
	}
	if state.translation.image {
		t.Fatal("a QR result was marked as an image translation")
	}
	if state.translation.preview == nil {
		t.Fatal("the card has no preview of what was captured")
	}
	if !card.visible() {
		t.Fatal("the card was not shown")
	}
}

func TestQRRecognitionSaysWhenThereIsNoCode(t *testing.T) {
	state, _, card := qrState(t)
	state.screenshot = &testScreenshot{png: qrPNG(t)}
	state.qrDecode = func([]byte) (string, error) {
		return "", kit.ErrQRCodeNotFound
	}

	state.startQRRecognition()
	waitQRShown(t, card)

	if !state.translation.qr {
		t.Fatal("the failed card is not marked as a QR result")
	}
	if state.translation.image {
		t.Fatal("a QR failure was marked as an image translation")
	}
	if !strings.Contains(state.translation.failure, "没有识别到二维码") {
		t.Fatalf("failure = %q, want a no-QR sentence", state.translation.failure)
	}
	if state.translation.preview == nil {
		t.Fatal("the failed card lost the picture it read")
	}
	if !card.visible() {
		t.Fatal("the card was not shown")
	}
}

func TestQRRecognitionSaysWhenTheCodeHasNoText(t *testing.T) {
	state, _, card := qrState(t)
	state.screenshot = &testScreenshot{png: qrPNG(t)}
	state.qrDecode = func([]byte) (string, error) {
		return "  ", nil
	}

	state.startQRRecognition()
	waitQRShown(t, card)

	if !strings.Contains(state.translation.failure, "没有文本") {
		t.Fatalf("failure = %q, want a no-text sentence", state.translation.failure)
	}
}

func TestQRRecognitionSaysWhenTheScreenshotCannotBeRead(t *testing.T) {
	state, _, card := qrState(t)
	state.screenshot = &testScreenshot{png: []byte("not a png")}
	state.qrDecode = func([]byte) (string, error) {
		return "", kit.ErrInvalidQRImage
	}

	state.startQRRecognition()
	waitQRShown(t, card)

	if !strings.Contains(state.translation.failure, "没能读取") {
		t.Fatalf("failure = %q, want a read-failure sentence", state.translation.failure)
	}
}

func TestQRRecognitionLeavesNoCardWhenTheSelectionIsCancelled(t *testing.T) {
	state, _, card := qrState(t)
	asked := make(chan struct{})
	state.screenshot = &testScreenshot{err: context.Canceled, asked: asked}

	state.startQRRecognition()
	select {
	case <-asked:
	case <-time.After(2 * time.Second):
		t.Fatal("the region picker was never opened")
	}
	// The picker's normal ways out must leave no card, exactly like the
	// screenshot translation's.
	time.Sleep(20 * time.Millisecond)
	if card.visible() {
		t.Fatal("cancelling the selection left a card behind")
	}
}

func TestQRRecognitionDoesNotNeedATranslationService(t *testing.T) {
	state, _, card := qrState(t)
	state.translate = nil
	state.screenshot = &testScreenshot{png: qrPNG(t)}
	state.qrDecode = func([]byte) (string, error) {
		return "扫码得到的文本", nil
	}

	state.startQRRecognition()
	waitQRShown(t, card)

	if state.translation.source != "扫码得到的文本" || !card.visible() {
		t.Fatalf("source = %q, visible = %v; want the decoded text on screen", state.translation.source, card.visible())
	}
}

func TestQRRecognitionSaysWhenThePlatformCannotCapture(t *testing.T) {
	state, _, card := qrState(t)
	state.screenshot = &testScreenshot{err: fmt.Errorf("%w: no screen reader", domain.ErrImageTranslateUnavailable)}
	state.qrDecode = func([]byte) (string, error) {
		t.Error("the decoder was asked for an image that was never captured")
		return "", nil
	}

	state.startQRRecognition()
	waitQRShown(t, card)

	if !state.translation.qr || !strings.Contains(state.translation.failure, "无法截取屏幕") {
		t.Fatalf("failure = %q, want an unsupported-capture sentence", state.translation.failure)
	}
}

func TestQRRecognitionSaysWhenTheSelectionTimesOut(t *testing.T) {
	state, _, card := qrState(t)
	state.screenshot = &testScreenshot{err: context.DeadlineExceeded}
	state.qrDecode = func([]byte) (string, error) {
		t.Error("the decoder was asked for an image that was never captured")
		return "", nil
	}

	state.startQRRecognition()
	waitQRShown(t, card)

	if !state.translation.qr || !strings.Contains(state.translation.failure, "超时") {
		t.Fatalf("failure = %q, want a timeout sentence", state.translation.failure)
	}
}
