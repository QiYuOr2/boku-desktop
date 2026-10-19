package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"boku/internal/update"
)

// The update assembly is where the feed, the download directory and the
// relaunch are wired together, and where a wrong target name would turn into
// a release nobody can ever find. It is checked here without a desktop
// session, the way services is.
func TestNewUpdatesAssemblesTheFeature(t *testing.T) {
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
}

// The manifest's URL is built from the platform, and the name is the one
// `mygo build` uploads. A target that disagrees with the build's own is a
// feed that answers 404 forever, which looks exactly like "up to date".
func TestReleaseFeedNamesThisBuildsManifest(t *testing.T) {
	feed := releaseFeed()
	want := "https://github.com/QiYuOr2/boku-desktop/releases/latest/download/update-" + updateTarget() + ".json"
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

// A development build cannot update itself, and the check says so quietly.
// This is the one case a test machine can prove without a published release:
// mygo.Updater.Enabled is false unless the binary was built by `mygo build`
// with a feed compiled in.
func TestCheckInADevelopmentBuildIsUnsupported(t *testing.T) {
	svc, err := update.New(update.Options{
		Current: "0.1.0",
		Dir:     t.TempDir(),
		Updater: &mygoUpdater{client: nil},
	})
	if err != nil {
		t.Fatalf("update.New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = svc.Check(ctx)
	if !update.IsUnsupported(err) {
		t.Fatalf("Check = %v, want ErrUpdateUnsupported", err)
	}
	if state := svc.State(); state.Status.String() != "unsupported" {
		t.Errorf("the state after a check = %v, want unsupported", state.Status)
	}
}
