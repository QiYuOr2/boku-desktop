package update

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"boku/internal/domain"
)

// installTimeout bounds how long the updater is given to replace the
// installation: long enough for an archive that has to be unpacked, short
// enough that a filesystem that hangs does not leave the offer standing for
// the rest of the run.
const installTimeout = 5 * time.Minute

// Install applies a ready update and restarts the application.
//
// What it does and does not verify is worth being plain about. This package
// downloads the release's archive, checks that it arrived whole and — when
// the feed can say — that it matches the SHA-256 the manifest signs, and
// shows the user that a version is ready. It does not apply the archive: the
// install itself is the updater's, through Updater.Install, because that is
// where the Ed25519 signature is checked against the public key compiled
// into the release, and the archive is unpacked with the layout `mygo build`
// produced. A second unpacker here would agree with the first only by luck.
//
// The order is the one that leaves a working application behind. The
// updater replaces the installation first and only then is anything
// restarted; a replace that fails leaves the running version untouched and
// the state says so. A replace that succeeds and a restart that cannot be
// started leaves the new version installed for the next launch. A nil
// Relauncher is that last case — installed, not restarted — and is not an
// error: the interface says "takes effect at the next launch" rather than
// offering a button that does nothing.
//
// It is safe to call from a goroutine: it reads and changes the state under
// the service's lock and does its I/O outside it. It must not be called from
// the drawing thread, which is what InstallAsync is for.
func (s *Service) Install(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	state := s.state
	release := s.release
	s.mu.Unlock()
	if state.Status != domain.UpdateReady || release == nil {
		return fmt.Errorf("%w: there is no downloaded update to install", domain.ErrUpdateUnavailable)
	}
	version := release.Version

	slog.Info("installing the update", "version", version)
	installCtx, cancel := context.WithTimeout(ctx, installTimeout)
	err := s.updater.Install(installCtx, version)
	cancel()

	// Whatever happened, this release is no longer something to offer: it
	// has been applied, or it has not and the update is not going to be
	// installed from here. Leaving Ready would offer a restart that does
	// nothing.
	s.mu.Lock()
	s.release = nil
	s.mu.Unlock()
	if err != nil {
		s.set(domain.UpdateState{
			Status:  domain.UpdateFailed,
			Version: version,
			Detail:  "the update could not be installed",
		})
		// The updater's own message can name the URL it was fetching, so it
		// is not repeated here.
		slog.Warn("the update could not be installed", "version", version)
		return wrapUnavailable("the update could not be installed", err)
	}

	if s.relaunch == nil {
		s.set(domain.UpdateState{
			Status:  domain.UpdateUpToDate,
			Version: version,
			Detail:  "the update is installed and takes effect at the next launch",
		})
		return nil
	}
	// The archive is the last thing that could restore the replaced
	// installation, so it stays on disk until the next launch collects it.
	s.set(domain.UpdateState{
		Status:  domain.UpdateUpToDate,
		Version: version,
		Detail:  "the update is installed; restarting",
	})
	// Relaunch posts the quit to the main thread and returns: nothing after
	// this line is needed, and it must be the last thing this method does.
	s.relaunch.Relaunch()
	return nil
}

// InstallAsync is Install on a goroutine of its own, reporting to done. It is
// what a click on "restart and update" calls: the click is on the main
// thread, and replacing an installation is I/O that must not hold it. A nil
// done is not an error.
func (s *Service) InstallAsync(ctx context.Context, done func(error)) {
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		err := s.Install(ctx)
		if done != nil {
			done(err)
		}
	}()
}

// entriesUnder lists the names in a directory, which is what a test counts to
// know that a download was made or a directory was collected. A missing
// directory is an empty list.
func entriesUnder(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
