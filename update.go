package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"boku/internal/domain"
	"boku/internal/platform/httpc"
	"boku/internal/update"
)

// 本文件是更新功能与框架对接的全部。
//
// internal/update 决定界面上说什么、何时查询源、如何校验下载。它自己不能做的是接触 MyGo 自己的更新器与发布源：那是本构建签名清单的发布处，在此适配——与剪贴板设备、开机启动一样，这是唯一允许 import 一切的文件。
//
// 这种拆分是刻意的，也是功能可测试的原因：状态机、定时器与下载均可离线测试，而无法在桌面会话之外测试的本文件几乎没有自己的逻辑。

const (
	// updateCheckTimeout 限定一次查询源的时长。MyGo 自己的检查没有超时；永不响应的源不得让「检查中」那行在整个会话期间留在页面上。
	updateCheckTimeout = 30 * time.Second
	// maxFeedBytes 限定本文件读取的清单大小。它与 MyGo 对该文件的上限一致，因此此处接受的清单框架也接受。
	maxFeedBytes = 1 << 20
	// updateUserAgent 是 Boku 向源表明身份的方式。GitHub 是本应用联系的非用户自有服务，它可以知道是谁在询问。
	updateUserAgent = "Boku"
	// githubLatestManifestBase 直接走 GitHub Release 的 /releases/latest/download 重定向，
	// 与 MyGo 内置更新器一致，不经过 api.github.com，避免匿名 60 次/小时 IP 限流导致 403。
	githubLatestManifestBase = "https://github.com/QiYuOr2/boku-desktop/releases/latest/download"
)

// errFeedMissing 表示源返回 404：本构建还没有发布任何版本。
var errFeedMissing = errors.New("the release feed has nothing yet")

// mygoUpdater 是 update.Updater：MyGo 更新器负责它能回答的两个问题，源本身负责它无法回答的那个。
//
// 框架的 Check 只返回一个它会安装的更新，别的什么都不给：它不交出清单，而页面在安装前需要发布的版本与说明。所以清单在此读取——同一份签名文件，取自框架编入构建的同一 URL——安装则交回框架，归档签名在那里对构建内的公钥校验。
type mygoUpdater struct {
	client *http.Client
}

// Enabled 报告本构建能否检查公共更新通道。检查对所有构建都有用，包括 go run 与 mygo dev；MyGo 自己的 Enabled 还额外回答这个可执行文件能否原地替换，这只在调用 Install 时才有意义。
func (u *mygoUpdater) Enabled() bool { return true }

// Release 读取本构建的清单，转换成界面展示的发布。它是 update.Updater。
func (u *mygoUpdater) Release(ctx context.Context, current string) (*update.Release, error) {
	return u.release(ctx, releaseFeed(), current)
}

// release 是接受测试指定源 URL 的 Release。404 是尚未发布的版本的答复——首个签名发布尚未上线的仓库的常态——它属于「已是最新」而非失败：在每个足够新的版本启动时都显示「检查更新失败」，是对无事的警告。
func (u *mygoUpdater) release(ctx context.Context, feed, current string) (*update.Release, error) {
	payload, err := u.get(ctx, feed)
	if err != nil {
		if errors.Is(err, errFeedMissing) {
			slog.Info("no release is published for this build yet", "version", current)
			return nil, nil
		}
		return nil, err
	}
	release, err := update.DecodeManifest(payload, "")
	if err != nil {
		return nil, err
	}
	slog.Debug("the release feed answered", "version", release.Version, "current", current)
	// 源中的版本是最新发布版，而非本构建可用的最新版：运行最新版本的构建会读回自己的清单。交出它会向用户提供「重启以更新到正在运行的版本」，所以框架在自身 Check 中做的比较也在此进行——本路径自己读取清单，决定前从不到达那个 Check。
	if update.Compare(release.Version, current) <= 0 {
		slog.Info("the application is up to date", "version", current, "newest", release.Version)
		return nil, nil
	}
	return release, nil
}

// Fetch 打开发布中的一个文件：internal/update 在提供某个版本前下载并校验的归档。它是 update.Updater。
func (u *mygoUpdater) Fetch(ctx context.Context, url string) (io.ReadCloser, error) {
	return u.open(ctx, url)
}

