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

// The values below make the application look like one browser to Bilibili's
// risk-control layer instead of a script that happens to speak HTTP.
//
// The web endpoints Bilibili protects (the space feed among them) want a
// self-consistent browser identity, an anonymous `buvid3` cookie and a WBI
// signature. Leaving any of the three out produces a rejection that the card
// would otherwise report as "unrecognised data":
//
//	no buvid3, even signed      HTTP 412, body {"code":-412,"message":"request was banned"}
//	buvid3 but no signature      HTTP 200, body {"code":-352}
//	all three                    HTTP 200, body {"code":0}
//
// The versions in the user agent and in sec-ch-ua must agree; a Firefox user
// agent beside a Chrome sec-ch-ua is itself a fingerprint, so they are written
// once, here, and never taken from the machine.
const (
	browserUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"
	browserSecChUa   = `"Not;A=Brand";v="99", "Google Chrome";v="138", "Chromium";v="138"`
)

// These are variables so that a test can point the session at a local server.
var (
	// webBaseURL is the site the anonymous identity cookie comes from. Asking
	// the API host directly does not hand one out.
	webBaseURL = "https://www.bilibili.com/"
	// navURL is where the WBI keys live. It answers without a session.
	navURL = "https://api.bilibili.com/x/web-interface/nav"
)

// session is the anonymous identity this application presents. It is created
// once per process and reused: a cookie that changes between requests is
// exactly what the risk-control layer treats as a bot.
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

// browserHeaders is the identity every Bilibili API request carries.
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

// headers returns the identity plus the cookies collected so far.
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
	// A stable order keeps the header identical between requests.
	sortStrings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+s.cookies[name])
	}
	return strings.Join(parts, "; ")
}

// ensureCookies fetches the anonymous identity once. A failure is not fatal:
// the caller's request may still work, and the next refresh tries again.
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

// absorbCookies keeps the name=value pairs an answer set.
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
		// Only anonymous identity cookies are kept. A session cookie a user
		// pasted somewhere is not this package's business.
		if name == "" || value == "" || !anonymousCookie(name) {
			continue
		}
		s.cookies[name] = value
	}
}

// anonymousCookie reports the cookies that identify a device rather than a
// person. These are the ones Bilibili requires for anonymous API access.
func anonymousCookie(name string) bool {
	switch name {
	case "buvid3", "buvid4", "b_nut", "buvid_fp", "_uuid", "b_lsid":
		return true
	}
	return false
}

// wbiKeys returns the signing keys, fetching them when absent or stale. The
// keys rotate roughly daily, so an old pair is replaced rather than used.
func (s *session) wbiKeysFor(ctx context.Context) (wbiKeys, bool) {
	s.mu.Lock()
	keys, at := s.keys, s.keysTime
	s.mu.Unlock()
	if keys.valid() && s.now().Sub(at) < 6*time.Hour {
		return keys, true
	}
	fresh, err := s.fetchWBIKeys(ctx)
	if err != nil {
		// An old pair is better than none: the signature only fails when the
		// keys are actually wrong, and the retry path replaces them.
		return keys, keys.valid()
	}
	s.mu.Lock()
	s.keys, s.keysTime = fresh, s.now()
	s.mu.Unlock()
	return fresh, true
}

// invalidateKeys forces the next request to fetch a new pair. It is what makes
// a -352 self-heal: that code means the signature was not accepted, and the
// usual reason is that the keys rotated under us.
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

// keyFromURL takes the file name of a key URL, which is the token itself.
func keyFromURL(raw string) string {
	if i := strings.LastIndex(raw, "/"); i >= 0 {
		raw = raw[i+1:]
	}
	if i := strings.LastIndex(raw, "."); i >= 0 {
		raw = raw[:i]
	}
	return raw
}

// signedURL signs path+params with the current keys. base is the host only.
func (s *session) signedURL(ctx context.Context, base, path string, params map[string]string) (string, bool) {
	keys, ok := s.wbiKeysFor(ctx)
	if !ok {
		return "", false
	}
	return base + path + "?" + signWBI(params, keys, s.now()), true
}

func sortStrings(v []string) { sort.Strings(v) }
