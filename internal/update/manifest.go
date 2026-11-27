package update

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"boku/internal/domain"
)

// maxManifestBytes 限制本应用读取的更新清单大小，与 MyGo 对该文件的限制一致
// （internal/update.MaxManifestSize）：超过它就不是发布物所发的清单。
const maxManifestBytes = 1 << 20

// manifest 是 `mygo build` 写出的 MyGo 更新清单 update-<target>.json。它的形态
// 属于发布物而非本应用：在边界处解码，转成本包命名的 Release。
//
// 只保留 release 需要的字段。date、previous、deltas 是 updater 自己的事务——
// 它们服务于本应用不走的增量路径——带这些字段的清单照常读取。
type manifest struct {
	Version   string `json:"version"`
	Notes     string `json:"notes"`
	URL       string `json:"url"`
	Size      int64  `json:"size"`
	Signature string `json:"signature"`
}

// DecodeManifest 读取 MyGo 更新清单，转成本应用安装的 release。
//
// 它覆盖两个发布源契约的全部：无法读取的清单（非 JSON、非对象、缺 version
// 或归档、大小不可能是文件）一律为 domain.ErrProviderBadResponse，绝不是一个
// 字段全零的 release。携带的原因是截断到 domain.MaxSnippetBytes 的、指明字段
// 而非引用正文的内容。
//
// digest 是归档的 SHA-256（小写十六进制），由核验清单签名的一方算出：清单签的是
// 摘要而非 URL，本包没有公钥。它把磁盘字节与 release 绑定，平台无法核验时可为
// 空，此时下载只按大小核对。
func DecodeManifest(payload []byte, digest string) (*Release, error) {
	trimmed := strings.TrimSpace(string(payload))
	if trimmed == "" {
		return nil, BadRelease("body", "the feed answered with nothing")
	}
	if len(payload) > maxManifestBytes {
		return nil, BadRelease("body", fmt.Sprintf("the manifest is larger than %d bytes", maxManifestBytes))
	}
	var m manifest
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	// 清单必须是对象。其它形态（列表、字符串、数字）都会无报错地解成零值，
	// 正是这里要拒绝的零值成功。
	if err := decoder.Decode(&m); err != nil {
		return nil, BadRelease("body", err.Error())
	}
	if decoder.More() {
		return nil, BadRelease("body", "the manifest carries more than one document")
	}
	if strings.TrimSpace(m.Version) == "" {
		return nil, BadRelease("version", "the manifest names no version")
	}
	if strings.TrimSpace(m.URL) == "" {
		return nil, BadRelease("url", "the manifest names no archive")
	}
	if m.Size <= 0 {
		return nil, BadRelease("size", fmt.Sprintf("the manifest gives the archive a size of %d bytes", m.Size))
	}
	if strings.TrimSpace(m.Signature) == "" {
		return nil, BadRelease("signature", "the manifest does not sign the archive")
	}
	release := &Release{
		Version: releaseVersion(m.Version),
		Notes:   strings.TrimSpace(m.Notes),
		Assets: []Asset{{
			// 下载的归档用本应用决定的文件名：URL 自身的路径属于发布物，版本
			// 目录才说明是哪个 release。
			Name:   archiveNameFor(releaseVersion(m.Version)),
			URL:    strings.TrimSpace(m.URL),
			Size:   m.Size,
			SHA256: strings.ToLower(strings.TrimSpace(digest)),
		}},
	}
	if err := release.Validate(); err != nil {
		return nil, err
	}
	return release, nil
}

// releaseVersion 把清单命名的版本变成目录名：清单属于发布物，前导的 "v" 只是
// 标签的写法。
func releaseVersion(version string) string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}

// archiveNameFor 是下载的 release 在磁盘上的名字。扩展名用 MyGo 发布的那个，
// 使目录列表能看出文件是什么；外层的版本目录说明是哪个 release。
func archiveNameFor(version string) string {
	return version + ".tar.gz"
}

// DecodeManifestFrom 从 r 读取清单，受 maxManifestBytes 限制。
func DecodeManifestFrom(r io.Reader, digest string) (*Release, error) {
	payload, err := io.ReadAll(io.LimitReader(r, maxManifestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: reading the manifest: %v", domain.ErrProviderBadResponse, err)
	}
	return DecodeManifest(payload, digest)
}
