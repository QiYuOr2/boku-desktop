package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"boku/internal/domain"
	"boku/internal/platform/httpc"
	"boku/internal/update"
)

// updateCheckTimeout 限定一次更新检查的时长。MyGo 内置 Check 本身不设超时，
// 避免无响应网络让界面长时间停在「检查中」。
const updateCheckTimeout = 30 * time.Second

// mygoUpdater 是基于 MyGo 内置更新器 mygo.Updater 的 update.Updater 适配层：
// 清单直连、增量包（.delta）、Ed25519 签名校验与原地替换全部交给框架本身，
// 此处只做超时控制、版本规整、404 视作最新与最近一次检查结果的复用。
type mygoUpdater struct {
	enabledFn func() bool
	checkFn   func(context.Context) (*mygo.Update, error)

	mu      sync.Mutex
	pending *mygo.Update
}

func newMyGoUpdater() *mygoUpdater {
	return &mygoUpdater{
		enabledFn: mygo.Updater.Enabled,
		checkFn:   mygo.Updater.Check,
	}
}

func (u *mygoUpdater) Enabled() bool {
	if u.enabledFn != nil {
		return u.enabledFn()
	}
	return mygo.Updater.Enabled()
}

func (u *mygoUpdater) Release(ctx context.Context, current string) (*update.Release, error) {
	checkCtx, cancel := context.WithTimeout(ctx, updateCheckTimeout)
	defer cancel()

	check := u.checkFn
	if check == nil {
		check = mygo.Updater.Check
	}
	found, err := check(checkCtx)
	if err != nil {
		u.setPending(nil)
		if errors.Is(err, mygo.ErrUpdatesDisabled) {
			return nil, domain.ErrUpdateUnsupported
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(checkCtx.Err(), context.DeadlineExceeded) {
			return nil, context.DeadlineExceeded
		}
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return nil, context.Canceled
		}
		// 尚未发布任何 latest release 时清单返回 404，视为「已是最新」而非失败。
		if isHTTPStatus(err, http.StatusNotFound) {
			slog.Info("no release is published for this build yet", "version", current)
			return nil, nil
		}
		return nil, fmt.Errorf("%w: the release feed could not be read", domain.ErrUpdateUnavailable)
	}
	if found == nil {
		u.setPending(nil)
		return nil, nil
	}
	version := strings.TrimPrefix(strings.TrimSpace(found.Version), "v")
	if version == "" {
		u.setPending(nil)
		return nil, update.BadRelease("version", "the release names no version")
	}
	if update.Compare(version, current) <= 0 {
		u.setPending(nil)
		slog.Info("the application is up to date", "version", current, "newest", version)
		return nil, nil
	}
	u.setPending(found)
	slog.Debug("the release feed answered", "version", version, "current", current)
	return &update.Release{
		Version: version,
		Notes:   strings.TrimSpace(found.Notes),
	}, nil
}

func (u *mygoUpdater) Install(ctx context.Context, version string, progress func(downloaded, total int64)) error {
	found := u.takePending(version)
	if found == nil {
		check := u.checkFn
		if check == nil {
			check = mygo.Updater.Check
		}
		var err error
		found, err = check(ctx)
		if err != nil {
			if errors.Is(err, mygo.ErrUpdatesDisabled) {
				return fmt.Errorf("%w: this build cannot update itself", domain.ErrUpdateUnavailable)
			}
			return fmt.Errorf("%w: the release feed could not be read", domain.ErrUpdateUnavailable)
		}
	}
	if found == nil || strings.TrimPrefix(strings.TrimSpace(found.Version), "v") != version {
		return fmt.Errorf("%w: the feed no longer offers %s", domain.ErrUpdateUnavailable, version)
	}
	if err := found.Install(ctx, progress); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrUpdateUnavailable, err)
	}
	return nil
}

func (u *mygoUpdater) setPending(up *mygo.Update) {
	u.mu.Lock()
	u.pending = up
	u.mu.Unlock()
}

func (u *mygoUpdater) takePending(version string) *mygo.Update {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.pending == nil {
		return nil
	}
	if strings.TrimPrefix(strings.TrimSpace(u.pending.Version), "v") != version {
		u.pending = nil
		return nil
	}
	found := u.pending
	u.pending = nil
	return found
}

func isHTTPStatus(err error, code int) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), fmt.Sprintf(": %d ", code))
}

// mygoRelauncher 是基于 mygo.App 的 update.Relauncher：以退出方式重启应用，让刚安装的新版本生效。
type mygoRelauncher struct{}

func (mygoRelauncher) Relaunch() { mygo.App.Relaunch() }

// newUpdates 装配更新功能：保存残留清理目录、委托 mygo.Updater 的源，以及安装后重启。
func newUpdates(userData, version string) (*update.Service, error) {
	// MyGo 内置更新器使用 http.DefaultClient：提前装上代理感知传输，使其遵循系统代理设置。
	http.DefaultTransport = httpc.ProxyTransport()
	svc, err := update.New(update.Options{
		Current:    version,
		Dir:        filepath.Join(userData, "updates"),
		Updater:    newMyGoUpdater(),
		Relauncher: mygoRelauncher{},
	})
	if err != nil {
		return nil, fmt.Errorf("prepare the update service: %w", err)
	}
	return svc, nil
}
