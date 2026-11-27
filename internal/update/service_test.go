package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"boku/internal/domain"
)

// newService 在 fake 之上构建服务，带自己的目录并让调度靠边：自己驱动 Check
// 的测试不想要服务的定时器。
func newService(t *testing.T, f *fakeUpdater, opts ...func(*Options)) *Service {
	t.Helper()
	options := Options{
		Current: "0.1.0-beta.5",
		Dir:     t.TempDir(),
		Updater: f,
		// 负的 FirstDelay 表示「循环不自行检查」，正是自己调 Check 的测试所需。
		FirstDelay: -1,
		Interval:   time.Hour,
	}
	for _, apply := range opts {
		apply(&options)
	}
	s, err := New(options)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func withRelauncher(r Relauncher) func(*Options) {
	return func(o *Options) { o.Relauncher = r }
}

func withCurrent(version string) func(*Options) {
	return func(o *Options) { o.Current = version }
}

func withDelays(first, interval time.Duration) func(*Options) {
	return func(o *Options) { o.FirstDelay, o.Interval = first, interval }
}

// 服务由发布源与目录装配而成，二者缺一即拒绝：什么都不查的服务就是永远不动的状态。
func TestNewRequiresAFeedAndADirectory(t *testing.T) {
	for _, c := range []struct {
		name string
		opts Options
	}{
		{"no updater", Options{Dir: t.TempDir()}},
		{"no directory", Options{Updater: newFakeUpdater()}},
		{"relative directory", Options{Dir: "updates", Updater: newFakeUpdater()}},
	} {
		if _, err := New(c.opts); !errors.Is(err, domain.ErrInvalidArgument) {
			t.Errorf("%s: New = %v, want ErrInvalidArgument", c.name, err)
		}
	}
}

// 全新服务尚未询问发布源，状态如实：Idle 行不绘制任何东西。
func TestAFreshServiceIsIdle(t *testing.T) {
	s := newService(t, newFakeUpdater())
	if state := s.State(); state.Status != domain.UpdateIdle {
		t.Errorf("the fresh state = %s, want idle", state.Status)
	}
}

// 正常路径：发现更新的 release，归档确实以发布声明的尺寸存在，状态以 Ready
// 结束并带上行要显示的版本与说明。
func TestCheckReachesReady(t *testing.T) {
	f := newFakeUpdater()
	payload := []byte(strings.Repeat("boku ", 1024))
	release := aRelease(t, "0.1.0-beta.6", payload)
	f.release = release
	serve(f, release, payload)

	changes := &counter{}
	s := newService(t, f, func(o *Options) { o.OnChange = changes.add })
	if err := s.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	state := s.State()
	if state.Status != domain.UpdateReady {
		t.Fatalf("the state = %s (%s), want ready", state.Status, state.Detail)
	}
	if state.Version != "0.1.0-beta.6" {
		t.Errorf("version = %q", state.Version)
	}
	if state.Notes != release.Notes {
		t.Errorf("notes = %q, want the release's", state.Notes)
	}
	if state.Detail != "" {
		t.Errorf("a ready state carries a detail: %q", state.Detail)
	}
	// 归档被请求一次：本次检查取它来确认存在。安装是 updater 的事，它会再下一次。
	if _, fetches, _ := f.counts(); len(fetches) != 1 {
		t.Errorf("the archive was fetched %d times, want once: %v", len(fetches), fetches)
	}
	if changes.get() < 2 {
		t.Errorf("the views were told %d times, want at least checking and ready", changes.get())
	}
}

// 已是最新是一种状态而非错误，且不取任何文件。
func TestCheckUpToDateFetchesNothing(t *testing.T) {
	f := newFakeUpdater()
	s := newService(t, f)
	if err := s.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	state := s.State()
	if state.Status != domain.UpdateUpToDate {
		t.Fatalf("the state = %s, want up to date", state.Status)
	}
	if state.Version != "0.1.0-beta.5" {
		t.Errorf("version = %q, want the running one", state.Version)
	}
	if _, fetches, _ := f.counts(); len(fetches) != 0 {
		t.Errorf("an up-to-date check fetched %v", fetches)
	}
}

// 不能自更新的构建对此保持安静：状态为 Unsupported，错误是其专属名字，发布源
// 从不被询问，warn 级也无输出。
func TestUnsupportedIsNotAFailure(t *testing.T) {
	f := newFakeUpdater()
	f.enabled = false
	s := newService(t, f)
	err := s.Check(context.Background())
	if !errors.Is(err, domain.ErrUpdateUnsupported) {
		t.Fatalf("Check = %v, want ErrUpdateUnsupported", err)
	}
	state := s.State()
	if state.Status != domain.UpdateUnsupported {
		t.Errorf("the state = %s, want unsupported", state.Status)
	}
	if state.Detail != "" {
		t.Errorf("unsupported carries a detail: %q", state.Detail)
	}
	if releases, _, _ := f.counts(); releases != 0 {
		t.Errorf("an unsupported build asked the feed %d times", releases)
	}
}

// 不能自更新的构建上 Start 不建定时器：每天醒来学到同一件事只是噪音。
func TestUnsupportedStartsNoLoop(t *testing.T) {
	f := newFakeUpdater()
	f.enabled = false
	s := newService(t, f, withDelays(time.Millisecond, time.Millisecond))
	s.Start(context.Background())
	defer s.Stop()
	if state := s.State(); state.Status != domain.UpdateUnsupported {
		t.Errorf("the state = %s, want unsupported", state.Status)
	}
	if releases, _, _ := f.counts(); releases != 0 {
		t.Errorf("the loop asked the feed %d times", releases)
	}
}

// 发布源描述不清的 release 是失败，其 detail 是本包自己的话：清单 URL 或响应体
// 绝不进入页面。
func TestAFailedCheckSaysWhyWithoutQuotingTheFeed(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(*fakeUpdater, *Release)
		want  domain.UpdateStatus
	}{
		{
			name: "the feed cannot be reached",
			setup: func(f *fakeUpdater, _ *Release) {
				f.releaseErr = fmt.Errorf("%w: GET https://example.invalid/secret-token", domain.ErrUpdateUnavailable)
			},
			want: domain.UpdateFailed,
		},
		{
			name: "the manifest is not one",
			setup: func(f *fakeUpdater, _ *Release) {
				f.releaseErr = BadRelease("version", "the release names no version")
			},
			want: domain.UpdateFailed,
		},
		{
			name: "the release names no archive",
			setup: func(f *fakeUpdater, _ *Release) {
				f.release = &Release{Version: "0.1.0-beta.6"}
			},
			want: domain.UpdateFailed,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeUpdater()
			var release *Release
			c.setup(f, release)
			s := newService(t, f)
			if err := s.Check(context.Background()); err == nil {
				t.Fatal("Check accepted a check that failed")
			}
			state := s.State()
			if state.Status != c.want {
				t.Errorf("the state = %s, want %s", state.Status, c.want)
			}
			if state.Detail == "" {
				t.Error("a failed check has nothing to say")
			}
			for _, leak := range []string{"http", "example.invalid", "secret-token", "GET "} {
				if strings.Contains(state.Detail, leak) {
					t.Errorf("the detail carries %q: %q", leak, state.Detail)
				}
			}
		})
	}
}

