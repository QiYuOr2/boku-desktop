package update

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"boku/internal/domain"
)

const (
	// FirstCheckDelay 是启动后到首次询问发布源的等待。启动本身已很忙（窗口、
	// 快捷键、剪贴板轮询），没人盯着的启动最容易被觉得慢。
	FirstCheckDelay = time.Minute
	// CheckInterval 是之后的询问间隔。桌面应用运行数周，对发布不急的程序一天
	// 一次足够。
	CheckInterval = 24 * time.Hour
)

// Options 是装配服务所需的配置。Updater 必填，其余有可用默认值。
type Options struct {
	// Current 是运行版本，按其与发布源比较：mygo.App.Version 报告的版本，
	// 为空时用构建期版本。
	Current string
	// Dir 是服务在两次检查之间存放东西的目录。必填，因为它会被清理：更新由
	// updater 下载到自己的目录安装，所以这里留下的都是残留，只删不留。
	Dir string
	// Updater 是发布源。必填：没有发布源就无从检查，在装配时报错胜过永远不动
	// 的状态。
	Updater Updater
	// Relauncher 在应用更新后重启应用。为 nil 时只安装不重启：下次启动生效，
	// 状态会如实说明。
	Relauncher Relauncher
	// FirstDelay 与 Interval 覆盖调度。零值用默认；负的 FirstDelay 表示循环在
	// 间隔到期前不自行检查，供自己驱动 Check 的测试使用。
	FirstDelay time.Duration
	Interval   time.Duration
	// Clock 是服务读时间的地方，仅用于给检查时间打戳（无行为依赖），nil 用系统时钟。
	Clock domain.Clock
	// OnChange 由改变状态的 goroutine 在每次变更后、状态可读时调用一次。它是
	// 让视图重绘的方式，因此不得阻塞——它只是重绘请求，重绘自身会读 State()。
	OnChange func()
}

// Service 是更新功能：决定界面画什么、何时询问发布源、安装意味着什么。
//
// 每个方法都可在任意 goroutine 调用；State 足够轻，可从绘制线程调用（一次
// 加锁、一次拷贝）。
type Service struct {
	current  string
	dir      string
	updater  Updater
	relaunch Relauncher
	firstIn  time.Duration
	interval time.Duration
	now      func() time.Time

	mu       sync.Mutex
	state    domain.UpdateState
	onChange func()
	// running 表示有 Check 在进行，即单飞：进行中到达的 Check 立即返回，不再
	// 询问发布源第二次。
	running    bool
	installing bool
	// release 是 Ready 状态对应的 release，整份保留以便 Install 知道安装什么。
	release *Release
	// checkedAt 是上次检查的开始时间，循环据此把自身保持为每间隔一次。
	checkedAt time.Time

	// cancel 与 done 是运行中的循环。
	cancel context.CancelFunc
	done   chan struct{}
}

// New 返回服务。
func New(opts Options) (*Service, error) {
	if opts.Updater == nil {
		return nil, fmt.Errorf("%w: the update service has no updater", domain.ErrInvalidArgument)
	}
	if opts.Dir == "" {
		return nil, fmt.Errorf("%w: the update service has no directory", domain.ErrInvalidArgument)
	}
	if !filepath.IsAbs(opts.Dir) {
		return nil, fmt.Errorf("%w: the update directory must be an absolute path", domain.ErrInvalidArgument)
	}
	s := &Service{
		current:  opts.Current,
		dir:      opts.Dir,
		updater:  opts.Updater,
		relaunch: opts.Relauncher,
		firstIn:  opts.FirstDelay,
		interval: opts.Interval,
		now:      domain.SystemClock{}.Now,
		onChange: opts.OnChange,
		state:    domain.UpdateState{Status: domain.UpdateIdle},
	}
	if opts.Clock != nil {
		s.now = opts.Clock.Now
	}
	if opts.FirstDelay == 0 {
		s.firstIn = FirstCheckDelay
	}
	if opts.Interval <= 0 {
		s.interval = CheckInterval
	}
	return s, nil
}

