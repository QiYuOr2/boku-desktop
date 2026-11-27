// Package logging 把应用日志按天写到用户数据目录下的文件，并保留最近 RetentionDays 份。
//
// Boku 是常驻托盘的图标应用，没有控制台：用户看不到的错误就等于无人能处理的错误，
// 已知问题排查需要事实依据而非猜测。日志就是这个记录。
//
// 只有 main.go 使用本包，且只用一次。其它部分都通过标准库 log/slog 打日志，由 Install
// 指向这里：日志是进程级设施，各层无需把 logger 当参数传递，也不会因此多出依赖。
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
	// RetentionDays 是保留的天数：今天加之前 29 天。选择按天数而非按体积上限，
	// 因为本应用一天的记录只有几 KB，而体积上限会在问题发生当天悄悄丢掉最早的时段。
	RetentionDays = 30

	// filePrefix 与 fileSuffix 使文件归本包所有：名字标明其日期，扫描目录即可
	// 判断保留策略要处理哪些文件。
	filePrefix = "boku-"
	fileSuffix = ".log"
	// dateLayout 是文件名中的日期格式。用本地日期，因为阅读者正处于该时区。
	dateLayout = "2006-01-02"

	fileMode = 0o600
	dirMode  = 0o700

	// LevelEnv 指定日志级别，用户或开发者无需重新构建、也无需设置页即可为某次运行打开详细日志。
	LevelEnv = "BOKU_LOG_LEVEL"
	// DirEnv 把日志移到用户数据目录之外，便于支持排查时整体交出文件。
	DirEnv = "BOKU_LOG_DIR"
)

// Options 配置 Log。零值可用：仅输出到 stderr、Info 及以上、无文件。
type Options struct {
	// Dir 是每日文件的写入目录。为空表示无处可写（测试或无用户目录的装配），此时日志只有 Sink。
	Dir string
	// Level 是写入的最低级别。
	Level slog.Level
	// Retention 是保留天数。零表示 RetentionDays。
	Retention int
	// Sink 在文件之外同时接收每条记录，使 dev 终端输出与有文件之前一样详尽。
	// 为 nil 时用进程 stderr；io.Discard 关闭这份副本。
	Sink io.Writer
	// Now 是时钟，便于测试决定当天日期。
	Now func() time.Time
}

// Log 是应用日志：每日一个文件、剪枝到保留窗口，并复制一份到 sink。
//
// 它实现 io.Writer，供 slog 处理器写入；可被多个 goroutine 并发使用：每条记录经同一把锁，
// 也保证轮转不会发生在一行中间。
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

// Open 准备日志：创建目录、打开当天文件、删除已出窗口的文件。
//
// 只有当有可写目录且该目录无法使用时才返回错误。调用方应报告错误并继续以无文件方式运行：
// 写不了日志只是少一种诊断手段，不是阻止应用启动的理由。
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

// Handler 是本日志写入所用的处理器：低于级别的记录被丢弃，写出的内容先经脱敏（见 redact.go）。
func (l *Log) Handler() slog.Handler {
	return &redacting{next: slog.NewTextHandler(l, &slog.HandlerOptions{
		Level: l.level,
		// 来源信息是把「出错了」变成「去这里查」的唯一属性。经标准库日志器产生的记录
		// 指向 shim，而非其调用方。
		AddSource: true,
	})}
}

// Logger 返回一个写入本日志的日志器。
func (l *Log) Logger() *slog.Logger { return slog.New(l.Handler()) }

// Install 使本日志成为进程自身的日志：slog 默认日志器与标准库 log 都写入这里。
// main.go 在 Open 之后调用一次，使 log.Fatalf 与托盘的调试输出也和其它内容一起进入文件。
func (l *Log) Install() {
	slog.SetDefault(l.Logger())
	// 标准库日志器自带的时间戳与前缀会与记录已有的重复。
	shim := slog.NewLogLogger(l.Handler(), slog.LevelInfo)
	shim.SetFlags(0)
	shim.SetPrefix("")
	log.SetOutput(shim.Writer())
	log.SetFlags(0)
	log.SetPrefix("")
}

// File 返回当天文件的路径，无处可写时为空。用户据此知道该看哪个文件。
func (l *Log) File() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.path
}

// Close 刷新并关闭当天文件。之后的记录被丢弃而非写入已关闭的文件，重复关闭不报错。
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	if l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	// 先刷新再关闭，才能让崩溃前最后几行——正是排查关心的那几行——存活下来。
	return errors.Join(file.Sync(), file.Close())
}

// Write 接收 slog 处理器的一条记录：日期变化时轮转文件，并把记录复制到 sink。
//
// 它从不返回错误：处理器拿到错误也做不了什么，而失败已写入 sink——人还能看到的地方。
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

// openFileLocked 把日志指向当前日期对应的文件，若与已打开的文件不同则打开，并在每次打开文件时
// （启动时、午夜后第一条记录时）剪枝保留窗口。
//
// 调用方持有锁。失败会让日志暂时没有文件，下一条记录会重试，因此午夜时不可写的目录
// （如同步中的云盘、磁盘已满）不至于让全天都写不了日志。
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

// reportLocked 在 sink 上报告错误——当文件本身出错时，这是唯一剩下的地方。调用方持有锁。
func (l *Log) reportLocked(err error) {
	if l.sink == nil || err == nil {
		return
	}
	_, _ = fmt.Fprintf(l.sink, "%s: %v\n", time.Now().Format(time.RFC3339), err)
}

// LevelFromEnv 读取 LevelEnv：debug、info、warn 或 error，大小写不敏感。
// 其它值（空变量、笔误）一律为 Info，即未配置时的默认值。
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
