package selection

import (
	"context"
	"testing"
	"time"

	"boku/internal/domain"
)

// fakeClipboard is the platform clipboard, with what a copy puts on it decided
// by the test.
type fakeClipboard struct {
	content domain.ClipContent
	err     error
	// writes is what was put back, oldest first.
	writes []string
	// files is what a file list was put back as.
	files [][]string
	// images is what a bitmap was put back as.
	images [][]byte
}

func (f *fakeClipboard) Read(context.Context) (domain.ClipContent, error) {
	return f.content, f.err
}

func (f *fakeClipboard) WriteText(_ context.Context, text string) error {
	f.writes = append(f.writes, text)
	f.content = domain.ClipContent{Kind: domain.ClipKindText, Text: text, Display: text}
	return nil
}

func (f *fakeClipboard) WriteFiles(_ context.Context, paths []string) error {
	f.files = append(f.files, paths)
	f.content = domain.ClipContent{Kind: domain.ClipKindFile, Files: paths, Display: paths[0]}
	return nil
}

func (f *fakeClipboard) WriteImage(_ context.Context, png []byte) error {
	f.images = append(f.images, append([]byte(nil), png...))
	f.content = domain.ClipContent{Kind: domain.ClipKindImage, HasImage: true, Image: png, ImageBytes: len(png)}
	return nil
}

// TestACopyIsWhatIsReadAndTheClipboardIsPutBack is the whole point of the
// fallback: an application that publishes nothing is asked to copy, the text
// arrives on the clipboard, and what the user had there is put back.
func TestACopyIsWhatIsReadAndTheClipboardIsPutBack(t *testing.T) {
	clipboard := &fakeClipboard{content: domain.ClipContent{Kind: domain.ClipKindText, Text: "what the user had"}}
	var sequence uint32 = 7
	copied := 0
	text := readWithACopy(context.Background(), clipboard,
		func() { copied++ },
		func() uint32 { return sequence },
		func() {
			clipboard.content = domain.ClipContent{Kind: domain.ClipKindText, Text: "what the user selected"}
			sequence++
		})
	if text != "what the user selected" {
		t.Errorf("read = %q, want what the application copied", text)
	}
	if copied != 1 {
		t.Errorf("the clipboard tool was told about the copy %d times, want once", copied)
	}
	if len(clipboard.writes) != 1 || clipboard.writes[0] != "what the user had" {
		t.Errorf("the clipboard was put back as %q", clipboard.writes)
	}
	if got := clipboard.content.Text; got != "what the user had" {
		t.Errorf("the clipboard holds %q, want what was there before", got)
	}
}

// An application with nothing selected answers the copy with nothing, and that
// is "no selection" rather than an empty translation: the clipboard's own write
// counter says which of the two happened.
func TestACopyThatChangesNothingIsNoSelection(t *testing.T) {
	clipboard := &fakeClipboard{content: domain.ClipContent{Kind: domain.ClipKindText, Text: "what the user had"}}
	text := readWithACopy(context.Background(), clipboard,
		nil,
		func() uint32 { return 12 },
		func() { /* the application copied nothing */ })
	if text != "" {
		t.Errorf("read = %q, want nothing", text)
	}
	if len(clipboard.writes) != 0 {
		t.Errorf("the clipboard was written %d times although nothing changed", len(clipboard.writes))
	}
}

