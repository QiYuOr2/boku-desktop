package logging

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// exists 判断路径是否存在，供下方断言使用。
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

// 窗口为今天加之前 29 天：今天为 2026-10-15 时，2026-09-16 是最早保留的一天，再早一天即删除。
func TestPruneKeepsTheWindowAndDeletesTheRest(t *testing.T) {
	dir := t.TempDir()
	now := localDay(2026, time.October, 15, 12)
	writeLogFile(t, dir, "boku-2026-10-15.log") // 今天
	writeLogFile(t, dir, "boku-2026-09-16.log") // 保留的最后一天
	writeLogFile(t, dir, "boku-2026-09-15.log") // 窗口之外的一天
	writeLogFile(t, dir, "boku-2026-08-01.log") // 远在窗口之外

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

// 只有本包命名的文件才是候选：与其它东西共享的日志目录、或来自命名已变的旧构建的文件，
// 都原样保留。
func TestPruneLeavesWhatItDidNotWrite(t *testing.T) {
	dir := t.TempDir()
	now := localDay(2026, time.October, 15, 12)
	ours := "boku-2026-08-01.log"
	strangers := []string{"other.log", "boku-not-a-day.log", "boku-2026-08-01.log.gz", "notes.txt"}
	for _, name := range strangers {
		writeLogFile(t, dir, name)
	}
	writeLogFile(t, dir, ours)
	// 恰好与我们某个文件同名的目录也不是待删除的文件。
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

// 未指定保留天数的调用方得到应用默认值，而非空窗口：这里的零绝不表示「什么都不留」。
func TestPruneWithoutARetentionKeepsTheDefault(t *testing.T) {
	dir := t.TempDir()
	now := localDay(2026, time.October, 15, 12)
	writeLogFile(t, dir, "boku-2026-09-16.log") // 29 天前
	writeLogFile(t, dir, "boku-2026-09-15.log") // 30 天前

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

// 窗口在打开日志时套用，因此闲置两个月的目录会在下次启动时被收回到一个月内。
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

// startOfDay 使同一日期的两个时间戳比较为同一天。
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

// 文件不存在不算失败：两次 prune 不能在第二轮变成错误。
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
