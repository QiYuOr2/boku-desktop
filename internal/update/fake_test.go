package update

import (
	"context"
	"sync"
	"testing"
)

// fakeUpdater 模拟底层更新器：发布源说什么、安装是否回报下载进度与成功/失败。
type fakeUpdater struct {
	enabled bool

	mu sync.Mutex
	// release 是发布源的回答。nil 且错误为空表示「已是最新」。
	release *Release
	// releaseErr 是无法读取的发布源。
	releaseErr error

	// releases 记录 Release 被调用次数，用于核对单飞。
	releases int
	// installed 是 Install 被要求的版本，installs 记录调用次数，使第二次安装可见。
	installed     string
	installs      int
	installErr    error
	installSteps  [][2]int64
	installBefore func()
}

func newFakeUpdater() *fakeUpdater {
	return &fakeUpdater{enabled: true}
}

func (f *fakeUpdater) Enabled() bool { return f.enabled }

func (f *fakeUpdater) Release(context.Context, string) (*Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releases++
	if f.releaseErr != nil {
		return nil, f.releaseErr
	}
	return f.release, nil
}

func (f *fakeUpdater) Install(_ context.Context, version string, progress func(downloaded, total int64)) error {
	f.mu.Lock()
	f.installs++
	f.installed = version
	steps := append([][2]int64(nil), f.installSteps...)
	before := f.installBefore
	err := f.installErr
	f.mu.Unlock()

	if before != nil {
		before()
	}
	if progress != nil {
		for _, step := range steps {
			progress(step[0], step[1])
		}
	}
	return err
}

// counts 在锁下返回检查与安装调用次数：检查可能在独立 goroutine 上运行。
func (f *fakeUpdater) counts() (releases, installs int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.releases, f.installs
}

// fakeRelauncher 记录应用是否被要求重启。
type fakeRelauncher struct {
	mu       sync.Mutex
	relaunch int
}

func (r *fakeRelauncher) Relaunch() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.relaunch++
}

func (r *fakeRelauncher) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.relaunch
}

func aRelease(t *testing.T, version string) *Release {
	t.Helper()
	release := &Release{
		Version: version,
		Notes:   "the notes of " + version,
	}
	if err := release.Validate(); err != nil {
		t.Fatalf("the fixture release is not one: %v", err)
	}
	return release
}
