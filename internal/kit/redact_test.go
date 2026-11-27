package kit

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://api.deepseek.com/user/balance", "https://api.deepseek.com/user/balance"},
		{"https://api.steampowered.com/IPlayerService/GetOwnedGames/v1/?key=ABC&steamid=1", "https://api.steampowered.com/IPlayerService/GetOwnedGames/v1/?key=redacted&steamid=1"},
		{"https://user:pass@example.com/x", "https://redacted@example.com/x"},
		{"not a url at all?token=abc", "not a url at all?token=redacted"},
	}
	for _, c := range cases {
		if got := Redact(c.in); got != c.want {
			t.Errorf("Redact(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRedactTextKeepsOrdinaryText(t *testing.T) {
	if got := RedactText("dial tcp: connection refused"); got != "dial tcp: connection refused" {
		t.Errorf("RedactText changed ordinary text: %q", got)
	}
	if got := RedactText("Get \"https://x/y?api_key=abc\": EOF"); strings.Contains(got, "abc") {
		t.Errorf("RedactText left the value in: %q", got)
	}
}

// 服务把凭据原样回显在错误里，正是本功能存在的场景：无论服务用了什么标点，
// 错误携带的片段都不得含有凭据。
func TestRedactTextRemovesCredentialFields(t *testing.T) {
	cases := map[string]string{
		`{"error":{"message":"failed","key":"sk-live-1234567890"}}`: `{"error":{"message":"failed","key":"redacted"}}`,
		`{"api_key": "sk-live-1234567890"}`:                         `{"api_key": "redacted"}`,
		`{"error":{"apiKey":"sk-live-1234567890"}}`:                 `{"error":{"apiKey":"redacted"}}`,
		`token=abc123&other=1`:                                      `token=redacted&other=1`,
		`{"access_token":"eyJhbGciOi"}`:                             `{"access_token":"redacted"}`,
		// 不含敏感信息的响应原样保留：脱敏不能吃掉这段 snippet 本身要提供的诊断信息。
		`{"model":"deepseek-chat","choices":[]}`: `{"model":"deepseek-chat","choices":[]}`,
	}
	for in, want := range cases {
		if got := RedactText(in); got != want {
			t.Errorf("RedactText(%q)\n = %q\nwant %q", in, got, want)
		}
	}
}

// 日志记录把属性打印为 `name=value`，名字才决定值是否为凭据：裸值本身没有可供识别的东西。
func TestRedactField(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{"apiKey", "sk-live-1234567890", "redacted"},
		{"api_key", "sk-live-1234567890", "redacted"},
		{"key", "abc123", "redacted"},
		{"token", "eyJhbGciOi", "redacted"},
		{"authorization", "Bearer abc123", "redacted"},
		{"panel", "deepseek", "deepseek"},
		{"note", "call ?key=abc failed", "call ?key=redacted failed"},
		// 名字不是凭据、值也不含凭据时，原样保留。
		{"status", "error", "error"},
		{"", "left alone", "left alone"},
		{"apiKey", "", ""},
	}
	for _, c := range cases {
		if got := RedactField(c.name, c.value); got != c.want {
			t.Errorf("RedactField(%q, %q) = %q, want %q", c.name, c.value, got, c.want)
		}
	}
}