// State 是界面要画的状态，为普通拷贝，可安全保留一帧。
func (s *Service) State() domain.UpdateState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// SetOnChange 替换回调。装配在服务之后才构建窗口、而重绘又需要窗口时用它。
func (s *Service) SetOnChange(onChange func()) {
	s.mu.Lock()
	s.onChange = onChange
	s.mu.Unlock()
}

// OnChange 与 SetOnChange 相同。两个名字合一，是因为展示层声明它需要服务的
// State、Check、Install 与 OnChange，这里正是该声明的满足。它在服务使用前
// 安装一次。
func (s *Service) OnChange(onChange func()) { s.SetOnChange(onChange) }

// set 替换状态并通知视图（无变化时除外）。它是状态迁移的唯一入口，故 OnChange
// 每次变更只调一次，且总在新状态可读之后。
func (s *Service) set(state domain.UpdateState) {
	s.mu.Lock()
	if s.state == state {
		s.mu.Unlock()
		return
	}
	s.state = state
	onChange := s.onChange
	s.mu.Unlock()
	// 回调在锁内读取、锁外调用：它最终落在主线程，若持锁等待绘制线程调用
	// State，就是等待时机的死锁。
	if onChange != nil {
		onChange()
	}
}

// Start 启动后台循环：FirstDelay 后检查一次，此后每 Interval 一次。不能自更新
// 的构建在此标记为 Unsupported 且不建定时器：醒来只为反复学到同一件事只是噪音。
// 可重复调用，第二次无操作。
func (s *Service) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !s.updater.Enabled() {
		s.set(domain.UpdateState{Status: domain.UpdateUnsupported})
		return
	}
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.cancel, s.done = cancel, done
	firstIn, interval := s.firstIn, s.interval
	s.mu.Unlock()

	slog.Info("the update check started",
		"firstIn", firstIn.String(),
		"interval", interval.String(),
		"version", s.current,
	)

	go func() {
		defer close(done)
		s.loop(runCtx, firstIn, interval)
	}()
}

// loop 是定时器：delay 后检查一次，此后每 interval 一次，直到 context 结束。
// delay 不高于零表示循环等满一个 interval 才首次检查，供自己驱动 Check 的测试使用。
func (s *Service) loop(ctx context.Context, delay, interval time.Duration) {
	timer := time.NewTimer(max(delay, 0))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		// 检查在循环 goroutine 上执行。若已有检查在跑（比如按钮触发的），本次
		// 调用立即返回，下一轮等满间隔——这就是单飞。
		_ = s.Check(ctx)
		timer.Reset(interval)
	}
}

// Stop 结束循环并等待其 goroutine。进行中的检查随启动它的 context 取消，因为
// 循环的 context 是它的父。main.go 在应用退出时调用。
func (s *Service) Stop() {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.cancel, s.done = nil, nil
	s.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	if done != nil {
		<-done
	}
	slog.Info("the update check stopped")
}

