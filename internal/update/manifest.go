package update

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"boku/internal/domain"
)

// maxManifestBytes bounds the update manifest this application reads. It is
// MyGo's own limit for the file (internal/update.MaxManifestSize): a
// manifest larger than this is not the file the release published.
const maxManifestBytes = 1 << 20

// manifest is the MyGo update manifest, update-<target>.json, as
// `mygo build` writes it. The shape is the release's, not this
// application's: it is decoded here, at the boundary, and becomes a Release
// with the names this package uses.
//
// Only the fields a release needs are kept. date, previous and deltas are
// the updater's own business — they feed the delta path this application
// does not take — and a manifest carrying them is read without complaint.
type manifest struct {
	Version   string `json:"version"`
	Notes     string `json:"notes"`
	URL       string `json:"url"`
	Size      int64  `json:"size"`
	Signature string `json:"signature"`
}

// DecodeManifest reads a MyGo update manifest and turns it into the release
// this application installs.
//
// It is the whole of the two feeds' contract: a manifest that cannot be
// read — not JSON, not an object, missing the version or the archive, with
// a size that cannot be a file — is domain.ErrProviderBadResponse and never
// a release with zero values in it. The reason it carries is trimmed to
// domain.MaxSnippetBytes and names a field rather than quoting the body.
//
// digest is the SHA-256 of the archive, in lowercase hex, computed by
// whoever verified the manifest's signature: the manifest signs a digest,
// not a URL, and this package has no public key. It is what binds the bytes
// on disk to the release, and it may be empty when the platform cannot
// verify it, in which case the download is checked for its size alone.
func DecodeManifest(payload []byte, digest string) (*Release, error) {
	trimmed := strings.TrimSpace(string(payload))
	if trimmed == "" {
		return nil, BadRelease("body", "the feed answered with nothing")
	}
	if len(payload) > maxManifestBytes {
		return nil, BadRelease("body", fmt.Sprintf("the manifest is larger than %d bytes", maxManifestBytes))
	}
	var m manifest
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	// A manifest is an object. Anything else — a list, a string, a number —
	// decodes into the zero value without complaint, which is exactly the
	// zero-value success this refuses.
	if err := decoder.Decode(&m); err != nil {
		return nil, BadRelease("body", err.Error())
	}
	if decoder.More() {
		return nil, BadRelease("body", "the manifest carries more than one document")
	}
	if strings.TrimSpace(m.Version) == "" {
		return nil, BadRelease("version", "the manifest names no version")
	}
	if strings.TrimSpace(m.URL) == "" {
		return nil, BadRelease("url", "the manifest names no archive")
	}
	if m.Size <= 0 {
		return nil, BadRelease("size", fmt.Sprintf("the manifest gives the archive a size of %d bytes", m.Size))
	}
	if strings.TrimSpace(m.Signature) == "" {
		return nil, BadRelease("signature", "the manifest does not sign the archive")
	}
	release := &Release{
		Version: releaseVersion(m.Version),
		Notes:   strings.TrimSpace(m.Notes),
		Assets: []Asset{{
			// The downloaded archive is stored under a name this
			// application decides: the URL's own path is the release's
			// business, and the version directory is what says which
			// release it is.
			Name:   archiveNameFor(releaseVersion(m.Version)),
			URL:    strings.TrimSpace(m.URL),
			Size:   m.Size,
			SHA256: strings.ToLower(strings.TrimSpace(digest)),
		}},
	}
	if err := release.Validate(); err != nil {
		return nil, err
	}
	return release, nil
}

// releaseVersion is the version a manifest names, as a directory name: the
// manifest is the release's, and a leading "v" is how tags are written.
func releaseVersion(version string) string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}

// archiveNameFor is what a downloaded release is called on disk. The
// extension is the one MyGo publishes, so that a directory listing says what
// the file is; the version directory around it says which release it is.
func archiveNameFor(version string) string {
	return version + ".tar.gz"
}

// DecodeManifestFrom reads a manifest from r, bounded by maxManifestBytes.
func DecodeManifestFrom(r io.Reader, digest string) (*Release, error) {
	payload, err := io.ReadAll(io.LimitReader(r, maxManifestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: reading the manifest: %v", domain.ErrProviderBadResponse, err)
	}
	return DecodeManifest(payload, digest)
}
