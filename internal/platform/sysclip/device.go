// Package sysclip reads and writes the system clipboard through MyGo.
//
// Every MyGo clipboard call touches the platform, which owns the clipboard
// from the main thread, and MyGo's own calls wait for that thread. This
// package therefore runs them through a dispatch function the assembly
// provides, so a poll running on a goroutine never blocks the event loop and
// a closed window cannot hang the poll. A dispatch that does not answer in
// time is dropped: there is another poll in a moment, and one reading is
// worth less than a stuck application.
//
// What it does with an answer is a pure function — formats, text, markup and
// file paths in, a domain.ClipContent out — which is tested on its own, since
// the platform half cannot run in a test process without a session.
package sysclip

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"

	"boku/internal/domain"
	"boku/internal/kit"
)

// DefaultTimeout bounds one round trip to the main thread. A clipboard read
// that has not happened within it is one this application does not need:
// there will be another poll, and a close should not wait on a clipboard.
//
// It is generous because one of those reads is a bitmap: converting a
// screenshot from a Windows bitmap to PNG costs tens of milliseconds, and a
// reading dropped for being slow is an image recorded with no bytes in it.
const DefaultTimeout = 2 * time.Second

// pictureFresh is how long a picture this device read is handed out again
// rather than read once more.
//
// A clipboard holds one value at a time and a picture does not change while it
// sits there, so a picture that is still offered a moment after it was read is
// the picture that was read. The window is what bounds how long that is
// assumed: it covers several poll intervals, so a screenshot left on the
// clipboard is read once rather than on every poll, and it is short enough
// that a picture the user copied in the meantime is never handed out as the
// old one for long.
const pictureFresh = 5 * time.Second

// Dispatcher runs fn on the thread the platform requires and waits for it,
// returning false when there is no such thread any more (the application is
// quitting). It is what makes a poll off the main thread safe.
type Dispatcher func(fn func()) bool

// Device is the platform clipboard.
//
// It is used by one reader at a time, which is what the poll loop is.
type Device struct {
	dispatch Dispatcher
	timeout  time.Duration
	// now stamps a reading. It is a field so that a test can pin it.
	now func() time.Time

	// pictureStamp is when the picture below was read, and picture is that
	// picture as the platform handed it over.
	//
	// Together they keep the poll from re-reading a picture that is still
	// there. Looking at a picture is the one expensive thing this device does:
	// the platform offers a bitmap as the device independent bitmap, and MyGo
	// hands it over as a PNG by decoding every pixel and encoding the whole
	// image — well over a hundred milliseconds for a screen's worth of pixels,
	// with the clipboard held shut for all of it, so that another
	// application's copy fails outright while it runs. Polling every 700ms
	// would pay that, and hold the clipboard, for as long as a screenshot sat
	// on the clipboard.
	//
	// It is kept rather than measured because a click on a picture in the
	// history puts it back, and this is the only moment it can be read — and
	// because the next poll can hand it out again instead of reading it once
	// more.
	picture      []byte
	pictureRef   string
	pictureStamp time.Time
}

// New returns a device reading through dispatch. A nil dispatcher runs the
// clipboard calls on the calling goroutine, which is what a test wants.
func New(dispatch Dispatcher) *Device {
	return &Device{
		dispatch: dispatch,
		timeout:  DefaultTimeout,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// Read returns what the clipboard holds now, or
// domain.ErrUnsupportedClipboardFormat when it holds nothing this
// application shows.
func (d *Device) Read(ctx context.Context) (domain.ClipContent, error) {
	var (
		content domain.ClipContent
		err     error
	)
	if !d.call(ctx, func() {
		content, err = d.snapshot()
	}) {
		return domain.ClipContent{}, domain.ErrUnsupportedClipboardFormat
	}
	if err != nil {
		return domain.ClipContent{}, err
	}
	content.CapturedAt = d.now()
	return content, nil
}

// WriteText puts text on the clipboard.
func (d *Device) WriteText(ctx context.Context, text string) error {
	// The platform call reports nothing, and the platform retries a
	// clipboard another process is holding.
	d.call(ctx, func() { mygo.Clipboard.WriteText(text) })
	return nil
}

// WriteFiles puts a file list on the clipboard, so that a click on a
// recorded copy of files pastes the files rather than the text of their
// paths.
func (d *Device) WriteFiles(ctx context.Context, paths []string) error {
	cleaned := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) != "" {
			cleaned = append(cleaned, path)
		}
	}
	if len(cleaned) == 0 {
		return domain.ErrUnsupportedClipboardFormat
	}
	var err error
	ran := d.call(ctx, func() {
		// A path the platform will not take — a relative one, or one that
		// no longer exists — is the platform's answer to give.
		err = mygo.Clipboard.WriteFiles(cleaned...)
	})
	if !ran {
		return domain.ErrUnsupportedClipboardFormat
	}
	if err != nil {
		return fmt.Errorf("%w: write the file list: %v", domain.ErrUnsupportedClipboardFormat, err)
	}
	return nil
}