// Check 询问发布源一次，有新版本时下载。状态依次为 Checking，再到 UpToDate、
// Ready 或 Failed；不能自更新的构建进入 Unsupported 并返回
// domain.ErrUpdateUnsupported，不算失败。
//
// 在另一次检查运行时到达的 Check 立即以空错误返回：调用方要的答案正在写入
// 状态，询问两次只会浪费两个请求去得知同一件事。
func (s *Service) Check(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	if !s.updater.Enabled() {
		s.mu.Unlock()
		// 不打 warn：开发构建问了也不会有结果，每次启动都记一条正是本功能
		// 要从日志里排除的噪音。
		s.set(domain.UpdateState{Status: domain.UpdateUnsupported})
		return domain.ErrUpdateUnsupported
	}
	s.running = true
	s.checkedAt = s.now()
	// 新一次检查重新作答：答案未知期间先前就绪的版本不再提供。若判定当前版本
	// 已是最新，则不下载任何东西，片刻后再次 Ready。
	s.release = nil
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	s.set(domain.UpdateState{Status: domain.UpdateChecking})

	release, err := s.updater.Release(ctx, s.current)
	if err != nil {
		if errors.Is(err, domain.ErrUpdateUnsupported) {
			s.set(domain.UpdateState{Status: domain.UpdateUnsupported})
			return domain.ErrUpdateUnsupported
		}
		return s.fail(err)
	}
	if release == nil {
		slog.Info("the application is up to date", "version", s.current)
		s.discard()
		s.set(domain.UpdateState{Status: domain.UpdateUpToDate, Version: s.current})
		return nil
	}
	if err := release.Validate(); err != nil {
		return s.fail(err)
	}
	s.mu.Lock()
	s.release = release
	s.mu.Unlock()
	s.discard()
	// 下载（含增量包）、Ed25519 签名校验与原地替换统一交由底层 updater 在 Install 时执行，
	// 避免检查阶段白下整包。
	s.set(domain.UpdateState{
		Status:  domain.UpdateReady,
		Version: release.Version,
		Notes:   release.Notes,
	})
	slog.Info("an update is ready", "version", release.Version)
	return nil
}

// fail 把状态置为 Failed 并返回导致它的错误，保持调用方要分支的形态：无法
// 读取的 release 仍是坏响应，其余都是不可用的更新。
func (s *Service) fail(err error) error {
	detail := detailOf(err)
	s.set(domain.UpdateState{Status: domain.UpdateFailed, Detail: detail})
	slog.Warn("the update check failed", "error", detail)
	return err
}

// detailOf 是 Failed 状态携带、也是日志记录的那句话。它的每个词都出自本函数：
// 注入的 updater 的错误可能引用被请求的 URL，而设置页或日志里出现 URL 或响应体
// 都不是用户想看到的。原因保留在 error 上供 errors.Is 使用，此处绝不重复。
func detailOf(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, domain.ErrProviderBadResponse):
		return "the release could not be read"
	case errors.Is(err, context.Canceled):
		return "the check was cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "the feed did not answer in time"
	case errors.Is(err, domain.ErrUpdateUnavailable):
		// 本包自身的失败带有自己的原因——如「release 无法下载」——它说明哪里
		// 失败而不说明向哪里请求。
		return reasonOf(err)
	default:
		return "the feed could not be read"
	}
}

// reasonOf 返回本包错误构造时的原因：包装的哨兵之后、为 errors.Is 追加的 cause
// 之前的部分。此处所有错误都写作「<sentinel>: <reason>[: <cause>]」，只有 cause
// 可能携带平台自己的话。
func reasonOf(err error) string {
	text := err.Error()
	for _, sentinel := range []error{domain.ErrUpdateUnavailable, domain.ErrProviderBadResponse} {
		if prefix, ok := strings.CutPrefix(text, sentinel.Error()+": "); ok {
			text = prefix
			break
		}
	}
	if reason, _, ok := strings.Cut(text, ": "); ok {
		text = reason
	}
	// 本包自己写的原因是句子；仍像地址的便不是原因，替换掉而不是显示。
	if strings.Contains(text, "://") {
		return "the update could not be completed"
	}
	return semantic(text)
}

// discard 删除 Dir 下早期版本可能遗留的所有内容。
//
// 服务不在两次运行之间保留东西——updater 下载到自己的临时目录并从那里替换安装
// ——所以 Dir 下任何内容都是残留，下载残留就是没人会安装的更新文件。清理而不是
// 保留，免得目录随每个版本增长；删除失败只记日志，不算检查失败。
func (s *Service) discard() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(s.dir, entry.Name())); err != nil {
			slog.Warn("a leftover file under the update directory could not be removed", "error", err)
		}
	}
}
