package update

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
)

// fakeUpdater is the feed and the transport in one value a test can make
// answer anything: what the feed says, what the archive is, and whether the
// installation was asked for.
type fakeUpdater struct {
	enabled bool

	mu sync.Mutex
	// release is what the feed answers. nil with a nil error is "up to date".
	release *Release
	// releaseErr is a feed that could not be read.
	releaseErr error
	// archive is what Fetch serves, by URL. A URL that is not here is a file
	// that cannot be downloaded.
	archive map[string][]byte
	// fetchErr fails Fetch for the URL it is set for.
	fetchErr map[string]error

	// releases and fetches count what was asked, which is what the single
	// flight is checked with.
	releases int
	fetches  []string
	// installed is the version Install was asked for, and installs counts
	// the calls so that a second install is visible.
	installed  string
	installs   int
	installErr error
}

func newFakeUpdater() *fakeUpdater {
	return &fakeUpdater{enabled: true, archive: map[string][]byte{}, fetchErr: map[string]error{}}
}

func (f *fakeUpdater) Enabled() bool { return f.enabled }

func (f *fakeUpdater) Release(context.Context, string) (*Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releases++
	if f.releaseErr != nil {
		return nil, f.releaseErr
	}
	return f.release, nil
}

func (f *fakeUpdater) Fetch(_ context.Context, url string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetches = append(f.fetches, url)
	if err := f.fetchErr[url]; err != nil {
		return nil, err
	}
	body, ok := f.archive[url]
	if !ok {
		return nil, errors.New("no such file")
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

func (f *fakeUpdater) Install(_ context.Context, version string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installs++
	f.installed = version
	return f.installErr
}

// counts is what the feed and the transport were asked, under the lock: a
// check may run on its own goroutine.
func (f *fakeUpdater) counts() (releases int, fetches []string, installs int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.releases, append([]string(nil), f.fetches...), f.installs
}

// fakeRelauncher records that the application was asked to restart.
type fakeRelauncher struct {
	mu       sync.Mutex
	relaunch int
}

func (r *fakeRelauncher) Relaunch() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.relaunch++
}

func (r *fakeRelauncher) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.relaunch
}

// aRelease is a release that names one archive.
func aRelease(t *testing.T, version string, payload []byte) *Release {
	t.Helper()
	release := &Release{
		Version: version,
		Notes:   "the notes of " + version,
		Assets: []Asset{{
			Name: version + ".tar.gz",
			URL:  "https://example.invalid/" + version + ".tar.gz",
			Size: int64(len(payload)),
		}},
	}
	if err := release.Validate(); err != nil {
		t.Fatalf("the fixture release is not one: %v", err)
	}
	return release
}

// serve puts the payload where the release's archive is.
func serve(f *fakeUpdater, release *Release, payload []byte) {
	asset, _ := release.Archive()
	f.archive[asset.URL] = payload
}
