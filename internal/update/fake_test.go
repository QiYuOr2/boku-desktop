package update

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
)

// fakeUpdater 把发布源与传输合为一体，测试可让它回答任何内容：发布源说什么、
// 归档是什么、是否被要求安装。
type fakeUpdater struct {
	enabled bool

	mu sync.Mutex
	// release 是发布源的回答。nil 且错误为空表示「已是最新」。
	release *Release
	// releaseErr 是无法读取的发布源。
	releaseErr error
	// archive 是 Fetch 按 URL 提供的归档；不在其中的 URL 即无法下载的文件。
	archive map[string][]byte
	// fetchErr 为指定 URL 让 Fetch 失败。
	fetchErr map[string]error

	// releases 与 fetches 记录被请求了什么，用于核对单飞。
	releases int
	fetches  []string
	// installed 是 Install 被要求的版本，installs 记录调用次数，使第二次安装可见。
	installed  string
	installs   int
	installErr error
}

func newFakeUpdater() *fakeUpdater {
	return &fakeUpdater{enabled: true, archive: map[string][]byte{}, fetchErr: map[string]error{}}
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

func (f *fakeUpdater) Fetch(_ context.Context, url string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetches = append(f.fetches, url)
	if err := f.fetchErr[url]; err != nil {
		return nil, err
	}
	body, ok := f.archive[url]
	if !ok {
		return nil, errors.New("no such file")
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

func (f *fakeUpdater) Install(_ context.Context, version string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installs++
	f.installed = version
	return f.installErr
}

// counts 在锁下返回发布源与传输被请求的次数：检查可能在独立 goroutine 上运行。
func (f *fakeUpdater) counts() (releases int, fetches []string, installs int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.releases, append([]string(nil), f.fetches...), f.installs
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

// aRelease 是命名一个归档的 release。
func aRelease(t *testing.T, version string, payload []byte) *Release {
	t.Helper()
	release := &Release{
		Version: version,
		Notes:   "the notes of " + version,
		Assets: []Asset{{
			Name: version + ".tar.gz",
			URL:  "https://example.invalid/" + version + ".tar.gz",
			Size: int64(len(payload)),
		}},
	}
	if err := release.Validate(); err != nil {
		t.Fatalf("the fixture release is not one: %v", err)
	}
	return release
}

// serve 把 payload 放到 release 的归档位置。
func serve(f *fakeUpdater, release *Release, payload []byte) {
	asset, _ := release.Archive()
	f.archive[asset.URL] = payload
}
