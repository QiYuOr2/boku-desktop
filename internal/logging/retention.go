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

// pruneOldLocked 删除已出保留窗口的每日文件。调用方持有锁。
//
// 失败在 sink 上报告而非返回：没有可返回的对象，且一个删不掉的文件不该阻止继续写入当前文件。
func (l *Log) pruneOldLocked() {
	if l.dir == "" || l.retention < 1 {
		return
	}
	if err := prune(l.dir, l.retention, l.now()); err != nil {
		l.reportLocked(err)
	}
}

// prune 删除早于窗口的每日文件：当前所在的日期加之前 retention-1 天保留，更早的删除。
//
// 只考虑本包命名的文件 `boku-<date>.log`，因此存有其它内容的目录、或命名已变的旧构建文件
// 都原样保留。即使某个删除失败也继续处理其余文件：一个被读取方占用的文件不能拖累其它文件。
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

// fileName 是包含时刻 t 的那一天的文件名。
func fileName(t time.Time) string {
	return filePrefix + t.Format(dateLayout) + fileSuffix
}

// dayFromFileName 从本包写出的文件名中读出日期。形状不符的名字不属于本包，返回 false。
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

// startOfDay 是 t 所在那一天开始的午夜，使同日期写出的两个日志文件比较时相等。
func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
