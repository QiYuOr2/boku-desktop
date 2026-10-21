// Package selection reads what the user has selected in another application.
//
// It exists because reading the selection is a better answer than reading the
// clipboard: the clipboard is what was copied last, and the selection is what
// the user is looking at. On Windows the first answer comes from UI Automation,
// which is the same interface a screen reader reads a control with; on macOS
// it comes from the Accessibility API.
//
// Neither covers everything: a browser turns its accessibility on only when it
// is first asked, an application may not publish its text at all (the Windows
// 11 Notepad is one), and a chat window may answer nothing however it is
// asked. So the device has a second answer, which is what other desktop
// translation tools do and is why they work everywhere: **ask the application
// to copy**. The copy goes to the clipboard, the text is read from there, and
// what was on the clipboard before is put back — which is stated here rather
// than hidden because it is the one thing this package does that the user can
// notice.
//
// What is on the clipboard and cannot be put back — a bitmap whose bytes the
// platform would not hand over — is never copied over: a translation is not
// worth losing the user's screenshot. A copy in a console window is not made
// either: there the copy combination is a signal, not a copy. On macOS the
// same rule is applied to a secure input field.
//
// A system with no such interface at all, an application that answers nothing
// and a selection of nothing are all ordinary answers: they mean the caller
// should fall back to the clipboard it already has, and ErrSelectionUnavailable
// says so rather than describing a failure.
//
// The implementations live in selection_windows.go, selection_darwin.go and
// selection_other.go: the first two have both answers, and the rest answer with
// the same "not here" as an application that publishes nothing.
package selection

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/logonce"
)

// DefaultTimeout bounds one reading of the selection, all attempts together. It
// is short: the answer is about what the user has in front of them now, and a
// request that took longer than this is one the user has already moved on from —
// the clipboard then answers instead.
const DefaultTimeout = 2 * time.Second

// DefaultAttempts and DefaultRetryGap say how a reading that answers nothing is
// tried again.
//
// One reading is not enough, and the reason is a property of the applications
// being read: an application that no accessibility client has ever asked — a
// browser, typically — turns its accessibility on when it is *first* asked and
// builds its tree a moment later, so the first reading comes back with nothing
// to say and the next one has the selection. Measured on a freshly started
// Chrome: the first reading answered nothing at 25ms, the second had the
// selected text 105ms later. Four attempts 150ms apart answer that case and
// still reach the copy below — which is what an application with nothing
// selected has to reach — in under half a second.
const (
	DefaultAttempts = 4
	DefaultRetryGap = 150 * time.Millisecond
)

// CopyWait and CopyStep bound the wait for an application to answer a synthetic
// copy. The clipboard's own write counter is watched in steps, so a fast
// application answers in one step and a slow one is given the whole of
// CopyWait.
const (
	CopyWait = 200 * time.Millisecond
	CopyStep = 10 * time.Millisecond
)

// Options is what a device is assembled from.
type Options struct {
	// Clipboard is the platform clipboard, which the copy fallback reads and
	// puts back. Without one the device only reads what UI Automation
	// publishes.
	Clipboard domain.ClipboardDevice
	// BeforeCopy, when set, is called immediately before the synthetic copy.
	// The clipboard tool uses it to keep the value that copy puts on the
	// clipboard out of the history: it is not something the user copied, and
	// the tool is about to put back what was.
	BeforeCopy func()
	// Sequence is the clipboard's write counter, when the platform has one.
	// It is what tells a copy that replaced the clipboard from a copy that
	// selected nothing: an application with no selection answers the copy
	// with nothing, and the counter does not move.
	//
	// It is a function rather than a platform call so that the copy fallback
	// is the same logic on every platform that has a counter; a platform
	// without one leaves it nil and does not use the fallback.
	Sequence func() uint32
}

// attempt is one reading of the selection: the text, or "" when the platform
// had nothing to say within the time it was given.
type attempt func(ctx context.Context, within time.Duration) string

// readUntilAnswered tries a reading until one of them answers, the attempts run
// out, the deadline passes or the caller gives up.
//
// It is separate from the platform it runs on so that the retrying — which is
// where the subtlety is — can be tested without a desktop session.
func readUntilAnswered(ctx context.Context, read attempt, attempts int, gap time.Duration, deadline time.Time) string {
	for round := 0; round < attempts; round++ {
		if round > 0 {
			select {
			case <-ctx.Done():
				return ""
			case <-time.After(gap):
			}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return ""
		}
		if text := read(ctx, remaining); strings.TrimSpace(text) != "" {
			return text
		}
	}
	return ""
}