// WriteImage puts a picture on the clipboard: a click on an image in the
// history, and the selection tool putting back what it borrowed while reading
// a selection.
//
// The bytes are what the platform handed over as PNG, and they go back the way
// they came.
func (d *Device) WriteImage(ctx context.Context, png []byte) error {
	if len(png) == 0 {
		return domain.ErrUnsupportedClipboardFormat
	}
	var err error
	ran := d.call(ctx, func() { err = mygo.Clipboard.WriteImage(png) })
	if !ran {
		return domain.ErrUnsupportedClipboardFormat
	}
	if err != nil {
		return fmt.Errorf("%w: write the image: %v", domain.ErrUnsupportedClipboardFormat, err)
	}
	return nil
}

// call runs fn through the dispatcher and reports whether it ran. A
// dispatcher that has not answered within the timeout is left behind: this
// returns, and the goroutine it was waiting for ends when the main thread
// gets to it.
func (d *Device) call(ctx context.Context, fn func()) bool {
	if d.dispatch == nil {
		fn()
		return true
	}
	done := make(chan bool, 1)
	go func() { done <- d.dispatch(fn) }()

	timeout := d.timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var canceled <-chan struct{}
	if ctx != nil {
		canceled = ctx.Done()
	}
	select {
	case ran := <-done:
		return ran
	case <-timer.C:
		return false
	case <-canceled:
		return false
	}
}

// snapshot is one reading of the platform clipboard. It runs on the thread
// that owns the clipboard.
//
// It reads what the clipboard advertises, once, and works from that answer.
//
// A picture is the one expensive flavour, and a picture that is still the one
// this device read a moment ago is handed out again rather than read once
// more; see pictureFresh. Everything else is read as it always was.
func (d *Device) snapshot() (domain.ClipContent, error) {
	formats := mygo.Clipboard.Formats()
	if len(formats) == 0 {
		// An empty clipboard, or a call the application's shutdown
		// dropped: either way there is nothing to show.
		d.forgetPicture()
		return domain.ClipContent{}, domain.ErrUnsupportedClipboardFormat
	}
	facts := platformFacts{
		formats:  formats,
		hasImage: hasImage(formats),
		imageErr: domain.ErrUnsupportedClipboardFormat,
	}
	if facts.hasImage {
		switch {
		case d.pictureStillThere():
			// The same picture is still on the clipboard: it is what was read
			// before, so it is what this reading found, and reading it again
			// would spend a screen's worth of decoding to learn nothing.
			facts.image = d.picture
			facts.imageRef = d.pictureRef
		default:
			png, err := mygo.Clipboard.ReadFormat(transfer.PNG)
			facts.image, facts.imageErr = png, err
			if err == nil && len(png) > 0 {
				d.rememberPicture(png, d.now())
			} else {
				// Nothing was read, so there is no picture to hand out again:
				// the next poll has to try afresh.
				d.forgetPicture()
			}
		}
	} else {
		d.forgetPicture()
	}
	// The text and the file list are read one flavour at a time, and a
	// flavour that is not there is not an error: a clipboard with a picture
	// on it is still a clipboard with nothing else on it.
	facts.text = mygo.Clipboard.ReadText()
	facts.html = mygo.Clipboard.ReadHTML()
	if paths, err := mygo.Clipboard.ReadFiles(); err == nil {
		facts.files = paths
	}
	return Convert(facts), nil
}

