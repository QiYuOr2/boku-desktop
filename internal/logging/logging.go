// Package logging writes the application's log to a file per day under the
// user data directory and keeps the last RetentionDays of them.
//
// It exists because Boku is a tray icon with no console: an error the user
// cannot see is an error nobody can act on, and the known-issues notes ask for
// facts before a guess. The log is that record.
//
// Only main.go uses this package, and it uses it once. Everything else logs
// through the standard library's log/slog, which Install points here: the log
// is one process-wide facility, so no layer has to take a logger as an
// argument to get one — and no layer gains a dependency it did not have.
package logging

import (
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// RetentionDays is how many days of logs are kept: today, and the 29
	// days before it. It is the number the user asked for — "keep about a
	// month" — and not a size: a day of this application's records is a few
	// kilobytes, while a size ceiling would silently drop the oldest hours
	// of the day a problem started in.
	RetentionDays = 30

	// filePrefix and fileSuffix make a file this package's own: the name
	// says which day it holds, and scanning the directory tells the
	// retention which files to consider at all.
	filePrefix = "boku-"
	fileSuffix = ".log"
	// dateLayout is the day in a file name. It is the local date, because
	// the person reading the file is sitting in that time zone.
	dateLayout = "2006-01-02"

	fileMode = 0o600
	dirMode  = 0o700

	// LevelEnv names the variable that decides how much is written. It is
	// how a user or a developer turns on the details for one run without a
	// rebuild and without a settings page.
	LevelEnv = "BOKU_LOG_LEVEL"
	// DirEnv names the variable that moves the logs elsewhere than the user
	// data directory, which is what a support run wants: the file is then
	// somewhere that can be handed over whole.
	DirEnv = "BOKU_LOG_DIR"
)

// Options configures a Log. The zero value is usable: stderr alone, Info and
// above, no file.
type Options struct {
	// Dir is where the daily files are written. An empty Dir means there is
	// nowhere to write one — a test, or an assembly without a user
	// directory — and the log is then the Sink alone.
	Dir string
	// Level is the lowest level that is written.
	Level slog.Level
	// Retention is how many days are kept. Zero means RetentionDays.
	Retention int
	// Sink also receives every record, next to the file. It is what keeps
	// `go tool mygo dev`'s terminal as informative as it was before there
	// was a file. Nil is the process's stderr; io.Discard turns the copy off.
	Sink io.Writer
	// Now is the clock, so that a test can decide what day it is.
	Now func() time.Time
}

// Log is the application's log: a file per day, pruned to the retention
// window, plus a copy to the sink.
//
// It is an io.Writer so that slog's own handlers can write to it, and it is
// safe for use from several goroutines: every record goes through one lock,
// which also keeps a rotation from happening in the middle of a line.
type Log struct {
	dir       string
	level     slog.Level
	retention int
	now       func() time.Time
	sink      io.Writer

	mu     sync.Mutex
	file   *os.File
	path   string
	day    string
	closed bool
}

// Open prepares the log: it creates the directory, opens the day's file and
// deletes the files that have fallen out of the window.
//
// It reports an error only when there is a directory to write to and that
// directory cannot be used. A caller that gets one should say so and carry on
// without a file: not being able to write a log is one way fewer to diagnose
// the application, not a reason for it not to start.
func Open(opts Options) (*Log, error) {
	l := &Log{
		dir:       strings.TrimSpace(opts.Dir),
		level:     opts.Level,
		retention: opts.Retention,
		now:       opts.Now,
		sink:      opts.Sink,
	}
	if l.retention < 1 {
		l.retention = RetentionDays
	}
	if l.now == nil {
		l.now = time.Now
	}
	if l.sink == nil {
		l.sink = os.Stderr
	}
	if l.dir == "" {
		return l, nil
	}
	if err := os.MkdirAll(l.dir, dirMode); err != nil {
		return nil, fmt.Errorf("logging: create %s: %w", l.dir, err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.openFileLocked(); err != nil {
		return nil, err
	}
	return l, nil
}

// Handler is the handler this log writes through: records below the level are
// dropped, and everything that is written is scrubbed first (redact.go).
func (l *Log) Handler() slog.Handler {
	return &redacting{next: slog.NewTextHandler(l, &slog.HandlerOptions{
		Level: l.level,
		// The source is the one attribute that turns "something failed" into
		// a place to look. A record that came through the standard library
		// logger points at the shim, not at its caller.
		AddSource: true,
	})}
}

// Logger is a logger that writes here.
func (l *Log) Logger() *slog.Logger { return slog.New(l.Handler()) }

// Install makes this log the process's own: slog's default logger and the
// standard library's log both write here. main.go calls it once, right after
// Open, which is what puts `log.Fatalf` and the tray's debug line in the file
// with everything else.
func (l *Log) Install() {
	slog.SetDefault(l.Logger())
	// The standard logger's own timestamp and prefix would be a second one
	// on a record that already has both.
	shim := slog.NewLogLogger(l.Handler(), slog.LevelInfo)
	shim.SetFlags(0)
	shim.SetPrefix("")
	log.SetOutput(shim.Writer())
	log.SetFlags(0)
	log.SetPrefix("")
}

// File is the path of the day's file, or "" when there is nowhere to write
// one. It is what tells the user which file to look at.
func (l *Log) File() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.path
}

// Close flushes and closes the day's file. Records after it are dropped
// rather than written to a closed file, and closing twice is not an error.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	if l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	// Flushing before closing is what makes the last lines of a run — the
	// ones a crash is about — survive the process.
	return errors.Join(file.Sync(), file.Close())
}

// Write takes one record from a slog handler. It rotates the file when the
// day has changed, and copies the record to the sink.
//
// It never reports an error: a handler that returns one has nothing better to
// do with it, and the failure has already been written to the sink, which is
// the place a person can still see it.
func (l *Log) Write(record []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return len(record), nil
	}
	if err := l.openFileLocked(); err != nil {
		l.reportLocked(err)
	}
	if l.file != nil {
		if _, err := l.file.Write(record); err != nil {
			l.reportLocked(fmt.Errorf("logging: write %s: %w", l.path, err))
		}
	}
	if l.sink != nil {
		_, _ = l.sink.Write(record)
	}
	return len(record), nil
}

// openFileLocked points the log at the file of the day it is now, opening it
// when that is not the file already open, and prunes the window whenever a
// file is opened — at startup, and at the first record after midnight.
//
// The caller holds the lock. A failure leaves the log without a file, and the
// next record tries again, so a directory that was not writable at midnight
// (a syncing cloud folder, a full disk) is not fatal for the rest of the day.
func (l *Log) openFileLocked() error {
	if l.dir == "" {
		return nil
	}
	day := l.now().Format(dateLayout)
	if l.file != nil && day == l.day {
		return nil
	}
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
	path := filepath.Join(l.dir, fileName(l.now()))
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode)
	if err != nil {
		return fmt.Errorf("logging: open %s: %w", path, err)
	}
	l.file, l.path, l.day = file, path, day
	l.pruneOldLocked()
	return nil
}

// reportLocked says what went wrong on the sink, which is the only place left
// once the file is the thing that failed. The caller holds the lock.
func (l *Log) reportLocked(err error) {
	if l.sink == nil || err == nil {
		return
	}
	_, _ = fmt.Fprintf(l.sink, "%s: %v\n", time.Now().Format(time.RFC3339), err)
}

// LevelFromEnv reads LevelEnv: debug, info, warn or error, in any case.
// Anything else — an empty variable, a typo — is Info, which is what a log
// nobody configured should be.
func LevelFromEnv() slog.Level {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(LevelEnv))) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