// readWithACopy asks the application itself for its selection: press is the
// copy combination, sequence is the clipboard's own write counter, and what the
// copy puts on the clipboard is read and then replaced by what was there.
//
// The write counter is what tells "nothing is selected" from "an empty
// selection": an application with nothing selected answers the copy with
// nothing, and its counter does not move. The wait is bounded, and so is the
// damage — a clipboard this tool cannot put back is left alone rather than
// copied over.
//
// It is here rather than in the platform file so that the whole of the decision
// can be tested with a fake clipboard and a fake copy.
func readWithACopy(ctx context.Context, clipboard domain.ClipboardDevice, beforeCopy func(), sequence func() uint32, press func()) string {
	if clipboard == nil {
		return ""
	}
	saved, savedErr := clipboard.Read(ctx)
	if savedErr == nil && !putBackable(saved) {
		// Something this tool cannot restore is on the clipboard. Copying over
		// it would lose it, and a translation is not worth that.
		return ""
	}
	was := sequence()
	if beforeCopy != nil {
		beforeCopy()
	}
	press()
	if !waitForTheClipboardToChange(sequence, was, CopyWait, CopyStep, time.Sleep) {
		// The application answered the copy with nothing, which is what an
		// application with no selection does.
		return ""
	}
	got, gotErr := clipboard.Read(ctx)
	putBack(ctx, clipboard, saved, savedErr)
	if gotErr != nil || got.Kind != domain.ClipKindText {
		// A file list or an image arrived: neither is text to translate, and
		// the clipboard has been put back either way.
		return ""
	}
	return got.Text
}

// putBackable reports whether what is on the clipboard can be put back after a
// synthetic copy has replaced it: text, a file list, and a bitmap whose bytes
// came with the reading. A bitmap whose bytes could not be read cannot — and
// nothing may destroy what it cannot restore, which is what the user's
// screenshot would be.
func putBackable(content domain.ClipContent) bool {
	switch content.Kind {
	case domain.ClipKindText, domain.ClipKindFile:
		return true
	case domain.ClipKindImage:
		return len(content.Image) > 0
	}
	return false
}

// putBack puts the clipboard back the way the copy found it.
//
// A write that fails is logged and not returned: the caller asked for the
// selection, and the copy it made has already happened either way — there is
// nothing it could do differently, and failing the reading over a clipboard
// that would not take the old value back would turn a translation into an
// error about something the user did not ask about. It is still worth a line:
// the user's previous clipboard is gone at that point, and this is the only
// record of why.
func putBack(ctx context.Context, clipboard domain.ClipboardDevice, saved domain.ClipContent, savedErr error) {
	if savedErr != nil {
		// Nothing could be read, so there is nothing to put back: the copy was
		// made over a clipboard this tool cannot describe.
		return
	}
	var err error
	switch saved.Kind {
	case domain.ClipKindFile:
		err = clipboard.WriteFiles(ctx, saved.Files)
	case domain.ClipKindImage:
		err = clipboard.WriteImage(ctx, saved.Image)
	default:
		err = clipboard.WriteText(ctx, saved.Text)
	}
	// Deduplicated: this runs once per selection read, which is once per
	// shortcut press or per screenshot tap — a user pressing the key again
	// because nothing happened would otherwise write the same line each time,
	// and the clipboard being unusable is not news after the first minute.
	if err != nil && logonce.Should("selection-clipboard-restore", err.Error()) {
		slog.Warn("the clipboard could not be put back after reading the selection", "event", "clipboard_restore_failed", "kind", saved.Kind, "error", err)
	}
	if err == nil {
		logonce.Forget("selection-clipboard-restore")
	}
}

// waitForTheClipboardToChange waits for the clipboard to be written to, as the
// system itself counts writes. It reports whether it changed.
//
// The counter and the clock are arguments so that the waiting can be tested
// without a clipboard and without spending the time it waits for.
func waitForTheClipboardToChange(sequence func() uint32, was uint32, budget, step time.Duration, sleep func(time.Duration)) bool {
	for waited := time.Duration(0); waited < budget; waited += step {
		if sequence() != was {
			return true
		}
		sleep(step)
	}
	return sequence() != was
}
