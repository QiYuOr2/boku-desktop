// Package selection reads what the user has selected in another application,
// through the platform's own accessibility API.
//
// It exists because reading the selection is a better answer than reading the
// clipboard: the clipboard is what was copied last, and the selection is what
// the user is looking at. On Windows the answer comes from UI Automation,
// which is the same interface a screen reader reads a control with; a control
// that does not publish its text — and a system, such as Linux without a
// running accessibility bus, that has no such interface at all — answers
// ErrSelectionUnavailable, and the caller falls back to the clipboard.
//
// The two implementations live in selection_windows.go and
// selection_other.go: one platform has the API, and the rest answer with the
// same "not here" as a control that does not publish its text.
package selection

import "time"

// DefaultTimeout bounds one reading of the selection. It is short: the answer
// is about what the user has in front of them now, and a request that took
// longer than this is one the user has already moved on from — the clipboard
// then answers instead.
const DefaultTimeout = 2 * time.Second
