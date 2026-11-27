// Package update 是更新功能：发现更新的 Boku、后台下载、核对发布承诺并
// 就位，供下次启动运行。
//
// 它不碰 MyGo、也不自行联网：发布源与下载传输都由外部注入（main.go 把
// mygo.Updater / mygo.App 适配成下面的接口），从而使状态机、定时器与下载
// 无需桌面会话和网络即可测试。界面只看到 domain.UpdateState，看不到
// release、URL 或错误。
package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"boku/internal/domain"
)

// Updater 是发布源：本机能否自更新、哪个 release 比当前版本新、如何取它的
// 文件，以及安装本身。main.go 基于 mygo 实现，测试用 fake 实现。
type Updater interface {
	// Enabled 报告本安装能否自更新：带更新源发布、且目录可写。返回否并不
	// 表示损坏，见 domain.UpdateUnsupported。
	Enabled() bool
	// Release 向发布源询问比 current 更新的版本；当前已是最新时返回 nil 与
	// 空错误。无法读取的 release 包装 domain.ErrProviderBadResponse，无法
	// 获取的包装 domain.ErrProviderUnavailable，消息中不引用发布源内容。
	Release(ctx context.Context, current string) (*Release, error)
	// Fetch 打开 release 的一个文件供读取，由调用方关闭。
	Fetch(ctx context.Context, url string) (io.ReadCloser, error)
	// Install 安装本应用已准备好的版本：由 updater 校验签名并就地替换安装。
	// 只在下载核验通过后由 Service.Install 调用一次。
	Install(ctx context.Context, version string) error
}

// Relauncher 重启应用，这是让已应用的更新生效的方式。main.go 基于
// mygo.App.Relaunch 实现，它像退出那样结束进程并以相同参数重启。
type Relauncher interface {
	Relaunch()
}

// Release 是发布源描述的一个更新版本：用到的是 MyGo 更新清单
// （update-<target>.json）里本应用需要的那部分字段。
type Release struct {
	Version string
	// Notes 是发布源提供的 Markdown 发布说明，界面暂不显示，但因发布源携带
	// 且状态需要而保留。
	Notes string
	// Assets 是 release 的文件；本应用只下载、核验并展示其中的归档。
	Assets []Asset
}

// Asset 是 release 的一个文件。清单签名的是归档的 SHA-256，因此 Asset 携带
// 摘要——它是磁盘字节与 release 之间的唯一绑定。
type Asset struct {
	Name string
	URL  string
	Size int64
	// SHA256 是文件的十六进制摘要。平台无法核验时为空（此时 updater 校验摘要
	// 本身的签名），下载便只按大小核对。
	SHA256 string
}

// Valid 报告 asset 是否信息足够被下载与核验：有名字、URL，以及可比较的大小。
func (a Asset) Valid() bool {
	return strings.TrimSpace(a.Name) != "" &&
		strings.TrimSpace(a.URL) != "" &&
		a.Size > 0
}

// Archive 返回 release 的第一个归档及其是否存在。release 可能列出的
// assets 包括归档、旧版本增量与旧版本归档；本应用只安装归档，一个都没有的
// release 无法使用，由 Validate 报告。
func (r *Release) Archive() (Asset, bool) {
	if r == nil {
		return Asset{}, false
	}
	for _, asset := range r.Assets {
		if isArchive(asset.Name) && asset.Valid() {
			return asset, true
		}
	}
	return Asset{}, false
}

// isArchive 判断文件名是否为 release 的归档；`mygo build` 命名为
// <app>-<version>-<target>.tar.gz。
func isArchive(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") || strings.HasSuffix(lower, ".zip")
}

// Validate 报告 release 缺少的必需项，返回调用方无法使用的结果。
//
// 在边界处、下载任何字节前校验：缺少 version 或归档的清单会变成显式失败，
// 而不是零值成功。
func (r *Release) Validate() error {
	if r == nil {
		return BadRelease("version", "the release says nothing")
	}
	if strings.TrimSpace(r.Version) == "" {
		return BadRelease("version", "the release names no version")
	}
	if strings.ContainsAny(r.Version, `/\`) || r.Version == "." || r.Version == ".." {
		// version 会成为下载目录下的子目录名，含路径的 version 会让文件被写到
		// release 指定的任意位置。
		return BadRelease("version", "the version is not a plain name")
	}
	if _, ok := r.Archive(); !ok {
		return BadRelease("assets", "the release holds no archive to install")
	}
	for _, asset := range r.Assets {
		if asset.SHA256 != "" && !isHexDigest(asset.SHA256) {
			return BadRelease("assets", "an asset carries a digest that is not SHA-256")
		}
	}
	return nil
}

// BadRelease 表示应用无法使用的 release，按 domain.ErrProviderBadResponse
// 报告。原因用本包自己的话并截断到 domain.MaxSnippetBytes，使解析失败的清单
// 不会刷满日志。导出是因为 main.go 的适配器在清单无法解码时也要构造它，让
// 「release 不可用」的原因在两处一致。
func BadRelease(field, reason string) error {
	return fmt.Errorf("%w: the release's %s: %s",
		domain.ErrProviderBadResponse, field, domain.UpdateReason(reason))
}

// isHexDigest 判断 s 是否为 64 个十六进制字符，即 SHA-256 摘要的写法。
func isHexDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// MaxArchiveSize 限制更新功能读取单个归档的上限。它是 MyGo 对 release 的
// 上限：超过的 release  updater 本就会拒绝，先在这里读一遍只会浪费两倍带宽。
const MaxArchiveSize = 1 << 30

// wrapUnavailable 覆盖通往已安装更新的所有失败：发布源无法访问、归档与承诺
// 不符、安装无法替换。原因是给用户看的一句话，绝不含 URL 或响应体。
func wrapUnavailable(reason string, cause error) error {
	reason = semantic(domain.UpdateReason(reason))
	if cause == nil {
		return fmt.Errorf("%w: %s", domain.ErrUpdateUnavailable, reason)
	}
	return fmt.Errorf("%w: %s: %v", domain.ErrUpdateUnavailable, reason, cause)
}

// IsUnsupported 判断错误是否为「本构建无法自更新」，它不算失败。
func IsUnsupported(err error) bool { return errors.Is(err, domain.ErrUpdateUnsupported) }

// IsUnavailable 判断错误是否为检查或下载失败。
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
