package logging

import (
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOpenWritesTheDaysFile(t *testing.T) {
	dir := t.TempDir()
	clock := &testClock{at: localDay(2026, time.October, 15, 9)}
	var sink strings.Builder
	l := newTestLog(t, Options{Dir: dir, Sink: &sink, Now: clock.now})

	want := filepath.Join(dir, "boku-2026-10-15.log")
	if got := l.File(); got != want {
		t.Errorf("File() = %q, want %q", got, want)
	}

	l.Logger().Info("boku started", "version", "0.1.0")
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	text := readLog(t, want)
	mustContain(t, text, "level=INFO")
	mustContain(t, text, `msg="boku started"`)
	mustContain(t, text, "version=0.1.0")
	// The sink gets what the file gets, which is what keeps the terminal of
	// `go tool mygo dev` as informative as it was before there was a file.
	mustContain(t, sink.String(), "boku started")
}

func TestTheLogRotatesWhenTheDayChanges(t *testing.T) {
	dir := t.TempDir()
	clock := &testClock{at: localDay(2026, time.October, 15, 23)}
	l := newTestLog(t, Options{Dir: dir, Now: clock.now})
	logger := l.Logger()

	logger.Info("before midnight")
	clock.at = localDay(2026, time.October, 16, 0)
	logger.Info("after midnight")

	first := readLog(t, filepath.Join(dir, "boku-2026-10-15.log"))
	mustContain(t, first, "before midnight")
	mustNotContain(t, first, "after midnight")

	second := readLog(t, filepath.Join(dir, "boku-2026-10-16.log"))
	mustContain(t, second, "after midnight")
	mustNotContain(t, second, "before midnight")

	if want := filepath.Join(dir, "boku-2026-10-16.log"); l.File() != want {
		t.Errorf("File() = %q, want %q", l.File(), want)
	}
}

func TestLevelDropsRecordsBelowIt(t *testing.T) {
	dir := t.TempDir()
	l := newTestLog(t, Options{Dir: dir, Level: slog.LevelWarn})
	logger := l.Logger()

	if logger.Enabled(t.Context(), slog.LevelInfo) {
		t.Error("an info record is enabled at level warn")
	}
	logger.Debug("a debug record")
	logger.Info("an info record")
	logger.Warn("a warning")
	logger.Error("a failure")

	text := readLog(t, filepath.Join(dir, "boku-2026-10-15.log"))
	mustContain(t, text, "a warning")
	mustContain(t, text, "a failure")
	mustNotContain(t, text, "a debug record")
	mustNotContain(t, text, "an info record")
}

// A log with nowhere to write a file is what a test and an assembly without a
// user directory get: the records still exist, they just have no file.
func TestWithoutADirectoryOnlyTheSinkHearsTheRecords(t *testing.T) {
	var sink strings.Builder
	l, err := Open(Options{Sink: &sink})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if l.File() != "" {
		t.Errorf("File() = %q, want no file", l.File())
	}
	l.Logger().Warn("nowhere to put this")
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	mustContain(t, sink.String(), "nowhere to put this")
}

func TestOpenRefusesADirectoryItCannotUse(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "logs")
	if err := os.WriteFile(blocked, []byte("not a directory"), fileMode); err != nil {
		t.Fatalf("prepare the file: %v", err)
	}
	if _, err := Open(Options{Dir: blocked, Sink: io.Discard}); err == nil {
		t.Fatal("Open accepted a file as its log directory")
	}
}

func TestRecordsAfterCloseAreDropped(t *testing.T) {
	dir := t.TempDir()
	l := newTestLog(t, Options{Dir: dir})
	l.Logger().Info("written")
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Neither of these may panic, and nothing more may reach the file.
	l.Logger().Error("after the close")
	if err := l.Close(); err != nil {
		t.Fatalf("the second Close: %v", err)
	}

	text := readLog(t, filepath.Join(dir, "boku-2026-10-15.log"))
	mustContain(t, text, "written")
	mustNotContain(t, text, "after the close")
}

// Several goroutines logging at once is the normal state of this application:
// the panel refreshes, the clipboard poll and the window all write. Each
// record is one Write under the lock, so the file holds every line whole.
func TestConcurrentRecordsAllReachTheFile(t *testing.T) {
	dir := t.TempDir()
	l := newTestLog(t, Options{Dir: dir})
	logger := l.Logger()

	const writers, each = 8, 32
	var group sync.WaitGroup
	for writer := range writers {
		group.Add(1)
		go func() {
			defer group.Done()
			for n := range each {
				logger.Info("a record", "writer", writer, "n", n)
			}
		}()
	}
	group.Wait()
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	text := readLog(t, filepath.Join(dir, "boku-2026-10-15.log"))
	if got := strings.Count(text, "msg=\"a record\""); got != writers*each {
		t.Errorf("the file holds %d records, want %d:\n%s", got, writers*each, text)
	}
}

// Install is what puts main.go's `log.Fatalf` and slog's default logger in the
// file: both are process-wide, and both have to end up here.
func TestInstallSendsTheProcessLoggersHere(t *testing.T) {
	keepStandardLoggers(t)
	dir := t.TempDir()
	l := newTestLog(t, Options{Dir: dir})
	l.Install()

	slog.Info("through the default logger")
	//nolint:staticcheck // the standard logger is what main.go's fatals use.
	log.Print("boku: could not open the credential store")

	text := readLog(t, filepath.Join(dir, "boku-2026-10-15.log"))
	mustContain(t, text, "through the default logger")
	mustContain(t, text, "could not open the credential store")
}

func TestLevelFromEnv(t *testing.T) {
	cases := map[string]slog.Level{
		"":         slog.LevelInfo,
		"   ":      slog.LevelInfo,
		"info":     slog.LevelInfo,
		"debug":    slog.LevelDebug,
		"DEBUG":    slog.LevelDebug,
		" warn ":   slog.LevelWarn,
		"warning":  slog.LevelWarn,
		"error":    slog.LevelError,
		"verbose":  slog.LevelInfo,
		"critical": slog.LevelInfo,
	}
	for value, want := range cases {
		t.Setenv(LevelEnv, value)
		if got := LevelFromEnv(); got != want {
			t.Errorf("LevelFromEnv() with %q = %v, want %v", value, got, want)
		}
	}
}
