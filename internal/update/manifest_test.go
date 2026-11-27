package update

import (
	"errors"
	"strings"
	"testing"

	"boku/internal/domain"
)

// 清单是发布物自己的文件，AGENTS.md 的适配器规则同样适用于它：不是清单的正文
// 是坏响应，绝不是一个全零的 release。
func TestDecodeManifestRejectsWhatIsNotARelease(t *testing.T) {
	for _, c := range []struct {
		name string
		body string
	}{
		{"empty", ""},
		{"whitespace", "  \n\t "},
		{"not json", "<html>404</html>"},
		{"a list", `[{"version":"1.0.0"}]`},
		{"a string", `"the release"`},
		{"an object with nothing in it", `{}`},
		{"no version", `{"url":"https://example.invalid/a.tar.gz","size":10,"signature":"sig"}`},
		{"no url", `{"version":"1.0.0","size":10,"signature":"sig"}`},
		{"no size", `{"version":"1.0.0","url":"https://example.invalid/a.tar.gz","signature":"sig"}`},
		{"negative size", `{"version":"1.0.0","url":"https://example.invalid/a.tar.gz","size":-1,"signature":"sig"}`},
		{"no signature", `{"version":"1.0.0","url":"https://example.invalid/a.tar.gz","size":10}`},
		{"truncated", `{"version":"1.0.0","url":`},
		{"two documents", `{"version":"1.0.0","url":"https://example.invalid/a.tar.gz","size":1,"signature":"s"} {"version":"2.0.0"}`},
		{"a version that is a path", `{"version":"../escape","url":"https://example.invalid/a.tar.gz","size":10,"signature":"sig"}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			release, err := DecodeManifest([]byte(c.body), "")
			if !errors.Is(err, domain.ErrProviderBadResponse) {
				t.Fatalf("DecodeManifest(%q) = (%v, %v), want ErrProviderBadResponse", c.body, release, err)
			}
			if release != nil {
				t.Errorf("a bad manifest produced a release: %+v", release)
			}
		})
	}
}

// 大于框架读取上限的清单被拒绝，而不是被部分理解。
func TestDecodeManifestRefusesAnOversizedBody(t *testing.T) {
	body := `{"version":"1.0.0","notes":"` + strings.Repeat("x", maxManifestBytes) + `"}`
	if _, err := DecodeManifest([]byte(body), ""); !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("DecodeManifest = %v, want ErrProviderBadResponse", err)
	}
}

// 正常路径：`mygo build` 写出的内容变成界面展示的 release，并绑入调用方核验过的摘要。
func TestDecodeManifestReadsARelease(t *testing.T) {
	body := `{
		"version": "0.1.0-beta.6",
		"notes": "  - one change\n- another  ",
		"date": "2026-10-19T00:00:00Z",
		"url": "https://github.com/QiYuOr2/boku-desktop/releases/download/v0.1.0-beta.6/boku-0.1.0-beta.6-windows-amd64.tar.gz",
		"size": 1234567,
		"signature": "c2lnbmF0dXJl",
		"previous": [{"version":"0.1.0-beta.5","url":"https://example.invalid/old.tar.gz","size":42,"signature":"old"}],
		"deltas": [{"from":"0.1.0-beta.5","url":"https://example.invalid/d.delta","size":12,"signature":"delta"}]
	}`
	digest := strings.Repeat("a", 64)
	release, err := DecodeManifest([]byte(body), digest)
	if err != nil {
		t.Fatalf("DecodeManifest: %v", err)
	}
	if release.Version != "0.1.0-beta.6" {
		t.Errorf("version = %q", release.Version)
	}
	if release.Notes != "- one change\n- another" {
		t.Errorf("notes = %q", release.Notes)
	}
	asset, ok := release.Archive()
	if !ok {
		t.Fatalf("the release has no archive: %+v", release)
	}
	if asset.Size != 1234567 {
		t.Errorf("size = %d", asset.Size)
	}
	if asset.SHA256 != digest {
		t.Errorf("digest = %q, want the one the caller checked", asset.SHA256)
	}
	// deltas 与旧版本归档是 updater 自己的事务：无报错地读取，且不是本应用
	// 命名的归档。
	if len(release.Assets) != 1 {
		t.Errorf("the release names %d assets, want the one archive: %+v", len(release.Assets), release.Assets)
	}
}

// 带 "v" 的标签与不带的是同一版本：标签是仓库的写法，目录不是。
func TestDecodeManifestTrimsTheTagPrefix(t *testing.T) {
	body := `{"version":"v0.1.0-beta.6","url":"https://example.invalid/a.tar.gz","size":10,"signature":"sig"}`
	release, err := DecodeManifest([]byte(body), "")
	if err != nil {
		t.Fatalf("DecodeManifest: %v", err)
	}
	if release.Version != "0.1.0-beta.6" {
		t.Errorf("version = %q, want the tag without its v", release.Version)
	}
}

// 给出的摘要必须是 SHA-256：否则是无法核验下载的 release。
func TestDecodeManifestRefusesADigestThatIsNotOne(t *testing.T) {
	body := `{"version":"1.0.0","url":"https://example.invalid/a.tar.gz","size":10,"signature":"sig"}`
	if _, err := DecodeManifest([]byte(body), "abc"); !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("DecodeManifest = %v, want ErrProviderBadResponse", err)
	}
}

// MaxArchiveSize 是框架施加的上限；更新功能用同一数值，使一方拒绝的 release
// 不会被另一方提供。
func TestMaxArchiveSizeIsAFullGigabyte(t *testing.T) {
	if MaxArchiveSize != 1<<30 {
		t.Errorf("MaxArchiveSize = %d, want 1<<30", MaxArchiveSize)
	}
}
