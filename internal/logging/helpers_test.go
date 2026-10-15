package logging

import (
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

// testClock is a clock a test decides. The day a file is named after is the
// one thing rotation depends on, and no test waits for midnight.
type testClock struct{ at time.Time }

func (c *testClock) now() time.Time { return c.at }

// localDay is a moment in the test's own time zone, which is the one log file
// names are written in.
func localDay(year int, month time.Month, day, hour int) time.Time {
	return time.Date(year, month, day, hour, 0, 0, 0, time.Local)
}

// newTestLog opens a log for a test: a directory of its own unless one is
// given, and a clock on 2026-10-15 unless the test decides another day. The
// log is closed when the test ends.
func newTestLog(t *testing.T, opts Options) *Log {
	t.Helper()
	if opts.Dir == "" {
		opts.Dir = t.TempDir()
	}
	if opts.Now == nil {
		clock := &testClock{at: localDay(2026, time.October, 15, 9)}
		opts.Now = clock.now
	}
	if opts.Sink == nil {
		opts.Sink = io.Discard
	}
	l, err := Open(opts)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

// readLog returns a log file as text.
func readLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func mustContain(t *testing.T, text, want string) {
	t.Helper()
	if !strings.Contains(text, want) {
		t.Errorf("the log does not have %q in it:\n%s", want, text)
	}
}

func mustNotContain(t *testing.T, text, unwanted string) {
	t.Helper()
	if strings.Contains(text, unwanted) {
		t.Errorf("the log still has %q in it:\n%s", unwanted, text)
	}
}

// keepStandardLoggers puts the process's loggers back the way they were: a
// test that installs a log owns the whole process while it runs.
func keepStandardLoggers(t *testing.T) {
	t.Helper()
	previousLogger := slog.Default()
	previousOut := log.Writer()
	previousFlags := log.Flags()
	previousPrefix := log.Prefix()
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
		log.SetOutput(previousOut)
		log.SetFlags(previousFlags)
		log.SetPrefix(previousPrefix)
	})
}
