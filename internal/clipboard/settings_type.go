// This file is the clipboard tool's own configuration: the switches that are
// about the clipboard and nothing else.
//
// It lives with the feature rather than in internal/domain because the
// clipboard is the only thing that reads it. What a feature stores should look
// like what that feature is, and a type only one feature uses belongs to that
// feature — domain is for the entities and ports several features share.
//
// The file it is written to is still the application's one settings file, and
// the keys it is written under are still the ones docs/database.md §8
// describes: the grouping is a property of the value, not of where it is
// stored. internal/settings owns that file and does not know this type.
package clipboard

import "time"

// Settings is what the user chose about the clipboard tool: whether its line
// is shown, whether it is read in the background, how often, and how much
// history is kept.
//
// Both switches default to false. The clipboard is the most sensitive thing
// Boku can read, so reading it is something the user turns on rather than
// something they have to find and turn off.
type Settings struct {
	// RegionVisible shows the clipboard line in the quick window.
	RegionVisible bool
	// Monitoring reads the clipboard in the background. Off means no reading
	// at all, not a hidden reading.
	Monitoring bool
	// PollInterval is how often the clipboard is read while monitoring.
	// Values below MinPollInterval are raised to it.
	PollInterval time.Duration
	// HistorySize is how many of the most recent copies the history keeps,
	// and therefore how many the history page lists. Anything that is not one
	// of HistorySizes is replaced by DefaultHistorySize.
	HistorySize int
}

// Normalize fills in what a stored setting left out and brings the interval
// inside its bounds. It never fails: a hand-edited file should not keep the
// application from starting, it should just be made sane.
func (s Settings) Normalize() Settings {
	if s.PollInterval < MinPollInterval {
		s.PollInterval = DefaultPollInterval
	}
	if !ValidHistorySize(s.HistorySize) {
		s.HistorySize = DefaultHistorySize
	}
	return s
}

// Interval is the poll interval, inside its bounds.
func (s Settings) Interval() time.Duration {
	if s.PollInterval < MinPollInterval {
		return DefaultPollInterval
	}
	return s.PollInterval
}

// KeepsHistory is how many copies the history keeps, one of the record ranges
// whatever the stored value said.
func (s Settings) KeepsHistory() int {
	if !ValidHistorySize(s.HistorySize) {
		return DefaultHistorySize
	}
	return s.HistorySize
}

// Reads reports whether the clipboard is read at all, which is what the quick
// window's line depends on.
func (s Settings) Reads() bool { return s.RegionVisible && s.Monitoring }

// Bounds on the clipboard poll. The floor keeps a mistaken configuration from
// turning the poll into a busy loop; the default is the interval docs/tools.md
// §4.1 names.
const (
	MinPollInterval     = 200 * time.Millisecond
	DefaultPollInterval = 700 * time.Millisecond
	// MaxTextBytes is how much text of one copy is kept. Anything longer is
	// truncated: a clipboard holding a whole file must not end up in the
	// history.
	MaxTextBytes = 64 << 10
	// MaxImageBytes is how large a bitmap on the clipboard is read. A larger
	// one is not read at all: the entry still says that a picture was copied,
	// without the application holding it.
	MaxImageBytes = 16 << 20
)

// The record range the user may choose, in entries: a short list that stays a
// glance, or a longer one for someone who keeps going back to it. It is a
// choice among three rather than a number, because a bound nobody can reason
// about is a bound nobody sets.
const (
	MinHistorySize     = 10
	DefaultHistorySize = 30
	MaxHistorySize     = 50
)

// historySizes is the choice, in the order it is offered. It is an array so
// that nothing can change it, and so that answering "is this one of them"
// costs no allocation on a frame.
var historySizes = [...]int{MinHistorySize, DefaultHistorySize, MaxHistorySize}

// HistorySizes returns the record ranges the user may choose, from the
// shortest to the longest.
func HistorySizes() []int { return append([]int(nil), historySizes[:]...) }

// ValidHistorySize reports whether n is one of the record ranges.
func ValidHistorySize(n int) bool {
	for _, size := range historySizes {
		if size == n {
			return true
		}
	}
	return false
}