// A picture on the clipboard is not a reason to refuse to read the selection:
// it is saved, the copy is made over it, and the picture goes back. This is
// what the user hit — a screenshot on the clipboard made every selection
// translation answer "the clipboard holds no text".
func TestAPictureOnTheClipboardIsPutBack(t *testing.T) {
	picture := []byte("89504e470d0a1a0a-screenshot")
	clipboard := &fakeClipboard{content: domain.ClipContent{
		Kind: domain.ClipKindImage, HasImage: true, Image: picture, ImageBytes: len(picture),
	}}
	var sequence uint32 = 3
	text := readWithACopy(context.Background(), clipboard, nil,
		func() uint32 { return sequence },
		func() {
			clipboard.content = domain.ClipContent{Kind: domain.ClipKindText, Text: "the selection"}
			sequence++
		})
	if text != "the selection" {
		t.Errorf("read = %q, want the text the application copied over the picture", text)
	}
	if len(clipboard.images) != 1 || string(clipboard.images[0]) != string(picture) {
		t.Errorf("the picture was put back as %q, want the bytes that were there", clipboard.images)
	}
	if clipboard.content.Kind != domain.ClipKindImage {
		t.Errorf("the clipboard holds %q, want the picture back", clipboard.content.Kind)
	}
}

// A picture whose bytes could not be read is the one thing still left alone:
// copying over it would lose it, and this tool cannot put it back.
func TestAPictureWithoutItsBytesIsNotCopiedOver(t *testing.T) {
	clipboard := &fakeClipboard{content: domain.ClipContent{
		Kind: domain.ClipKindImage, HasImage: true, ImageBytes: 4096,
	}}
	pressed := false
	text := readWithACopy(context.Background(), clipboard, nil,
		func() uint32 { return 3 },
		func() { pressed = true })
	if text != "" {
		t.Errorf("read = %q, want nothing", text)
	}
	if pressed {
		t.Error("the application was asked to copy over a picture that could not be restored")
	}
}

// A copy that brings back a file list or an image is not a text to translate,
// and the clipboard is put back all the same.
func TestACopyOfSomethingOtherThanTextIsNotATranslation(t *testing.T) {
	for _, arrived := range []domain.ClipContent{
		{Kind: domain.ClipKindFile, Files: []string{`C:\a.txt`}},
		{Kind: domain.ClipKindImage, ImageBytes: 16},
	} {
		clipboard := &fakeClipboard{content: domain.ClipContent{Kind: domain.ClipKindText, Text: "what the user had"}}
		var sequence uint32 = 1
		text := readWithACopy(context.Background(), clipboard, nil,
			func() uint32 { return sequence },
			func() { clipboard.content = arrived; sequence++ })
		if text != "" {
			t.Errorf("kind %s read as %q, want nothing to translate", arrived.Kind, text)
		}
		if len(clipboard.writes) != 1 {
			t.Errorf("kind %s: the clipboard was put back %d times, want once", arrived.Kind, len(clipboard.writes))
		}
	}
}

// A clipboard this tool cannot read is not a reason to refuse: the copy is made
// over it and there is simply nothing to put back.
func TestACopyOverAnUnknownClipboardIsStillMade(t *testing.T) {
	clipboard := &fakeClipboard{err: domain.ErrUnsupportedClipboardFormat}
	var sequence uint32 = 5
	text := readWithACopy(context.Background(), clipboard, nil,
		func() uint32 { return sequence },
		func() {
			clipboard.err = nil
			clipboard.content = domain.ClipContent{Kind: domain.ClipKindText, Text: "the selection"}
			sequence++
		})
	if text != "the selection" {
		t.Errorf("read = %q, want the copied text", text)
	}
	if len(clipboard.writes) != 0 {
		t.Errorf("something was put back although nothing could be read: %q", clipboard.writes)
	}
}

// The waiting is bounded by the budget, and it stops as soon as the counter
// moves: a fast application is not made to wait for a slow one's budget.
func TestTheWaitForTheCopyIsBounded(t *testing.T) {
	slept := time.Duration(0)
	steps := 0
	changed := waitForTheClipboardToChange(
		func() uint32 { return 1 },
		1,
		50*time.Millisecond,
		10*time.Millisecond,
		func(d time.Duration) { slept += d; steps++ },
	)
	if changed {
		t.Error("the wait reported a change that never happened")
	}
	if slept > 60*time.Millisecond {
		t.Errorf("the wait slept %v, want no more than the budget", slept)
	}

	// A counter that moves on the third look: the wait ends there rather than
	// running out its budget.
	looks := 0
	changed = waitForTheClipboardToChange(
		func() uint32 {
			looks++
			if looks > 3 {
				return 2
			}
			return 1
		},
		1,
		500*time.Millisecond,
		10*time.Millisecond,
		func(time.Duration) {},
	)
	if !changed {
		t.Error("the wait missed a change that happened inside its budget")
	}
	if looks > 5 {
		t.Errorf("the wait kept looking %d times after the counter moved", looks)
	}
}

