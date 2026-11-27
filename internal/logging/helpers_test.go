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

// testClock 是测试可控的时钟：轮转只取决于文件名中的日期，测试无需等待午夜。
type testClock struct{ at time.Time }

func (c *testClock) now() time.Time { return c.at }

// localDay 返回测试所在时区的一个时刻，日志文件名用的就是这个时区。
func localDay(year int, month time.Month, day, hour int) time.Time {
	return time.Date(year, month, day, hour, 0, 0, 0, time.Local)
}

// newTestLog 为测试打开日志：未指定目录则用独立目录，未指定时钟则固定为 2026-10-15；
// 测试结束时自动关闭。
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

// readLog 以文本形式返回日志文件内容。
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

// keepStandardLoggers 在测试结束后还原进程级日志器：安装日志的测试在运行期间独占整个进程。
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
