package openai

import "testing"

// The address a user typed is turned into one the application will ask, and
// anything that is not plain HTTP becomes nothing rather than being passed on:
// a credential is about to be sent there.
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
		"localhost:8080/v1":                 "https://localhost:8080/v1",
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