// Install 应用 internal/update 下载的版本。
//
// 它把工作交回框架而非自己解包：框架用编入构建的密钥校验归档的 Ed25519 签名，以 `mygo build` 产出的布局解包，并原地替换安装。在此再写一个解包器只会靠运气与之一致。它是 update.Updater。
func (u *mygoUpdater) Install(ctx context.Context, version string) error {
	found, err := mygo.Updater.Check(ctx)
	if err != nil {
		if errors.Is(err, mygo.ErrUpdatesDisabled) {
			return fmt.Errorf("%w: this build cannot update itself", domain.ErrUpdateUnavailable)
		}
		return fmt.Errorf("%w: the release feed could not be read", domain.ErrUpdateUnavailable)
	}
	if found == nil || strings.TrimPrefix(found.Version, "v") != version {
		// 源已不再提供下载过的那个版本：向用户展示的与实际将安装的是不同的发布。
		return fmt.Errorf("%w: the feed no longer offers %s", domain.ErrUpdateUnavailable, version)
	}
	if err := found.Install(ctx, nil); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrUpdateUnavailable, err)
	}
	return nil
}

// get 读取源的清单，有上限，并保留那个唯一不算失败的答复：404 意味着本构建尚未发布任何版本。此处任何错误都不引用所请求的 URL。
func (u *mygoUpdater) get(ctx context.Context, url string) ([]byte, error) {
	body, err := u.open(ctx, url)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	payload, err := io.ReadAll(io.LimitReader(body, maxFeedBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: the release feed stopped answering", domain.ErrUpdateUnavailable)
	}
	if len(payload) > maxFeedBytes {
		return nil, update.BadRelease("body", fmt.Sprintf("the manifest is larger than %d bytes", maxFeedBytes))
	}
	return payload, nil
}

// open 是本文件唯一的网络调用，也是更新功能通往网络的整条路径：请求带 context 与超时，答复内容不被引用，调用方区分的两种状态——404 与其余——在此分辨。
func (u *mygoUpdater) open(ctx context.Context, url string) (io.ReadCloser, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: the update address cannot be asked", domain.ErrUpdateUnavailable)
	}
	request.Header.Set("User-Agent", updateUserAgent)
	response, err := u.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%w: the check timed out", domain.ErrUpdateUnavailable)
		}
		return nil, fmt.Errorf("%w: the release feed could not be reached", domain.ErrUpdateUnavailable)
	}
	switch {
	case response.StatusCode == http.StatusNotFound:
		response.Body.Close()
		return nil, errFeedMissing
	case response.StatusCode != http.StatusOK:
		response.Body.Close()
		return nil, fmt.Errorf("%w: the release feed answered %d", domain.ErrUpdateUnavailable, response.StatusCode)
	}
	return response.Body, nil
}

// mygoRelauncher 是基于 mygo.App 的 update.Relauncher：应用以退出的方式退出，并以相同参数重启，刚安装的版本由此生效。
type mygoRelauncher struct{}

// Relaunch 把退出投递到主线程后返回。
func (mygoRelauncher) Relaunch() { mygo.App.Relaunch() }

// releaseFeed 是本平台最新签名更新清单的直链，与 MyGo 内置更新器一致。
func releaseFeed() string {
	return githubLatestManifestBase + "/update-" + updateTarget() + ".json"
}

// updateTarget 命名 `mygo build` 为本平台发布的一对文件：darwin-universal 是两种 Mac 架构共用的一个包，其余平台各成一对。
func updateTarget() string {
	if runtime.GOOS == "darwin" {
		return "darwin-universal"
	}
	return runtime.GOOS + "-" + runtime.GOARCH
}

// newUpdates 装配更新功能：保存下载的目录、internal/update 声明的界面背后的源，以及让已安装更新生效的重启。
//
// 与 services 一样独立成函数，使接线在一处可读，并可在无桌面会话时检查。服务不在此启动：main 在应用就绪后启动它，因为检查应在托盘存在之后进行。
func newUpdates(userData, version string) (*update.Service, error) {
	// MyGo 的安装器没有传输注入点：用户选择「重启并更新」时它使用 http.DefaultClient。在 MyGo 发出该请求前装上同样的代理感知传输，让它的归档下载与检查、校验请求遵循完全相同的规则。
	http.DefaultTransport = httpc.ProxyTransport()
	svc, err := update.New(update.Options{
		Current: version,
		Dir:     filepath.Join(userData, "updates"),
		Updater: &mygoUpdater{client: &http.Client{
			Timeout:   updateCheckTimeout,
			Transport: httpc.ProxyTransport(),
		}},
		Relauncher: mygoRelauncher{},
	})
	if err != nil {
		return nil, fmt.Errorf("prepare the update service: %w", err)
	}
	return svc, nil
}
