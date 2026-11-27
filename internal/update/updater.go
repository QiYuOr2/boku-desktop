// Package update 是更新功能：调度检查、管理界面状态机、通过外部注入的更新器完成
// 检查与就地替换安装，并在安装成功后重启应用。
//
// 它不直接依赖 MyGo 或网络：实际检查、增量下载、Ed25519 签名校验与原地替换由
// 外部注入的 Updater（装配层委托给 mygo.Updater）承担，界面只看到 domain.UpdateState。
package update

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"boku/internal/domain"
)

// Updater 是底层更新能力：本机能否自更新、查询是否有新版本，以及下载校验并安装。
// 装配层基于 mygo.Updater 实现，测试用 fake 实现。
type Updater interface {
	// Enabled 报告本安装能否自更新：带更新源发布、且安装目录可写。
	Enabled() bool
	// Release 向发布源询问比 current 更新的版本；已是最新时返回 nil, nil。
	// 不支持自更新时返回 domain.ErrUpdateUnsupported；其它失败包装
	// domain.ErrUpdateUnavailable 或 domain.ErrProviderBadResponse。
	Release(ctx context.Context, current string) (*Release, error)
	// Install 下载（支持增量包与签名校验）并就地替换安装指定版本，下载期间回调 progress。
	Install(ctx context.Context, version string, progress func(downloaded, total int64)) error
}

// Relauncher 重启应用，这是让已应用的更新生效的方式。
type Relauncher interface {
	Relaunch()
}

// Release 是发布源报告的一个可用更新版本。
type Release struct {
	Version string
	Notes   string
}

// Validate 校验 release 的版本号合法，防止空版本或路径穿越字符进入状态与日志。
func (r *Release) Validate() error {
	if r == nil {
		return BadRelease("version", "the release says nothing")
	}
	if strings.TrimSpace(r.Version) == "" {
		return BadRelease("version", "the release names no version")
	}
	if strings.ContainsAny(r.Version, `/\`) || r.Version == "." || r.Version == ".." {
		return BadRelease("version", "the version is not a plain name")
	}
	return nil
}

// BadRelease 表示应用无法使用的 release，按 domain.ErrProviderBadResponse 报告。
func BadRelease(field, reason string) error {
	return fmt.Errorf("%w: the release's %s: %s",
		domain.ErrProviderBadResponse, field, domain.UpdateReason(reason))
}

// wrapUnavailable 覆盖通往已安装更新的所有失败；原因是给用户看的一句话，绝不含 URL 或响应体。
func wrapUnavailable(reason string, cause error) error {
	reason = semantic(domain.UpdateReason(reason))
	if cause == nil {
		return fmt.Errorf("%w: %s", domain.ErrUpdateUnavailable, reason)
	}
	return fmt.Errorf("%w: %s: %v", domain.ErrUpdateUnavailable, reason, cause)
}

// IsUnsupported 判断错误是否为「本构建无法自更新」，它不算失败。
func IsUnsupported(err error) bool { return errors.Is(err, domain.ErrUpdateUnsupported) }

// IsUnavailable 判断错误是否为检查或安装失败。
func IsUnavailable(err error) bool { return errors.Is(err, domain.ErrUpdateUnavailable) }

// IsBadRelease 判断错误是否为无法读取的 release。
func IsBadRelease(err error) bool { return errors.Is(err, domain.ErrProviderBadResponse) }

// semantic 把原因规整为域错误给用户读的语气：小写、句末无句号。
func semantic(reason string) string {
	reason = strings.TrimSpace(reason)
	reason = strings.TrimRight(reason, ".!;,")
	if reason == "" {
		return "the update could not be completed"
	}
	return reason
}