// hasFormat reports whether the clipboard advertises a format.
func hasFormat(formats []transfer.Format, want transfer.Format) bool {
	for _, format := range formats {
		if format == want {
			return true
		}
	}
	return false
}

// pictureStillThere reports whether the picture this device read is the one the
// clipboard is still offering.
//
// What makes that safe is that a clipboard holds one value at a time: the
// picture cannot change without the clipboard changing, and this application
// reads the clipboard more often than a person copies something. The window
// bounds the assumption, so a picture that somehow stayed past it is read
// again rather than handed out forever.
func (d *Device) pictureStillThere() bool {
	if len(d.picture) == 0 || d.pictureRef == "" {
		return false
	}
	if d.pictureStamp.IsZero() {
		return false
	}
	return d.now().Sub(d.pictureStamp) < pictureFresh
}

// rememberPicture keeps the picture that was just read, so that the next poll
// can hand it out instead of reading and encoding it again.
func (d *Device) rememberPicture(png []byte, at time.Time) {
	d.picture = png
	d.pictureRef = kit.ClipImageRef(png)
	d.pictureStamp = at
}

// forgetPicture drops the picture this device was holding.
//
// It is what a reading that found no picture, or could not read one, leaves
// behind: there is nothing to hand out again, and a picture kept from earlier
// would have the next reading claim to hold a picture that is not there.
func (d *Device) forgetPicture() {
	d.picture, d.pictureRef, d.pictureStamp = nil, "", time.Time{}
}

// platformFacts is what one reading of the platform clipboard found, as
// plain values: the part of a reading that can be tested without a session.
type platformFacts struct {
	formats  []transfer.Format
	text     string
	html     string
	files    []string
	hasImage bool
	// image is the bitmap on the clipboard as the platform hands it over, when
	// one was read; imageErr is why it is missing when the formats said there
	// was one. The bytes are kept because a picture has to be able to go back
	// — a click on it in the history, or the selection tool giving the
	// clipboard back — and this is the only moment it can be read.
	image    []byte
	imageErr error
	// imageRef is what the picture is called, when it was named by whoever
	// read it. A reading that carries the picture carries its name with it:
	// naming it here would hash the bytes a second time, and naming it here
	// after they were dropped would name it wrongly.
	imageRef string
}

// Convert turns one platform reading into what the application records.
//
// The order decides what the user meant: a file list is a file list even
// though the platform also puts the first path on the clipboard as text, and
// a bare bitmap is what is left when the platform has no name for what it
// holds.
func Convert(facts platformFacts) domain.ClipContent {
	switch {
	case len(facts.files) > 0:
		content := domain.ClipContent{
			Kind:       domain.ClipKindFile,
			Files:      facts.files,
			HasImage:   facts.hasImage,
			ImageBytes: len(facts.image),
			Image:      facts.image,
			ImageRef:   facts.imageRef,
		}
		if facts.hasImage && len(facts.image) == 0 && facts.imageErr != nil {
			content = content.WithObservation(
				"图片没能读出：" + facts.imageErr.Error() + "；已记下文件名")
		}
		return content
	case facts.hasImage:
		content := domain.ClipContent{
			Kind:       domain.ClipKindImage,
			HasImage:   true,
			ImageBytes: len(facts.image),
			Image:      facts.image,
			ImageRef:   facts.imageRef,
		}
		if len(facts.image) == 0 {
			reason := "剪贴板没有给出图片数据"
			if facts.imageErr != nil {
				reason = facts.imageErr.Error()
			}
			content = content.WithObservation("图片没能读出：" + reason)
		}
		return content
	case strings.TrimSpace(facts.text) != "" || strings.TrimSpace(facts.html) != "":
		return domain.ClipContent{Kind: domain.ClipKindText, Text: facts.text, HTML: facts.html}
	default:
		return domain.ClipContent{}
	}
}

// hasImage reports whether the clipboard offers an image.
func hasImage(formats []transfer.Format) bool {
	for _, format := range formats {
		if format == transfer.PNG {
			return true
		}
	}
	return false
}

// The device is what the use case asks for.
var _ domain.ClipboardDevice = (*Device)(nil)
