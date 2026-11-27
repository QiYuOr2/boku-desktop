package bilibili

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"boku/internal/platform/httpc"
)

// 下面的值让应用对 B 站风控层看起来像一个浏览器，而不是一个碰巧会说 HTTP 的脚本。
//
// B 站保护的 web 接口（space feed 是其中之一）需要自洽的浏览器身份、匿名 buvid3
// cookie 和 WBI 签名。缺任一项都会得到拒绝，卡片会把它报成「无法识别的数据」：
//
//	没有 buvid3，即使已签名   HTTP 412, body {"code":-412,"message":"request was banned"}
//	有 buvid3 但没签名        HTTP 200, body {"code":-352}
//	三者齐全                  HTTP 200, body {"code":0}
//
// user agent 与 sec-ch-ua 的版本号必须一致；Firefox 的 UA 配 Chrome 的 sec-ch-ua
// 本身就是指纹，所以它们只在这里各写一次，绝不从本机取。
const (
	browserUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"
	browserSecChUa   = `"Not;A=Brand";v="99", "Google Chrome";v="138", "Chromium";v="138"`
)

// 这些是变量，方便测试把会话指向本地服务器。
var (
	// webBaseURL 是匿名身份 cookie 的来源站点。直接问 API 主机不会下发它。
	webBaseURL = "https://www.bilibili.com/"
	// navURL 是 WBI 密钥所在，无需会话即可应答。
	navURL = "https://api.bilibili.com/x/web-interface/nav"
)

// session 是本应用呈现的匿名身份。每进程创建一次并复用：请求间变化的 cookie 正是
// 风控层当作机器人的特征。
type session struct {
	client *httpc.Client
	now    func() time.Time

	mu       sync.Mutex
	cookies  map[string]string
	keys     wbiKeys
	keysTime time.Time
}

func newSession(client *httpc.Client, now func() time.Time) *session {
	return &session{client: client, now: now, cookies: make(map[string]string)}
}

// browserHeaders 是每个 B 站 API 请求携带的身份。
func browserHeaders() map[string]string {
	return map[string]string{
		"User-Agent":         browserUserAgent,
		"Accept":             "application/json, text/plain, */*",
		"Origin":             "https://www.bilibili.com",
		"Referer":            "https://www.bilibili.com/",
		"sec-ch-ua":          browserSecChUa,
		"sec-ch-ua-mobile":   "?0",
		"sec-ch-ua-platform": `"Linux"`,
		"sec-fetch-dest":     "empty",
		"sec-fetch-mode":     "cors",
		"sec-fetch-site":     "same-site",
		"priority":           "u=1, i",
	}
}

// headers 返回身份加上目前已收集的 cookie。
func (s *session) headers() map[string]string {
	h := browserHeaders()
	s.mu.Lock()
	cookie := s.cookieHeaderLocked()
	s.mu.Unlock()
	if cookie != "" {
		h["Cookie"] = cookie
	}
	return h
}

func (s *session) cookieHeaderLocked() string {
	if len(s.cookies) == 0 {
		return ""
	}
	names := make([]string, 0, len(s.cookies))
	for name := range s.cookies {
		names = append(names, name)
	}
	// 稳定顺序让请求间的该头部保持一致。
	sortStrings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+s.cookies[name])
	}
	return strings.Join(parts, "; ")
}

// ensureCookies 抓取一次匿名身份。失败不致命：调用方的请求可能仍然可用，下次刷新
// 再试。
func (s *session) ensureCookies(ctx context.Context) {
	s.mu.Lock()
	have := len(s.cookies) > 0
	s.mu.Unlock()
	if have {
		return
	}
	result, err := s.client.GetBytes(ctx, webBaseURL, browserHeaders())
	if err != nil {
		return
	}
	s.absorbCookies(result.Cookies)
}

// absorbCookies 保留应答下发的 name=value 对。
func (s *session) absorbCookies(pairs []string) {
	if len(pairs) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, pair := range pairs {
		name, value, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		// 只保留匿名身份 cookie。用户从别处粘贴的会话 cookie 不归本包管。
		if name == "" || value == "" || !anonymousCookie(name) {
			continue
		}
		s.cookies[name] = value
	}
}

// anonymousCookie 报告标识设备而非个人的 cookie。它们是 B 站匿名访问 API 所需的。
func anonymousCookie(name string) bool {
	switch name {
	case "buvid3", "buvid4", "b_nut", "buvid_fp", "_uuid", "b_lsid":
		return true
	}
	return false
}

// wbiKeysFor 返回签名密钥，缺失或过期时抓取。密钥约每天轮换，所以旧的一对会被替换
// 而非使用。
func (s *session) wbiKeysFor(ctx context.Context) (wbiKeys, bool) {
	s.mu.Lock()
	keys, at := s.keys, s.keysTime
	s.mu.Unlock()
	if keys.valid() && s.now().Sub(at) < 6*time.Hour {
		return keys, true
	}
	fresh, err := s.fetchWBIKeys(ctx)
	if err != nil {
		// 旧的一对比没有好：密钥真的错了签名才会失败，并且重试路径会替换它们。
		return keys, keys.valid()
	}
	s.mu.Lock()
	s.keys, s.keysTime = fresh, s.now()
	s.mu.Unlock()
	return fresh, true
}

// invalidateKeys 强制下一个请求抓取一对新密钥。它让 -352 自愈：该码意味着签名未被
// 接受，而通常的原因是密钥在我们脚下轮换了。
func (s *session) invalidateKeys() {
	s.mu.Lock()
	s.keys, s.keysTime = wbiKeys{}, time.Time{}
	s.mu.Unlock()
}

type navResponse struct {
	Code int `json:"code"`
	Data struct {
		WBIImg struct {
			ImgURL string `json:"img_url"`
			SubURL string `json:"sub_url"`
		} `json:"wbi_img"`
	} `json:"data"`
}

func (s *session) fetchWBIKeys(ctx context.Context) (wbiKeys, error) {
	result, err := s.client.GetJSON(ctx, navURL, s.headers())
	if err != nil {
		return wbiKeys{}, err
	}
	s.absorbCookies(result.Cookies)
	var nav navResponse
	if err := json.Unmarshal(result.Payload, &nav); err != nil {
		return wbiKeys{}, fmt.Errorf("the nav answer is not an object: %w", err)
	}
	keys := wbiKeys{imgKey: keyFromURL(nav.Data.WBIImg.ImgURL), subKey: keyFromURL(nav.Data.WBIImg.SubURL)}
	if !keys.valid() {
		return wbiKeys{}, fmt.Errorf("the nav answer carries no WBI keys")
	}
	return keys, nil
}

// keyFromURL 取密钥 URL 的文件名，也就是 token 本身。
func keyFromURL(raw string) string {
	if i := strings.LastIndex(raw, "/"); i >= 0 {
		raw = raw[i+1:]
	}
	if i := strings.LastIndex(raw, "."); i >= 0 {
		raw = raw[:i]
	}
	return raw
}

// signedURL 用当前密钥对 path+params 签名。base 仅为主机部分。
func (s *session) signedURL(ctx context.Context, base, path string, params map[string]string) (string, bool) {
	keys, ok := s.wbiKeysFor(ctx)
	if !ok {
		return "", false
	}
	return base + path + "?" + signWBI(params, keys, s.now()), true
}

func sortStrings(v []string) { sort.Strings(v) }
