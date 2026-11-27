package openai

import "testing"

// 用户输入的地址被整理成应用将请求的地址；任何非纯 HTTP 的输入都变成空而非放行：即将
// 把凭据发往此处。
func TestNormalizeBaseURL(t *testing.T) {
	cases := map[string]string{
		"":                                  "",
		"   ":                               "",
		"api.example.com":                   "https://api.example.com",
		"api.example.com/v1":                "https://api.example.com/v1",
		"https://api.example.com/v1/":       "https://api.example.com/v1",
		"http://127.0.0.1:11434/v1":         "http://127.0.0.1:11434/v1",
		"HTTPS://API.EXAMPLE.COM/v1":        "https://API.EXAMPLE.COM/v1",
		"file:///etc/passwd":                "",
		"javascript:alert(1)":               "",
		"  https://api.example.com/v1  ":    "https://api.example.com/v1",
		"https://api.example.com/v1///":     "https://api.example.com/v1",
		"https://user:pass@example.com/v1":  "https://user:pass@example.com/v1",
		"ftp://example.com":                 "",
		"https://example.com/v1?key=secret": "https://example.com/v1?key=secret",
		"//example.com/v1":                  "",
		"example.com/v1#fragment":           "https://example.com/v1#fragment",
		"https://example.com/v1/chat/":      "https://example.com/v1/chat",
	}
	for in, want := range cases {
		if got := NormalizeBaseURL(in); got != want {
			t.Errorf("NormalizeBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// 无 scheme 的地址，外部服务用 https，本机服务用 http。
//
// 它是用户从本地服务文档里复制的地址——"127.0.0.1:11434/v1"——读成 https 就是向不讲 TLS 的
// 端口发请求：服务明明在跑，卡片却报连接失败，两边都说不出原因。
func TestNormalizeBaseURLPicksTheSchemeForTheHost(t *testing.T) {
	https := []string{
		"api.example.com/v1",
		"example.com:8080/v1",
		// 仅包含该词的主机名不算本机。
		"localhost.example.com/v1",
		"notlocalhost/v1",
	}
	for _, in := range https {
		if got := NormalizeBaseURL(in); got[:5] != "https" {
			t.Errorf("NormalizeBaseURL(%q) = %q, want https", in, got)
		}
	}
	httpCases := map[string]string{
		"localhost:11434/v1":      "http://localhost:11434/v1",
		"127.0.0.1:11434/v1":      "http://127.0.0.1:11434/v1",
		"127.0.0.1/v1":            "http://127.0.0.1/v1",
		"0.0.0.0:8080/v1":         "http://0.0.0.0:8080/v1",
		"localhost":               "http://localhost",
		"[::1]:8080/v1":           "http://[::1]:8080/v1",
		"LOCALHOST:11434/v1":      "http://LOCALHOST:11434/v1",
		"localhost:11434/v1/":     "http://localhost:11434/v1",
		"localhost/v1?x=1":        "http://localhost/v1?x=1",
		"localhost:11434/v1#frag": "http://localhost:11434/v1#frag",
	}
	for in, want := range httpCases {
		if got := NormalizeBaseURL(in); got != want {
			t.Errorf("NormalizeBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
	// 用户确实写了的 scheme 予以保留，无论指向哪台主机：显式 https 到本地端口是有意为之。
	if got := NormalizeBaseURL("https://localhost:11434/v1"); got != "https://localhost:11434/v1" {
		t.Errorf("an explicit scheme was overridden: %q", got)
	}
}
