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

// An address without a scheme is https for a service somewhere else, and http
// for one on this machine.
//
// It is the address a user copies out of a local service's own documentation —
// "127.0.0.1:11434/v1" — and reading it as https is a request to a port that
// speaks no TLS: the service is running, the card says the connection failed,
// and nothing on either side says why.
func TestNormalizeBaseURLPicksTheSchemeForTheHost(t *testing.T) {
	https := []string{
		"api.example.com/v1",
		"example.com:8080/v1",
		// A hostname that merely contains the word is not the machine.
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
	// A scheme the user did write is kept, whichever host it names: an
	// explicit https to a local port is a deliberate thing to ask for.
	if got := NormalizeBaseURL("https://localhost:11434/v1"); got != "https://localhost:11434/v1" {
		t.Errorf("an explicit scheme was overridden: %q", got)
	}
}
