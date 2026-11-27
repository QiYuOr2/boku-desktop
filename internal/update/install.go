package update

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"boku/internal/domain"
)

// installTimeout 限制 updater 替换安装的时间：足够解包归档，又短到文件系统
// 卡住时不会让更新提示挂满整轮运行。
const installTimeout = 5 * time.Minute

// Install 应用一个就绪更新并重启应用。
//
// 它核验与不核验的边界值得说明：本包下载归档、核对完整、在发布源能给时核对
// 清单所签的 SHA-256，并告知用户版本就绪；但它不应用归档——安装由 updater 通过
// Updater.Install 完成，Ed25519 签名在那里对编译进发布物的公钥校验，归档按
// `mygo build` 的布局解包。在这里再写一个解包器只会与第一个碰巧一致。
//
// 顺序以留下可运行的应用为准：updater 先替换安装，只有替换成功才重启；替换失败
// 则运行版本原封不动，状态如实说明。替换成功但重启未能启动，则新版本已安装、
// 下次启动生效。Relauncher 为 nil 就是最后一种——已装不重启——并非错误：界面说
// 「下次启动生效」而不是给一个点了没用的按钮。
//
// 可从 goroutine 调用：它在服务锁内读写状态、锁外做 I/O。不得从绘制线程调用，
// 那是 InstallAsync 的用途。
func (s *Service) Install(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	state := s.state
	release := s.release
	s.mu.Unlock()
	if state.Status != domain.UpdateReady || release == nil {
		return fmt.Errorf("%w: there is no downloaded update to install", domain.ErrUpdateUnavailable)
	}
	version := release.Version

	slog.Info("installing the update", "version", version)
	installCtx, cancel := context.WithTimeout(ctx, installTimeout)
	err := s.updater.Install(installCtx, version)
	cancel()

	// 无论如何，这个 release 都不再提供：要么已应用，要么已无法从这里安装。
	// 留在 Ready 会给出一个重启了也没用的提示。
	s.mu.Lock()
	s.release = nil
	s.mu.Unlock()
	if err != nil {
		s.set(domain.UpdateState{
			Status:  domain.UpdateFailed,
			Version: version,
			Detail:  "the update could not be installed",
		})
		// updater 自己的消息可能引用它请求的 URL，故不在此重复。
		slog.Warn("the update could not be installed", "version", version)
		return wrapUnavailable("the update could not be installed", err)
	}

	if s.relaunch == nil {
		s.set(domain.UpdateState{
			Status:  domain.UpdateUpToDate,
			Version: version,
			Detail:  "the update is installed and takes effect at the next launch",
		})
		return nil
	}
	// 归档是唯一能还原被替换安装的东西，因此留在磁盘上直到下次启动清理。
	s.set(domain.UpdateState{
		Status:  domain.UpdateUpToDate,
		Version: version,
		Detail:  "the update is installed; restarting",
	})
	// Relaunch 把退出投递到主线程后返回：此行之后无需再做任何事，它必须是本
	// 方法最后一行。
	s.relaunch.Relaunch()
	return nil
}

// InstallAsync 在独立 goroutine 上执行 Install，结果交给 done。它是点击
// 「重启并更新」的入口：点击在主线程，而替换安装是 I/O，不得占用主线程。done
// 为 nil 不算错误。
func (s *Service) InstallAsync(ctx context.Context, done func(error)) {
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		err := s.Install(ctx)
		if done != nil {
			done(err)
		}
	}()
}

// entriesUnder 列出一个目录下的名字，供测试统计是否发生下载或目录是否被清理。
// 目录不存在返回空列表。
func entriesUnder(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
