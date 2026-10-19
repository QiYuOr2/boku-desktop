// Package update is the update feature: finding out that a newer Boku
// exists, fetching it in the background, checking it against what the
// release promised, and putting it in place so that the next launch runs it.
//
// It does not talk to MyGo and it does not open a network connection. The
// updater that knows the release feed and the transport that fetches an
// archive are both injected — main.go adapts mygo.Updater and mygo.App to
// the interfaces below — which is what keeps the state machine, the timer
// and the download testable without a desktop session and without a
// network. AGENTS.md's Network Rules say where outbound HTTP belongs; this
// package receives it rather than performing it.
//
// What the interface draws is domain.UpdateState, and this package decides
// it: the views never see a release, a URL or an error.
package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"boku/internal/domain"
)

// Updater is a release feed: whether this machine can update itself, which
// release is newer than the running version, a way to fetch one of its
// files, and the installation itself.
//
// main.go implements it over mygo.Updater, mygo.Update and mygo.App; a test
// implements it with a fake.
type Updater interface {
	// Enabled reports whether this installation can update itself: a build
	// published with an update feed, in a directory it may write. A build
	// that says no is not broken — see domain.UpdateUnsupported.
	Enabled() bool
	// Release asks the feed for a version newer than current. It returns
	// nil and a nil error when the running version is the newest.
	//
	// A release that cannot be read is an error that wraps
	// domain.ErrProviderBadResponse, and one that cannot be fetched wraps
	// domain.ErrProviderUnavailable; nothing the feed answered is quoted in
	// the message.
	Release(ctx context.Context, current string) (*Release, error)
	// Fetch opens one of a release's assets for reading. The caller closes
	// it.
	Fetch(ctx context.Context, url string) (io.ReadCloser, error)
	// Install applies a version this application has prepared: the updater
	// verifies its signature and replaces the installation in place. It is
	// called once, by Service.Install, after the download has been checked.
	Install(ctx context.Context, version string) error
}

// Relauncher restarts the application, which is how an applied update takes
// effect. main.go implements it over mygo.App.Relaunch, which quits the
// process like a quit and starts it again with the same arguments.
type Relauncher interface {
	Relaunch()
}

// Release is a newer version of the application, as the feed describes it:
// the fields of the MyGo update manifest (update-<target>.json) that this
// application uses.
type Release struct {
	Version string
	// Notes are the release notes, in Markdown, as the release publishes
	// them. The interface does not draw them yet; they are kept because the
	// feed has them and the state carries them.
	Notes string
	// Assets are the files of the release. The archive is the one this
	// application downloads, checks and displays.
	Assets []Asset
}

// Asset is one file of a release. The manifest signs the archive's SHA-256
// digest, so the digest is what an Asset carries: it is the only thing that
// binds the bytes on disk to the release.
type Asset struct {
	Name string
	URL  string
	Size int64
	// SHA256 is the digest of the file, in hexadecimal. It is empty when
	// the platform cannot verify it — the updater checks the signature of
	// the digest itself — and a download is then checked for its size
	// alone.
	SHA256 string
}

// Valid reports whether an asset says enough to be fetched and checked: a
// name, a URL, and a size it is worth comparing against.
func (a Asset) Valid() bool {
	return strings.TrimSpace(a.Name) != "" &&
		strings.TrimSpace(a.URL) != "" &&
		a.Size > 0
}

// Archive returns the release's first archive and whether it has one.
//
// The assets a release may name are: the archive, the deltas from earlier
// versions, and the archives of those earlier versions. Only the archive is
// something this application installs; a release that names none is one it
// cannot use, which Validate reports.
func (r *Release) Archive() (Asset, bool) {
	if r == nil {
		return Asset{}, false
	}
	for _, asset := range r.Assets {
		if isArchive(asset.Name) && asset.Valid() {
			return asset, true
		}
	}
	return Asset{}, false
}

// isArchive reports whether a name is one of the archives a release ships.
// `mygo build` names them <app>-<version>-<target>.tar.gz.
func isArchive(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") || strings.HasSuffix(lower, ".zip")
}

// Validate reports what a release does not say, as a response the caller
// cannot use.
//
// It is checked at the boundary — before a byte is downloaded — so that a
// manifest whose version or archive is missing becomes a visible failure and
// never a zero-value success. This is the adapter rule of AGENTS.md applied
// to a file the application fetches rather than one a provider answered.
func (r *Release) Validate() error {
	if r == nil {
		return BadRelease("version", "the release says nothing")
	}
	if strings.TrimSpace(r.Version) == "" {
		return BadRelease("version", "the release names no version")
	}
	if strings.ContainsAny(r.Version, `/\`) || r.Version == "." || r.Version == ".." {
		// The version becomes a directory name under the download
		// directory, so a version that is a path is a file written
		// somewhere the release chose.
		return BadRelease("version", "the version is not a plain name")
	}
	if _, ok := r.Archive(); !ok {
		return BadRelease("assets", "the release holds no archive to install")
	}
	for _, asset := range r.Assets {
		if asset.SHA256 != "" && !isHexDigest(asset.SHA256) {
			return BadRelease("assets", "an asset carries a digest that is not SHA-256")
		}
	}
	return nil
}

// BadRelease is a release the application could not use, reported the way
// every other bad response is: as domain.ErrProviderBadResponse. The reason
// is this package's own words and is trimmed to domain.MaxSnippetBytes, so a
// manifest that failed to parse does not become a log full of it.
//
// It is exported because main.go's adapter builds one when the manifest
// itself cannot be decoded, and the reason a release is unusable has to look
// the same whether it was found here or there.
func BadRelease(field, reason string) error {
	return fmt.Errorf("%w: the release's %s: %s",
		domain.ErrProviderBadResponse, field, domain.UpdateReason(reason))
}

// isHexDigest reports whether s is 64 hexadecimal characters, which is what
// a SHA-256 digest is written as.
func isHexDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// MaxArchiveSize bounds what the update feature will read of one archive. It
// is MyGo's own ceiling for a release: a release larger than this is one the
// updater would refuse anyway, and reading it here first only spends the
// bandwidth twice.
const MaxArchiveSize = 1 << 30

// wrapUnavailable is every failure on the way to an installed update: the
// feed could not be reached, an archive did not match what the release
// promised, the installation could not be replaced. The reason is a
// sentence for the user and never a URL or a response body.
func wrapUnavailable(reason string, cause error) error {
	reason = semantic(domain.UpdateReason(reason))
	if cause == nil {
		return fmt.Errorf("%w: %s", domain.ErrUpdateUnavailable, reason)
	}
	return fmt.Errorf("%w: %s: %v", domain.ErrUpdateUnavailable, reason, cause)
}

// IsUnsupported reports whether an error is the answer "this build cannot
// update itself", which is not a failure.
func IsUnsupported(err error) bool { return errors.Is(err, domain.ErrUpdateUnsupported) }

// IsUnavailable reports whether an error is a failed check or download.
func IsUnavailable(err error) bool { return errors.Is(err, domain.ErrUpdateUnavailable) }

// IsBadRelease reports whether an error is a release that could not be read.
func IsBadRelease(err error) bool { return errors.Is(err, domain.ErrProviderBadResponse) }

// semantic is the reason a user reads, in the one voice the domain uses for
// its own errors: lowercase, no trailing full stop.
func semantic(reason string) string {
	reason = strings.TrimSpace(reason)
	reason = strings.TrimRight(reason, ".!;,")
	if reason == "" {
		return "the update could not be completed"
	}
	return reason
}