// 无法下载、或大小与承诺不符的归档是检查失败：release 并不真存在，就不提供重启。
func TestAnArchiveThatIsNotThereIsAFailure(t *testing.T) {
	for _, c := range []struct {
		name string
		with func(*fakeUpdater, *Release)
	}{
		{
			name: "the archive cannot be fetched",
			with: func(f *fakeUpdater, release *Release) {
				asset, _ := release.Archive()
				f.fetchErr[asset.URL] = fmt.Errorf("%w: connection reset", domain.ErrUpdateUnavailable)
			},
		},
		{
			name: "nothing serves the archive",
			with: func(_ *fakeUpdater, _ *Release) {},
		},
		{
			name: "the archive is shorter than promised",
			with: func(f *fakeUpdater, release *Release) {
				asset, _ := release.Archive()
				f.archive[asset.URL] = []byte("short")
			},
		},
		{
			name: "the archive is longer than promised",
			with: func(f *fakeUpdater, release *Release) {
				asset, _ := release.Archive()
				f.archive[asset.URL] = []byte(strings.Repeat("x", int(asset.Size)+10))
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeUpdater()
			payload := []byte("the promised bytes")
			release := aRelease(t, "0.1.0-beta.6", payload)
			f.release = release
			c.with(f, release)
			s := newService(t, f)
			err := s.Check(context.Background())
			if !errors.Is(err, domain.ErrUpdateUnavailable) {
				t.Fatalf("Check = %v, want ErrUpdateUnavailable", err)
			}
			state := s.State()
			if state.Status != domain.UpdateFailed {
				t.Errorf("the state = %s, want failed", state.Status)
			}
			if strings.Contains(state.Detail, "http") {
				t.Errorf("the detail quotes a URL: %q", state.Detail)
			}
		})
	}
}