// A reading that answers is the answer: the retrying exists for applications
// that need asking twice, not for making a working reading slower.
func TestAReadingThatAnswersIsNotRepeated(t *testing.T) {
	asked := 0
	text := readUntilAnswered(context.Background(), func(context.Context, time.Duration) string {
		asked++
		return "the selection"
	}, 4, time.Millisecond, time.Now().Add(time.Second))
	if text != "the selection" {
		t.Errorf("read = %q, want the reading", text)
	}
	if asked != 1 {
		t.Errorf("the platform was asked %d times, want once", asked)
	}
}

// The case this was written for: an application that has never been asked by an
// accessibility client answers nothing the first time and has the selection on
// the second reading (measured against a freshly started Chrome: 25ms and then
// 105ms later).
func TestAReadingThatAnswersNothingIsTriedAgain(t *testing.T) {
	asked := 0
	text := readUntilAnswered(context.Background(), func(context.Context, time.Duration) string {
		asked++
		if asked < 2 {
			return ""
		}
		return "the selection"
	}, DefaultAttempts, time.Millisecond, time.Now().Add(time.Second))
	if text != "the selection" {
		t.Errorf("read = %q, want the selection the second reading had", text)
	}
	if asked != 2 {
		t.Errorf("the platform was asked %d times, want twice", asked)
	}
}

// An application with nothing selected is the ordinary case, and the clipboard
// is waiting behind it: the attempts are bounded and so is the wait.
func TestNothingSelectedGivesUpAfterTheAttempts(t *testing.T) {
	asked := 0
	start := time.Now()
	text := readUntilAnswered(context.Background(), func(context.Context, time.Duration) string {
		asked++
		return "   "
	}, 3, time.Millisecond, time.Now().Add(time.Second))
	if text != "" {
		t.Errorf("read = %q, want nothing", text)
	}
	if asked != 3 {
		t.Errorf("the platform was asked %d times, want the three attempts", asked)
	}
	if waited := time.Since(start); waited > 200*time.Millisecond {
		t.Errorf("giving up took %v, want the gaps only", waited)
	}
}

// The overall bound is the whole reading, retries included: a reading that has
// already run out of time does not start another attempt.
func TestTheDeadlineEndsTheRetrying(t *testing.T) {
	asked := 0
	text := readUntilAnswered(context.Background(), func(context.Context, time.Duration) string {
		asked++
		return ""
	}, 5, time.Millisecond, time.Now().Add(-time.Second))
	if text != "" || asked != 0 {
		t.Errorf("read = %q after %d attempts, want nothing and no attempt at all", text, asked)
	}
}

// A caller that has given up is not kept waiting through the gaps.
func TestACancelledReadingStopsBetweenAttempts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	asked := 0
	cancel()
	start := time.Now()
	text := readUntilAnswered(ctx, func(context.Context, time.Duration) string {
		asked++
		return ""
	}, 5, time.Second, time.Now().Add(time.Minute))
	if text != "" {
		t.Errorf("read = %q, want nothing", text)
	}
	if waited := time.Since(start); waited > 100*time.Millisecond {
		t.Errorf("a cancelled reading waited %v, want the first attempt only", waited)
	}
	if asked > 1 {
		t.Errorf("the platform was asked %d times after the caller gave up", asked)
	}
}
