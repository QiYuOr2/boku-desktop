package diary

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

// HTMLMetadata 是从外链网页 <head> 解析出的 Open Graph / Twitter Card 元数据。
type HTMLMetadata struct {
	Title    string
	Summary  string
	SiteName string
	ImageURL *string
}

var (
	metaTagRe   = regexp.MustCompile(`(?i)<meta\b[^>]*>`)
	titleTagRe  = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title>`)
	attrRe      = regexp.MustCompile(`([A-Za-z_:][-A-Za-z0-9_:.]*)\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	numEntityRe = regexp.MustCompile(`&#(x?[0-9A-Fa-f]+);`)
)

// ParseHTMLMetadata 从 HTML 片段提取标题、摘要、站点名与 HTTPS 封面图地址。
func ParseHTMLMetadata(htmlText string, baseURL *url.URL) HTMLMetadata {
	values := make(map[string]string)
	for _, tag := range metaTagRe.FindAllString(htmlText, -1) {
		attrs := parseHTMLAttrs(tag)
		key := attrs["property"]
		if key == "" {
			key = attrs["name"]
		}
		key = strings.ToLower(strings.TrimSpace(key))
		content, ok := attrs["content"]
		if key == "" || !ok {
			continue
		}
		if _, exists := values[key]; !exists {
			values[key] = decodeHTMLEntities(content)
		}
	}

	title := values["og:title"]
	if title == "" {
		title = values["twitter:title"]
	}
	if title == "" {
		if m := titleTagRe.FindStringSubmatch(htmlText); len(m) == 2 {
			title = decodeHTMLEntities(m[1])
		}
	}
	summary := values["og:description"]
	if summary == "" {
		summary = values["twitter:description"]
	}
	if summary == "" {
		summary = values["description"]
	}
	siteName := values["og:site_name"]

	var imageURL *string
	rawImg := values["og:image"]
	if rawImg == "" {
		rawImg = values["twitter:image"]
	}
	if rawImg != "" && baseURL != nil {
		if ref, err := url.Parse(strings.TrimSpace(rawImg)); err == nil {
			resolved := baseURL.ResolveReference(ref).String()
			if clean, ok := ValidateImageURL(resolved); ok {
				imageURL = &clean
			}
		}
	}

	return HTMLMetadata{
		Title:    strings.TrimSpace(title),
		Summary:  strings.TrimSpace(summary),
		SiteName: strings.TrimSpace(siteName),
		ImageURL: imageURL,
	}
}

func parseHTMLAttrs(tag string) map[string]string {
	out := make(map[string]string)
	for _, m := range attrRe.FindAllStringSubmatch(tag, -1) {
		key := strings.ToLower(m[1])
		val := m[2]
		if val == "" {
			val = m[3]
		}
		out[key] = val
	}
	return out
}

func decodeHTMLEntities(value string) string {
	r := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#39;", "'",
		"&#x27;", "'",
		"&nbsp;", " ",
	)
	decoded := r.Replace(value)
	return numEntityRe.ReplaceAllStringFunc(decoded, func(match string) string {
		inner := match[2 : len(match)-1]
		var code uint64
		var err error
		if strings.HasPrefix(inner, "x") || strings.HasPrefix(inner, "X") {
			code, err = strconv.ParseUint(inner[1:], 16, 32)
		} else {
			code, err = strconv.ParseUint(inner, 10, 32)
		}
		if err != nil || !utf8.ValidRune(rune(code)) {
			return match
		}
		return string(rune(code))
	})
}

// MetadataFetcher 按需抓取外链网页的标题、摘要与 HTTPS 封面图（最多 512 KiB、3 次跳转）。
type MetadataFetcher struct {
	http RawRequester
	now  func() time.Time
}

func NewMetadataFetcher(http RawRequester, clock domain.Clock) *MetadataFetcher {
	if clock == nil {
		clock = domain.SystemClock{}
	}
	return &MetadataFetcher{http: http, now: clock.Now}
}

func (f *MetadataFetcher) Fetch(ctx context.Context, rawURL string) (LinkCard, error) {
	cleanURL, ok := ValidateWebURL(rawURL)
	if !ok {
		return LinkCard{}, fmt.Errorf("%w: 只支持 HTTP 或 HTTPS 链接", domain.ErrInvalidArgument)
	}
	if f == nil || f.http == nil {
		return LinkCard{}, domain.ErrProviderUnavailable
	}
	resp, err := f.http.Raw(ctx, "GET", cleanURL, map[string]string{
		"Accept":     "text/html,application/xhtml+xml",
		"User-Agent": "Boku/0.5 (https://github.com/QiYuOr2/boku)",
	}, nil, httpc.RawOptions{
		Timeout:      12 * time.Second,
		MaxBytes:     MaxMetadataHTMLBytes,
		MaxRedirects: 3,
	})
	if err != nil {
		return LinkCard{}, err
	}
	if resp.Status < 200 || resp.Status > 299 {
		return LinkCard{}, &domain.BadResponseError{
			ProviderID: "diary.link",
			HTTPStatus: resp.Status,
			Reason:     "网页返回异常状态码",
		}
	}
	finalStr := resp.FinalURL
	if finalStr == "" {
		finalStr = cleanURL
	}
	finalClean, ok := ValidateWebURL(finalStr)
	if !ok {
		return LinkCard{}, &domain.BadResponseError{
			ProviderID: "diary.link",
			Reason:     "链接跳转到了不支持的地址",
		}
	}
	ct := strings.ToLower(resp.Headers["content-type"])
	if !strings.Contains(ct, "text/html") && !strings.Contains(ct, "application/xhtml+xml") {
		return LinkCard{}, &domain.BadResponseError{
			ProviderID: "diary.link",
			Reason:     "链接不是网页",
		}
	}
	baseParsed, err := url.Parse(finalClean)
	if err != nil {
		return LinkCard{}, &domain.BadResponseError{ProviderID: "diary.link", Reason: "无效网页地址"}
	}
	htmlText := string(resp.Body)
	if !utf8.Valid(resp.Body) {
		htmlText = latin1ToUTF8(resp.Body)
	}
	meta := ParseHTMLMetadata(htmlText, baseParsed)
	host := baseParsed.Host
	if host == "" {
		host = meta.SiteName
	}
	title := meta.Title
	if title == "" {
		if meta.SiteName != "" {
			title = meta.SiteName
		} else {
			title = host
		}
	}
	id, err := domain.NewID()
	if err != nil {
		return LinkCard{}, err
	}
	now := f.now().UTC()
	return LinkCard{
		ID:           id,
		URL:          cleanURL,
		Title:        title,
		Summary:      meta.Summary,
		Domain:       host,
		ImageURL:     meta.ImageURL,
		FetchedAt:    &now,
		ShowsPreview: true,
		FetchFailed:  false,
	}, nil
}

func latin1ToUTF8(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b) * 2)
	for _, c := range b {
		sb.WriteRune(rune(c))
	}
	return sb.String()
}
