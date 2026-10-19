package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"boku/internal/domain"
	"boku/internal/platform/httpc"
	"boku/internal/update"
)

// This file is the whole of how the update feature meets the framework.
//
// internal/update decides what the interface says, when the feed is asked and
// how a download is checked. What it may not do itself is reach MyGo's own
// updater and the release feed: that is where the signed manifests of this
// build are published, and it is adapted here — the one file allowed to import
// everything — exactly like the clipboard device and the login start are.
//
// The split is deliberate, and it is what keeps the feature testable: the
// state machine, the timer and the download are exercised offline, and this
// file, which cannot be exercised without a desktop session, has almost no
// logic of its own.

const (
	// updateCheckTimeout bounds one look at the feed. MyGo's own check has no
	// timeout; a feed that never answers must not keep the "checking" row on
	// the page for the length of the session.
	updateCheckTimeout = 30 * time.Second
	// maxFeedBytes bounds the manifest this file reads. It is MyGo's own limit
	// for the file, so a manifest accepted here is one the framework accepts
	// too.
	maxFeedBytes = 1 << 20
	// updateUserAgent is how Boku asks the feed who it is. GitHub is the one
	// host this application contacts that is not the user's own service, and it
	// can say what is asking.
	updateUserAgent   = "Boku"
	githubReleasesURL = "https://api.github.com/repos/QiYuOr2/boku-desktop/releases?per_page=100"
)

// errFeedMissing says the feed answered 404: no release is published for this
// build yet.
var errFeedMissing = errors.New("the release feed has nothing yet")

// mygoUpdater is update.Updater: MyGo's updater for the two questions it can
// answer, and the release feed itself for the one it cannot.
//
// The framework's Check answers with an update it will install and nothing
// else: it does not hand over the manifest, and the page needs the release's
// version and notes before anything is installed. So the manifest is read here
// — the same signed file, from the same URL the framework compiles into the
// build — and the install is handed back to the framework, which is where the
// archive's signature is checked against the public key in the build.
type mygoUpdater struct {
	client *http.Client
}

// Enabled reports whether this build can check the public update channel.
// Checking is useful to every build, including go run and mygo dev; MyGo's
// own Enabled additionally answers whether this particular executable can be
// replaced in place, which only matters when Install is called.
func (u *mygoUpdater) Enabled() bool { return true }

// Release reads the manifest of this build and turns it into the release the
// interface shows. It is update.Updater.
func (u *mygoUpdater) Release(ctx context.Context, current string) (*update.Release, error) {
	feed, err := u.latestManifest(ctx, releaseFeed())
	if err != nil {
		if errors.Is(err, errFeedMissing) {
			slog.Info("no release is published for this build yet", "version", current)
			return nil, nil
		}
		return nil, err
	}
	return u.release(ctx, feed, current)
}

// githubRelease is the part of GitHub's release list this application needs.
// The selected asset is the signed manifest; its archive URL and signature
// remain inside that manifest, where MyGo verifies them during installation.
type githubRelease struct {
	Draft      bool                 `json:"draft"`
	Prerelease bool                 `json:"prerelease"`
	Assets     []githubReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
}

// latestManifest selects the newest published stable release which carries
// this platform's manifest. If this repository has not made a stable release
// yet, its newest published prerelease is the update channel instead. GitHub's
// /releases/latest endpoint deliberately omits prereleases, so it cannot
// express that fallback.
func (u *mygoUpdater) latestManifest(ctx context.Context, endpoint string) (string, error) {
	payload, err := u.get(ctx, endpoint)
	if err != nil {
		return "", err
	}
	var releases []githubRelease
	if err := json.Unmarshal(payload, &releases); err != nil {
		return "", update.BadRelease("releases", "the release list could not be read")
	}
	name := "update-" + updateTarget() + ".json"
	for _, prerelease := range []bool{false, true} {
		for _, release := range releases {
			if release.Draft || release.Prerelease != prerelease {
				continue
			}
			for _, asset := range release.Assets {
				if asset.Name == name && strings.TrimSpace(asset.DownloadURL) != "" {
					return asset.DownloadURL, nil
				}
			}
		}
	}
	return "", errFeedMissing
}

// release is Release for a feed URL a test can name. A 404 is the answer of a
// release that is not published yet — the ordinary state of a repository
// whose first signed release has not gone out — and it is "up to date"
// rather than a failure: a row saying "检查更新失败" on every launch of a
// version that is new enough would be a warning about nothing.
func (u *mygoUpdater) release(ctx context.Context, feed, current string) (*update.Release, error) {
	payload, err := u.get(ctx, feed)
	if err != nil {
		if errors.Is(err, errFeedMissing) {
			slog.Info("no release is published for this build yet", "version", current)
			return nil, nil
		}
		return nil, err
	}
	release, err := update.DecodeManifest(payload, "")
	if err != nil {
		return nil, err
	}
	slog.Debug("the release feed answered", "version", release.Version, "current", current)
	return release, nil
}

