package domain

import (
	"context"
	"errors"
)

// SelectionDevice is the platform's own answer to "what has the user
// selected", read from the application they are working in rather than from
// the clipboard.
//
// It is the better question to ask. The clipboard holds what was copied
// last, which may be something else entirely — a password from a moment ago,
// a line from another window — so a tool that reads it is guessing. The
// selection is not a guess: the user has it in front of them right now, and
// reading it disturbs nothing, where copying it would replace what they had
// on the clipboard and, on the platforms that need one, would need a
// synthetic key press to do it.
//
// Not every platform, and not every application, publishes its selection. A
// system with no API for it, a control that does not implement the pattern,
// and an empty selection are all ordinary answers: they mean the caller
// should fall back to the clipboard, and ErrSelectionUnavailable says so
// rather than describing a failure.
type SelectionDevice interface {
	// SelectedText returns what is selected in the application the user is
	// working in, trimmed to nothing when there is none. It returns
	// ErrSelectionUnavailable when the platform cannot say.
	//
	// It may run on any goroutine; an implementation is responsible for
	// whatever thread the platform requires.
	SelectedText(ctx context.Context) (string, error)
}

// ErrSelectionUnavailable means the platform could not say what is selected:
// it has no such API, the application in front does not publish its
// selection, or nothing is selected.
var ErrSelectionUnavailable = errors.New("the platform has no selected text to give")
