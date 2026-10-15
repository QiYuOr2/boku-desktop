package domain

import "time"

// ClipboardSettings is what the user chose about the clipboard tool: whether
// its line is shown, whether it is read in the background, how often, and how
// much history is kept.
//
// It is a type of its own rather than four fields of a shared settings bag
// because the clipboard feature is the only thing that reads them: what a
// feature stores should look like what that feature is. A feature is handed
// its own settings and cannot name another's.
//
// Both switches default to false. The clipboard is the most sensitive thing
// Boku can read, so reading it is something the user turns on rather than
// something they have to find and turn off.
type ClipboardSettings struct {
	// RegionVisible shows the clipboard line in the quick window.
	RegionVisible bool
	// Monitoring reads the clipboard in the background. Off means no reading
	// at all, not a hidden reading.
	Monitoring bool
	// PollInterval is how often the clipboard is read while monitoring.
	// Values below ClipboardMinPollInterval are raised to it.
	PollInterval time.Duration
	// HistorySize is how many of the most recent copies the history keeps,
	// and therefore how many the history page lists. Anything that is not
	// one of ClipboardHistorySizes is replaced by
	// ClipboardDefaultHistorySize.
	HistorySize int
}

// Normalize fills in what a stored setting left out and brings the interval
// inside its bounds. Like every other Normalize here it never fails: a
// hand-edited file should not keep the application from starting, it should
// just be made sane.
func (s ClipboardSettings) Normalize() ClipboardSettings {
	if s.PollInterval < ClipboardMinPollInterval {
		s.PollInterval = ClipboardDefaultPollInterval
	}
	if !ValidClipboardHistorySize(s.HistorySize) {
		s.HistorySize = ClipboardDefaultHistorySize
	}
	return s
}

// Interval is the poll interval, inside its bounds.
func (s ClipboardSettings) Interval() time.Duration {
	if s.PollInterval < ClipboardMinPollInterval {
		return ClipboardDefaultPollInterval
	}
	return s.PollInterval
}

// KeepsHistory is how many copies the history keeps, one of the record ranges
// whatever the stored value said.
func (s ClipboardSettings) KeepsHistory() int {
	if !ValidClipboardHistorySize(s.HistorySize) {
		return ClipboardDefaultHistorySize
	}
	return s.HistorySize
}

// Reads reports whether the clipboard is read at all, which is what the
// quick window's line depends on.
func (s ClipboardSettings) Reads() bool {
	return s.RegionVisible && s.Monitoring
}

// Bounds on the clipboard poll. The floor keeps a mistaken configuration
// from turning the poll into a busy loop; the default is the interval
// docs/tools.md §4.1 names.
const (
	ClipboardMinPollInterval     = 200 * time.Millisecond
	ClipboardDefaultPollInterval = 700 * time.Millisecond
	// MaxClipTextBytes is how much text of one copy is kept. Anything
	// longer is truncated: a clipboard holding a whole file must not end
	// up in the history.
	MaxClipTextBytes = 64 << 10
	// MaxClipImageBytes is how large a bitmap on the clipboard is read. A
	// larger one is not read at all: the entry still says that a picture
	// was copied, without the application holding it.
	MaxClipImageBytes = 16 << 20
)

// The record range the user may choose, in entries: a short list that stays a
// glance, or a longer one for someone who keeps going back to it. It is a
// choice among three rather than a number, because a bound nobody can reason
// about is a bound nobody sets.
const (
	ClipboardMinHistorySize     = 10
	ClipboardDefaultHistorySize = 30
	ClipboardMaxHistorySize     = 50
)

// clipboardHistorySizes is the choice, in the order it is offered. It is an
// array so that nothing can change it, and so that answering "is this one of
// them" costs no allocation on a frame.
var clipboardHistorySizes = [...]int{
	ClipboardMinHistorySize,
	ClipboardDefaultHistorySize,
	ClipboardMaxHistorySize,
}

// ClipboardHistorySizes returns the record ranges the user may choose, from
// the shortest to the longest.
func ClipboardHistorySizes() []int {
	return append([]int(nil), clipboardHistorySizes[:]...)
}

// ValidClipboardHistorySize reports whether n is one of the record ranges.
func ValidClipboardHistorySize(n int) bool {
	for _, size := range clipboardHistorySizes {
		if size == n {
			return true
		}
	}
	return false
}
