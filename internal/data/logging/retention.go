package logging

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// pruneOldLocked deletes the daily files that have fallen out of the
// retention window. The caller holds the lock.
//
// A failure is reported on the sink rather than returned: there is nobody to
// return it to, and a file that cannot be deleted is not a reason to stop
// logging into the one being written.
func (l *Log) pruneOldLocked() {
	if l.dir == "" || l.retention < 1 {
		return
	}
	if err := prune(l.dir, l.retention, l.now()); err != nil {
		l.reportLocked(err)
	}
}

// prune deletes the daily files older than the window: the day now is in and
// the retention-1 days before it are kept, and anything older goes.
//
// Only files this package names — `boku-<date>.log` — are considered, so a
// directory that holds something else, or a file from a build whose naming
// changed, is left exactly as it is. Every deletion is attempted even when
// one fails: one file held open by a reader must not keep the rest.
func prune(dir string, retention int, now time.Time) error {
	if retention < 1 {
		retention = RetentionDays
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("logging: read %s: %w", dir, err)
	}
	cutoff := startOfDay(now.In(time.Local)).AddDate(0, 0, -(retention - 1))
	var errs []error
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		day, ok := dayFromFileName(entry.Name())
		if !ok || !day.Before(cutoff) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("logging: delete %s: %w", path, err))
		}
	}
	return errors.Join(errs...)
}

// fileName is the name of the file holding the day t is in.
func fileName(t time.Time) string {
	return filePrefix + t.Format(dateLayout) + fileSuffix
}

// dayFromFileName reads the day out of a file this package wrote. A name that
// does not have the shape is not one of ours, and false says so.
func dayFromFileName(name string) (time.Time, bool) {
	if !strings.HasPrefix(name, filePrefix) || !strings.HasSuffix(name, fileSuffix) {
		return time.Time{}, false
	}
	date := strings.TrimSuffix(strings.TrimPrefix(name, filePrefix), fileSuffix)
	day, err := time.ParseInLocation(dateLayout, date, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return day, true
}

// startOfDay is midnight at the beginning of t's own day, which is how two
// log files written on the same date compare as equal.
func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
