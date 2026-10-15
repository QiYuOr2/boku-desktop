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

// A service that echoes a credential back inside its error is the case this
// exists for: the fragment an error carries must not hold one, whatever
// punctuation the service used.
func TestRedactTextRemovesCredentialFields(t *testing.T) {
	cases := map[string]string{
		`{"error":{"message":"failed","key":"sk-live-1234567890"}}`: `{"error":{"message":"failed","key":"redacted"}}`,
		`{"api_key": "sk-live-1234567890"}`:                         `{"api_key": "redacted"}`,
		`{"error":{"apiKey":"sk-live-1234567890"}}`:                 `{"error":{"apiKey":"redacted"}}`,
		`token=abc123&other=1`:                                      `token=redacted&other=1`,
		`{"access_token":"eyJhbGciOi"}`:                             `{"access_token":"redacted"}`,
		// A response with nothing sensitive in it is left exactly as it was:
		// redaction must not eat the diagnostic the snippet is for.
		`{"model":"deepseek-chat","choices":[]}`: `{"model":"deepseek-chat","choices":[]}`,
	}
	for in, want := range cases {
		if got := RedactText(in); got != want {
			t.Errorf("RedactText(%q)\n = %q\nwant %q", in, got, want)
		}
	}
}

// A log record prints an attribute as `name=value`, and the name is what
// says whether the value is a credential: a bare key has nothing else in it
// to recognize.
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
		// A name that is not a credential, with a value that carries no
		// credential either, is left exactly as it was.
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
