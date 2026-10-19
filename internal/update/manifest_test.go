package update

import (
	"errors"
	"strings"
	"testing"

	"boku/internal/domain"
)

// The manifest is the release's own file, and the adapter rules of AGENTS.md
// apply to it as much as to a provider's answer: a body that is not a
// manifest is a bad response, never a release with zero values in it.
func TestDecodeManifestRejectsWhatIsNotARelease(t *testing.T) {
	for _, c := range []struct {
		name string
		body string
	}{
		{"empty", ""},
		{"whitespace", "  \n\t "},
		{"not json", "<html>404</html>"},
		{"a list", `[{"version":"1.0.0"}]`},
		{"a string", `"the release"`},
		{"an object with nothing in it", `{}`},
		{"no version", `{"url":"https://example.invalid/a.tar.gz","size":10,"signature":"sig"}`},
		{"no url", `{"version":"1.0.0","size":10,"signature":"sig"}`},
		{"no size", `{"version":"1.0.0","url":"https://example.invalid/a.tar.gz","signature":"sig"}`},
		{"negative size", `{"version":"1.0.0","url":"https://example.invalid/a.tar.gz","size":-1,"signature":"sig"}`},
		{"no signature", `{"version":"1.0.0","url":"https://example.invalid/a.tar.gz","size":10}`},
		{"truncated", `{"version":"1.0.0","url":`},
		{"two documents", `{"version":"1.0.0","url":"https://example.invalid/a.tar.gz","size":1,"signature":"s"} {"version":"2.0.0"}`},
		{"a version that is a path", `{"version":"../escape","url":"https://example.invalid/a.tar.gz","size":10,"signature":"sig"}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			release, err := DecodeManifest([]byte(c.body), "")
			if !errors.Is(err, domain.ErrProviderBadResponse) {
				t.Fatalf("DecodeManifest(%q) = (%v, %v), want ErrProviderBadResponse", c.body, release, err)
			}
			if release != nil {
				t.Errorf("a bad manifest produced a release: %+v", release)
			}
		})
	}
}

// A manifest larger than the framework reads is refused rather than partly
// understood.
func TestDecodeManifestRefusesAnOversizedBody(t *testing.T) {
	body := `{"version":"1.0.0","notes":"` + strings.Repeat("x", maxManifestBytes) + `"}`
	if _, err := DecodeManifest([]byte(body), ""); !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("DecodeManifest = %v, want ErrProviderBadResponse", err)
	}
}

// The happy path: what `mygo build` writes becomes the release the interface
// shows, with the digest the caller checked bound into the asset.
func TestDecodeManifestReadsARelease(t *testing.T) {
	body := `{
		"version": "0.1.0-beta.6",
		"notes": "  - one change\n- another  ",
		"date": "2026-10-19T00:00:00Z",
		"url": "https://github.com/QiYuOr2/boku-desktop/releases/download/v0.1.0-beta.6/boku-0.1.0-beta.6-windows-amd64.tar.gz",
		"size": 1234567,
		"signature": "c2lnbmF0dXJl",
		"previous": [{"version":"0.1.0-beta.5","url":"https://example.invalid/old.tar.gz","size":42,"signature":"old"}],
		"deltas": [{"from":"0.1.0-beta.5","url":"https://example.invalid/d.delta","size":12,"signature":"delta"}]
	}`
	digest := strings.Repeat("a", 64)
	release, err := DecodeManifest([]byte(body), digest)
	if err != nil {
		t.Fatalf("DecodeManifest: %v", err)
	}
	if release.Version != "0.1.0-beta.6" {
		t.Errorf("version = %q", release.Version)
	}
	if release.Notes != "- one change\n- another" {
		t.Errorf("notes = %q", release.Notes)
	}
	asset, ok := release.Archive()
	if !ok {
		t.Fatalf("the release has no archive: %+v", release)
	}
	if asset.Size != 1234567 {
		t.Errorf("size = %d", asset.Size)
	}
	if asset.SHA256 != digest {
		t.Errorf("digest = %q, want the one the caller checked", asset.SHA256)
	}
	// The deltas and the archives of earlier versions are the updater's own
	// business: they are read without complaint and are not an archive this
	// application names.
	if len(release.Assets) != 1 {
		t.Errorf("the release names %d assets, want the one archive: %+v", len(release.Assets), release.Assets)
	}
}

// A tag written with its "v" is the same version as one without: tags are
// how repositories write a version, and a directory is not.
func TestDecodeManifestTrimsTheTagPrefix(t *testing.T) {
	body := `{"version":"v0.1.0-beta.6","url":"https://example.invalid/a.tar.gz","size":10,"signature":"sig"}`
	release, err := DecodeManifest([]byte(body), "")
	if err != nil {
		t.Fatalf("DecodeManifest: %v", err)
	}
	if release.Version != "0.1.0-beta.6" {
		t.Errorf("version = %q, want the tag without its v", release.Version)
	}
}

// A digest that is given must be a SHA-256: anything else is a release whose
// download cannot be checked.
func TestDecodeManifestRefusesADigestThatIsNotOne(t *testing.T) {
	body := `{"version":"1.0.0","url":"https://example.invalid/a.tar.gz","size":10,"signature":"sig"}`
	if _, err := DecodeManifest([]byte(body), "abc"); !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("DecodeManifest = %v, want ErrProviderBadResponse", err)
	}
}

// MaxArchiveSize is the ceiling the framework applies; the update feature
// says the same number so that a release one of them refuses is not offered
// by the other.
func TestMaxArchiveSizeIsAFullGigabyte(t *testing.T) {
	if MaxArchiveSize != 1<<30 {
		t.Errorf("MaxArchiveSize = %d, want 1<<30", MaxArchiveSize)
	}
}
