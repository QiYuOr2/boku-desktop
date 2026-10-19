package domain

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// UpdateStatus is where the application is in finding and preparing a newer
// version of itself. It is the whole of what the interface knows: the
// service in internal/update decides it, and the views draw it.
type UpdateStatus int

const (
	// UpdateIdle is a build that has not looked for an update yet. The
	// interface draws nothing for it.
	UpdateIdle UpdateStatus = iota
	// UpdateChecking is a check in flight: the feed is being asked, or a
	// release is being compared with the running version.
	UpdateChecking
	// UpdateUpToDate is an answer that this is the newest version.
	UpdateUpToDate
	// UpdateDownloading is a newer version being fetched in the background,
	// with UpdateState.Downloaded of UpdateState.Total bytes so far.
	UpdateDownloading
	// UpdateReady is a newer version that has been downloaded and checked:
	// it is in place and takes effect at the next launch.
	UpdateReady
	// UpdateUnsupported is a build that cannot update itself — a
	// development build, or an installation a package manager or an
	// administrator owns. It is not a failure and it is not said out loud:
	// there is nothing for the user to fix, and a row explaining it would
	// be noise on every launch.
	UpdateUnsupported
	// UpdateFailed is a check or a download that did not finish. It is the
	// only status with something for the user to do about it.
	UpdateFailed
)

// String names the status the way a log record wants it.
func (s UpdateStatus) String() string {
	switch s {
	case UpdateIdle:
		return "idle"
	case UpdateChecking:
		return "checking"
	case UpdateUpToDate:
		return "up_to_date"
	case UpdateDownloading:
		return "downloading"
	case UpdateReady:
		return "ready"
	case UpdateUnsupported:
		return "unsupported"
	case UpdateFailed:
		return "failed"
	}
	return fmt.Sprintf("update_status(%d)", int(s))
}

// Valid reports whether the status is one this build knows.
func (s UpdateStatus) Valid() bool {
	return s >= UpdateIdle && s <= UpdateFailed
}

// UpdateState is what the update service reports about itself, in the shape
// the interface draws: the status, the version it is about, and a sentence
// for the cases that need one.
//
// Detail is a human sentence and nothing else. It never holds a URL, a
// signature or a fragment of a response, because it is drawn on a settings
// page and written to a log; the errors that carry those are the service's,
// and what it keeps here is the answer it would show the user.
type UpdateState struct {
	Status UpdateStatus
	// Version is the version a Ready update would install, or — for
	// Downloading — the version being downloaded. It is empty for the
	// statuses that are about no particular version.
	Version string
	// Notes are the release notes, in Markdown, as the release publishes
	// them. They are the newer version's own words, or empty.
	Notes string
	// Downloaded is how many bytes of the update are on disk, and Total how
	// many there are to fetch. Total may be zero while it is not known yet;
	// Percent says whether the two are worth showing.
	Downloaded int64
	Total      int64
	// Detail is why the check or the download failed, in a sentence, for
	// UpdateFailed. It is empty for every other status.
	Detail string
}

// Percent is how much of the update has been fetched, 0..100, and whether
// the bound is known. A total of zero is "not known": a caller that shows
// nought percent then would tell the user the download is not happening.
func (s UpdateState) Percent() (int, bool) {
	if s.Total <= 0 || s.Downloaded < 0 {
		return 0, false
	}
	if s.Downloaded >= s.Total {
		return 100, true
	}
	return int(s.Downloaded * 100 / s.Total), true
}

// Settled reports whether the status is one a check has finished on: a
// check is not running and the state is not going to change until another
// one starts.
func (s UpdateState) Settled() bool {
	switch s.Status {
	case UpdateIdle, UpdateUpToDate, UpdateReady, UpdateUnsupported, UpdateFailed:
		return true
	}
	return false
}

// Update errors. They are separate names because they are separate
// sentences: "this build cannot update itself" is a fact about the
// installation that the interface keeps quiet about, while an unavailable
// update is a check or a download that failed and that the user can try
// again.
var (
	// ErrUpdateUnsupported is returned by a check when the running build
	// cannot update itself. It is not a failure: see UpdateUnsupported.
	ErrUpdateUnsupported = errors.New("updates are not available for this build")
	// ErrUpdateUnavailable is what anything that went wrong on the way to
	// a ready update wraps: a download, a download that does not match what
	// the release promised, a file that cannot be put in place, a
	// relaunch that could not be started.
	ErrUpdateUnavailable = errors.New("update unavailable")
)

// maxUpdateReasonBytes is how much of a reason is kept for a release that
// could not be used. It is MaxSnippetBytes: the rule for what a domain error
// carries, and the same rule applies whether the text came from a response
// body or from a JSON decoder.
const maxUpdateReasonBytes = MaxSnippetBytes

// UpdateReason trims a reason to the length an error message may carry, so
// that a manifest that failed to parse does not become a log full of it. It
// also makes the text valid UTF-8, because a truncated encoding is not text.
func UpdateReason(reason string) string {
	reason = strings.ToValidUTF8(strings.TrimSpace(reason), "\uFFFD")
	if len(reason) <= maxUpdateReasonBytes {
		return reason
	}
	cut := maxUpdateReasonBytes
	// An encoded character is one to four bytes, and the one starting
	// before the cut is the only one that can be left half in it.
	for cut > 0 && !utf8.RuneStart(reason[cut]) {
		cut--
	}
	return strings.TrimSpace(reason[:cut])
}
