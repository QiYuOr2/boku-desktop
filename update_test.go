package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// The update assembly is where the feed, the download directory and the
// relaunch are wired together, and where a wrong target name would turn into
// a release nobody can ever find. It is checked here without a desktop
// session, the way services is.
func TestNewUpdatesAssemblesTheFeature(t *testing.T) {
	previousTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	userData := t.TempDir()
	svc, err := newUpdates(userData, "0.1.0-beta.5")
	if err != nil {
		t.Fatalf("newUpdates: %v", err)
	}
	if svc == nil {
		t.Fatal("newUpdates returned no service")
	}
	// Nothing has been asked yet: the state is Idle and the directory is the
	// one under the user data the caller named.
	if state := svc.State(); state.Status != 0 {
		t.Errorf("the fresh state = %v, want idle", state.Status)
	}
	if _, err := os.Stat(filepath.Join(userData, "updates")); err == nil {
		t.Error("the download directory was created before anything was downloaded")
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok || transport.Proxy == nil {
		t.Error("newUpdates did not give MyGo's default client the proxy-aware transport")
	}
}

// The manifest's URL is built from the platform, and the name is the one
// `mygo build` uploads. A target that disagrees with the build's own is a
// feed that answers 404 forever, which looks exactly like "up to date".
func TestReleaseFeedNamesThisBuildsManifest(t *testing.T) {
	feed := releaseFeed()
	want := githubReleasesURL
	if feed != want {
		t.Errorf("releaseFeed() = %q, want %q", feed, want)
	}
	if target := updateTarget(); target == "" {
		t.Error("updateTarget() is empty")
	}
}

// assembling the service gives the presentation something it accepts: a
// *update.Service satisfies app.UpdateService, and a nil one is what a build
// without the feature passes.
func TestUpdateServiceIsWhatThePresentationAccepts(t *testing.T) {
	svc, err := newUpdates(t.TempDir(), "0.1.0")
	if err != nil {
		t.Fatalf("newUpdates: %v", err)
	}
	if got := updateService(svc); got == nil {
		t.Fatal("the assembled service is nil to the presentation")
	}
	if got := updateService(nil); got != nil {
		t.Error("a build without updates passes a non-nil service")
	}
}

func TestLatestManifestPrefersStableAndFallsBackToPrerelease(t *testing.T) {
	manifestName := "update-" + updateTarget() + ".json"
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			fmt.Fprintf(w, `[
				{"prerelease":true,"assets":[{"name":%q,"browser_download_url":%q}]},
				{"prerelease":false,"assets":[{"name":%q,"browser_download_url":%q}]}
			]`, manifestName, server.URL+"/beta.json", manifestName, server.URL+"/stable.json")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	u := &mygoUpdater{client: server.Client()}
	got, err := u.latestManifest(context.Background(), server.URL+"/releases")
	if err != nil {
		t.Fatalf("latestManifest: %v", err)
	}
	if want := server.URL + "/stable.json"; got != want {
		t.Errorf("latestManifest = %q, want %q", got, want)
	}
}

func TestLatestManifestUsesPrereleaseWhenNoStableReleaseExists(t *testing.T) {
	manifestName := "update-" + updateTarget() + ".json"
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `[{"prerelease":true,"assets":[{"name":%q,"browser_download_url":%q}]}]`, manifestName, server.URL+"/beta.json")
	}))
	defer server.Close()

	u := &mygoUpdater{client: server.Client()}
	got, err := u.latestManifest(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("latestManifest: %v", err)
	}
	if want := server.URL + "/beta.json"; got != want {
		t.Errorf("latestManifest = %q, want %q", got, want)
	}
}