// Fetch opens one of a release's files: the archive internal/update downloads
// and checks before a version is offered. It is update.Updater.
func (u *mygoUpdater) Fetch(ctx context.Context, url string) (io.ReadCloser, error) {
	return u.open(ctx, url)
}

// Install applies the version internal/update downloaded.
//
// It hands the work back to the framework rather than unpacking anything
// itself: the framework checks the archive's Ed25519 signature against the key
// compiled into the build, unpacks it with the layout `mygo build` produced,
// and replaces the installation in place. A second unpacker here would agree
// with it only by luck. It is update.Updater.
func (u *mygoUpdater) Install(ctx context.Context, version string) error {
	found, err := mygo.Updater.Check(ctx)
	if err != nil {
		if errors.Is(err, mygo.ErrUpdatesDisabled) {
			return fmt.Errorf("%w: this build cannot update itself", domain.ErrUpdateUnavailable)
		}
		return fmt.Errorf("%w: the release feed could not be read", domain.ErrUpdateUnavailable)
	}
	if found == nil || strings.TrimPrefix(found.Version, "v") != version {
		// The feed no longer offers the version that was downloaded: what the
		// user was shown and what would be installed are different releases.
		return fmt.Errorf("%w: the feed no longer offers %s", domain.ErrUpdateUnavailable, version)
	}
	if err := found.Install(ctx, nil); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrUpdateUnavailable, err)
	}
	return nil
}

// get reads the manifest of the feed, bounded, and keeps the one answer that
// is not a failure: a 404 means nothing is published for this build yet.
// No error here quotes the URL it was asked for.
func (u *mygoUpdater) get(ctx context.Context, url string) ([]byte, error) {
	body, err := u.open(ctx, url)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	payload, err := io.ReadAll(io.LimitReader(body, maxFeedBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: the release feed stopped answering", domain.ErrUpdateUnavailable)
	}
	if len(payload) > maxFeedBytes {
		return nil, update.BadRelease("body", fmt.Sprintf("the manifest is larger than %d bytes", maxFeedBytes))
	}
	return payload, nil
}

// open is the one network call this file makes. It is the update feature's
// whole way to the network: the request has a context and a timeout, the
// answer is not quoted, and the two statuses the caller distinguishes — a 404
// and everything else — are told apart here.
func (u *mygoUpdater) open(ctx context.Context, url string) (io.ReadCloser, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: the update address cannot be asked", domain.ErrUpdateUnavailable)
	}
	request.Header.Set("User-Agent", updateUserAgent)
	response, err := u.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%w: the check timed out", domain.ErrUpdateUnavailable)
		}
		return nil, fmt.Errorf("%w: the release feed could not be reached", domain.ErrUpdateUnavailable)
	}
	switch {
	case response.StatusCode == http.StatusNotFound:
		response.Body.Close()
		return nil, errFeedMissing
	case response.StatusCode != http.StatusOK:
		response.Body.Close()
		return nil, fmt.Errorf("%w: the release feed answered %d", domain.ErrUpdateUnavailable, response.StatusCode)
	}
	return response.Body, nil
}

// mygoRelauncher is update.Relauncher over mygo.App: the application quits the
// way a quit quits and starts again with the same arguments, which is how the
// version that was just installed takes effect.
type mygoRelauncher struct{}

// Relaunch posts the quit to the main thread and returns.
func (mygoRelauncher) Relaunch() { mygo.App.Relaunch() }

// releaseFeed is the public list of Boku releases. The updater selects a
// release from it rather than using GitHub's /latest redirect, so a repository
// containing only prereleases remains a usable update channel.
func releaseFeed() string {
	return githubReleasesURL
}

// updateTarget names the pair of files `mygo build` publishes for this
// platform: darwin-universal is one bundle for both Mac architectures, and
// every other platform is its own pair.
func updateTarget() string {
	if runtime.GOOS == "darwin" {
		return "darwin-universal"
	}
	return runtime.GOOS + "-" + runtime.GOARCH
}

// newUpdates assembles the update feature: the directory a download is kept
// in, the feed behind the interface internal/update declares, and the relaunch
// that makes an installed update take effect.
//
// It is a function of its own, like services, so that the wiring is read in
// one place and can be checked without a desktop session. The service is not
// started here: main starts it once the application is ready, because a check
// belongs after the tray exists.
func newUpdates(userData, version string) (*update.Service, error) {
	// MyGo's installer has no transport injection point: it uses
	// http.DefaultClient when the user chooses "重启并更新". Install the same
	// proxy-aware transport before MyGo can issue that request, so its archive
	// download follows the exact rule as the check and verification requests.
	http.DefaultTransport = httpc.ProxyTransport()
	svc, err := update.New(update.Options{
		Current: version,
		Dir:     filepath.Join(userData, "updates"),
		Updater: &mygoUpdater{client: &http.Client{
			Timeout:   updateCheckTimeout,
			Transport: httpc.ProxyTransport(),
		}},
		Relauncher: mygoRelauncher{},
	})
	if err != nil {
		return nil, fmt.Errorf("prepare the update service: %w", err)
	}
	return svc, nil
}