// 同时进行的两次检查只询问发布源一次：第二次是同一个问题。
func TestChecksInFlightAreSingleFlight(t *testing.T) {
	gate := make(chan struct{})
	f := newFakeUpdater()
	s := newService(t, f)
	blocking := blockingUpdater{f: f, gate: gate, entered: make(chan struct{})}
	s.updater = blocking

	first := make(chan error, 1)
	go func() { first <- s.Check(context.Background()) }()
	// 等第一次检查真正进入发布源，而不只是开始：在其进入前到达的第二次检查
	// 不是被测的情形。
	<-blocking.entered
	for i := 0; i < 3; i++ {
		if err := s.Check(context.Background()); err != nil {
			t.Fatalf("a check during one in flight = %v, want nil", err)
		}
	}
	close(gate)
	if err := <-first; err != nil {
		t.Fatalf("the first check: %v", err)
	}
	if releases, _, _ := f.counts(); releases != 1 {
		t.Errorf("the feed was asked %d times, want once", releases)
	}
}

// blockingUpdater 是发布源被闸门卡住的 fake，以便在第一次仍在等待时发起第二次检查。
type blockingUpdater struct {
	f       *fakeUpdater
	gate    chan struct{}
	entered chan struct{}
}

func (b blockingUpdater) Enabled() bool { return true }

func (b blockingUpdater) Release(ctx context.Context, current string) (*Release, error) {
	close(b.entered)
	select {
	case <-b.gate:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return b.f.Release(ctx, current)
}

func (b blockingUpdater) Fetch(ctx context.Context, url string) (io.ReadCloser, error) {
	return b.f.Fetch(ctx, url)
}

func (b blockingUpdater) Install(ctx context.Context, version string) error {
	return b.f.Install(ctx, version)
}

// 定时器：首次检查等 FirstDelay，刚跑过的检查在间隔到期前不再运行。
func TestTheLoopWaitsBeforeAsking(t *testing.T) {
	f := newFakeUpdater()
	s := newService(t, f, withDelays(200*time.Millisecond, time.Hour))
	s.Start(context.Background())
	defer s.Stop()

	// 首个延迟未过前，什么都没问。
	time.Sleep(80 * time.Millisecond)
	if releases, _, _ := f.counts(); releases != 0 {
		t.Fatalf("the feed was asked %d times before the first delay", releases)
	}
	// 之后恰好检查一次。
	deadline := time.Now().Add(2 * time.Second)
	for {
		if releases, _, _ := f.counts(); releases == 1 {
			break
		} else if releases > 1 {
			t.Fatalf("the loop asked %d times, want once before the interval", releases)
		}
		if time.Now().After(deadline) {
			t.Fatal("the loop never asked the feed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// 间隔未过前不再问第二次。
	time.Sleep(150 * time.Millisecond)
	if releases, _, _ := f.counts(); releases != 1 {
		t.Errorf("the loop asked %d times, want once until the interval", releases)
	}
}

// Stop 结束循环；从未启动的服务也能干净停止。
func TestStopIsSafeBeforeAndAfterStart(t *testing.T) {
	s := newService(t, newFakeUpdater())
	s.Stop()
	s.Start(context.Background())
	s.Start(context.Background())
	finished := make(chan struct{})
	go func() { s.Stop(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not end the loop")
	}
}

// 检查在另一 goroutine 运行时从绘制线程读 State：这正是给竞态检测器用的，
// 且答案必须是真实状态。
func TestStateIsReadableDuringACheck(t *testing.T) {
	f := newFakeUpdater()
	payload := []byte(strings.Repeat("x", 64))
	release := aRelease(t, "0.1.0-beta.6", payload)
	f.release = release
	serve(f, release, payload)
	s := newService(t, f)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				if !s.State().Status.Valid() {
					t.Error("a state that is not a status")
					return
				}
			}
		}
	}()
	if err := s.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	close(stop)
	wg.Wait()
}

// counter 统计来自任意 goroutine 的调用。
type counter struct {
	mu sync.Mutex
	n  int
}

func (c *counter) add() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
}

func (c *counter) get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// 服务的目录被清理而非累积：留在这里的是 updater 不保留的下载残留。
func TestTheServiceCollectsItsDirectory(t *testing.T) {
	dir := t.TempDir()
	leftover := filepath.Join(dir, "0.1.0-beta.4")
	if err := os.MkdirAll(leftover, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(leftover, "archive.tar.gz"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := newFakeUpdater()
	s := newService(t, f, func(o *Options) { o.Dir = dir })
	if err := s.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Errorf("the leftover %s was kept", leftover)
	}
}
