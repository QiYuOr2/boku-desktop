package logging

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// exists reports whether a path is there, for the assertions below.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func writeLogFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("a log\n"), fileMode); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// The window is today and the 29 days before it: 2026-09-16 is the oldest day
// that stays when today is 2026-10-15, and one day past it is deleted.
func TestPruneKeepsTheWindowAndDeletesTheRest(t *testing.T) {
	dir := t.TempDir()
	now := localDay(2026, time.October, 15, 12)
	writeLogFile(t, dir, "boku-2026-10-15.log") // today
	writeLogFile(t, dir, "boku-2026-09-16.log") // the last day kept
	writeLogFile(t, dir, "boku-2026-09-15.log") // a day outside the window
	writeLogFile(t, dir, "boku-2026-08-01.log") // far outside it

	if err := prune(dir, RetentionDays, now); err != nil {
		t.Fatalf("prune: %v", err)
	}
	for _, kept := range []string{"boku-2026-10-15.log", "boku-2026-09-16.log"} {
		if !exists(filepath.Join(dir, kept)) {
			t.Errorf("%s was deleted, want it kept", kept)
		}
	}
	for _, gone := range []string{"boku-2026-09-15.log", "boku-2026-08-01.log"} {
		if exists(filepath.Join(dir, gone)) {
			t.Errorf("%s is still there, want it deleted", gone)
		}
	}
}

// Only the files this package names are candidates: a log directory shared
// with something else, or holding a file from a build whose naming changed,
// keeps everything that is not ours.
func TestPruneLeavesWhatItDidNotWrite(t *testing.T) {
	dir := t.TempDir()
	now := localDay(2026, time.October, 15, 12)
	ours := "boku-2026-08-01.log"
	strangers := []string{"other.log", "boku-not-a-day.log", "boku-2026-08-01.log.gz", "notes.txt"}
	for _, name := range strangers {
		writeLogFile(t, dir, name)
	}
	writeLogFile(t, dir, ours)
	// A directory that happens to be named like one of ours is not a file to
	// delete either.
	directory := filepath.Join(dir, "boku-2020-01-01.log")
	if err := os.Mkdir(directory, dirMode); err != nil {
		t.Fatalf("create the directory: %v", err)
	}

	if err := prune(dir, RetentionDays, now); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if exists(filepath.Join(dir, ours)) {
		t.Errorf("%s is still there, want it deleted", ours)
	}
	for _, kept := range append(strangers, "boku-2020-01-01.log") {
		if !exists(filepath.Join(dir, kept)) {
			t.Errorf("%s was deleted, want it left alone", kept)
		}
	}
}

// A caller that names no retention gets the one the application keeps, not an
// empty window: "keep nothing" is never what zero means here.
func TestPruneWithoutARetentionKeepsTheDefault(t *testing.T) {
	dir := t.TempDir()
	now := localDay(2026, time.October, 15, 12)
	writeLogFile(t, dir, "boku-2026-09-16.log") // 29 days back
	writeLogFile(t, dir, "boku-2026-09-15.log") // 30 days back

	if err := prune(dir, 0, now); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if !exists(filepath.Join(dir, "boku-2026-09-16.log")) {
		t.Error("the 29-day-old log was deleted, want it kept")
	}
	if exists(filepath.Join(dir, "boku-2026-09-15.log")) {
		t.Error("the 30-day-old log is still there, want it deleted")
	}
}

func TestPruneReportsADirectoryItCannotRead(t *testing.T) {
	err := prune(filepath.Join(t.TempDir(), "missing"), RetentionDays, time.Now())
	if err == nil {
		t.Fatal("prune read a directory that is not there")
	}
}

// The window is applied when the log opens, so a directory left alone for two
// months is brought back to a month of files at the next launch.
func TestOpenPrunesTheDaysOutsideTheWindow(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "boku-2026-08-01.log")
	writeLogFile(t, dir, "boku-2026-08-01.log")

	clock := &testClock{at: localDay(2026, time.October, 15, 9)}
	newTestLog(t, Options{Dir: dir, Now: clock.now})

	if exists(old) {
		t.Error("the log from two months ago survived the open")
	}
	if !exists(filepath.Join(dir, "boku-2026-10-15.log")) {
		t.Error("the day's file was not created")
	}
}

func TestFileNameAndDayFromFileName(t *testing.T) {
	day := localDay(2026, time.October, 15, 9)
	if got := fileName(day); got != "boku-2026-10-15.log" {
		t.Errorf("fileName() = %q, want boku-2026-10-15.log", got)
	}
	parsed, ok := dayFromFileName("boku-2026-10-15.log")
	if !ok {
		t.Fatal("dayFromFileName refused a name this package writes")
	}
	if want := startOfDay(day); !parsed.Equal(want) {
		t.Errorf("dayFromFileName() = %v, want %v", parsed, want)
	}

	notOurs := []string{
		"",
		"other.log",
		"boku.log",
		"boku-2026-10-15.txt",
		"boku-2026-10-15.log.gz",
		"boku-2026-13-01.log",
		"boku-2026-02-30.log",
	}
	for _, name := range notOurs {
		if _, ok := dayFromFileName(name); ok {
			t.Errorf("dayFromFileName(%q) accepted a name that is not ours", name)
		}
	}
}

// startOfDay is what makes two timestamps on one date compare as the same day.
func TestStartOfDay(t *testing.T) {
	moment := time.Date(2026, time.October, 15, 23, 59, 59, 999, time.Local)
	start := startOfDay(moment)
	if start.Hour() != 0 || start.Minute() != 0 || start.Second() != 0 || start.Nanosecond() != 0 {
		t.Errorf("startOfDay(%v) = %v, want midnight", moment, start)
	}
	if start.Day() != 15 || start.Month() != time.October {
		t.Errorf("startOfDay moved the date: %v", start)
	}
}

// A missing file is not a failure: two runs of prune must not turn into an
// error the second time around.
func TestPruneToleratesAFileThatIsAlreadyGone(t *testing.T) {
	dir := t.TempDir()
	now := localDay(2026, time.October, 15, 12)
	writeLogFile(t, dir, "boku-2026-08-01.log")
	if err := prune(dir, RetentionDays, now); err != nil {
		t.Fatalf("the first prune: %v", err)
	}
	if err := prune(dir, RetentionDays, now); err != nil {
		t.Fatalf("the second prune: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "boku-2026-08-01.log")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the old log is still there: %v", err)
	}
}
