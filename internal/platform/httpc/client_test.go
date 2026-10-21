package httpc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"boku/internal/domain"
)

func TestGetJSONReturnsThePayload(t *testing.T) {
	var gotHeader http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Clone()
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"is_available":true}`)
	}))
	defer server.Close()

	client := New(Options{HTTPClient: server.Client(), UserAgent: "Boku/0.1.0"})
	result, err := client.GetJSON(context.Background(), server.URL, map[string]string{"Authorization": "Bearer sk-test"})
	if err != nil {
		t.Fatalf("GetJSON failed: %v", err)
	}
	if string(result.Payload) != `{"is_available":true}` {
		t.Errorf("payload = %q", result.Payload)
	}
	if result.HTTPStatus != 200 {
		t.Errorf("status = %d, want 200", result.HTTPStatus)
	}
	if result.FetchedAt.IsZero() {
		t.Error("the result has no fetch time")
	}
	if got := gotHeader.Get("Authorization"); got != "Bearer sk-test" {
		t.Errorf("Authorization = %q", got)
	}
	if got := gotHeader.Get("User-Agent"); got != "Boku/0.1.0" {
		t.Errorf("User-Agent = %q", got)
	}
	if got := gotHeader.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q", got)
	}
}

func TestGetBytesReturnsBinaryPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{0x89, 'P', 'N', 'G'})
	}))
	defer server.Close()
	result, err := New(Options{HTTPClient: server.Client()}).GetBytes(context.Background(), server.URL, nil)
	if err != nil {
		t.Fatalf("GetBytes failed: %v", err)
	}
	if !bytes.Equal(result.Payload, []byte{0x89, 'P', 'N', 'G'}) {
		t.Errorf("payload = %v", result.Payload)
	}
}

func TestPostJSONSendsTheBody(t *testing.T) {
	var body string
	var contentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		contentType = r.Header.Get("Content-Type")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()

	client := New(Options{HTTPClient: server.Client()})
	if _, err := client.PostJSON(context.Background(), server.URL, nil, []byte(`{"api_name":"/x"}`)); err != nil {
		t.Fatalf("PostJSON failed: %v", err)
	}
	if body != `{"api_name":"/x"}` {
		t.Errorf("body = %q", body)
	}
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q", contentType)
	}
}

func TestStatusMapping(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   error
	}{
		{"unauthorized", 401, domain.ErrProviderUnauthorized},
		{"forbidden", 403, domain.ErrProviderUnauthorized},
		{"rate limited", 429, domain.ErrProviderRateLimited},
		{"server error", 500, domain.ErrProviderUnavailable},
		{"bad gateway", 502, domain.ErrProviderUnavailable},
		{"bad request", 400, domain.ErrProviderBadResponse},
		{"not found", 404, domain.ErrProviderBadResponse},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				_, _ = io.WriteString(w, `{"error":{"message":"nope"}}`)
			}))
			defer server.Close()

			client := New(Options{HTTPClient: server.Client()})
			_, err := client.GetJSON(context.Background(), server.URL, nil)
			if !errors.Is(err, c.want) {
				t.Fatalf("error = %v, want %v", err, c.want)
			}
			if c.want == domain.ErrProviderBadResponse {
				var bad *domain.BadResponseError
				if !errors.As(err, &bad) {
					t.Fatalf("error %v is not a BadResponseError", err)
				}
				if bad.HTTPStatus != c.status {
					t.Errorf("the error carries status %d, want %d", bad.HTTPStatus, c.status)
				}
			}
		})
	}
}

func TestNonJSONSuccessIsABadResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "<html>signed out</html>")
	}))
	defer server.Close()

	client := New(Options{HTTPClient: server.Client()})
	_, err := client.GetJSON(context.Background(), server.URL, nil)
	if !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("error = %v, want ErrProviderBadResponse", err)
	}
	if !strings.Contains(err.Error(), "not JSON") {
		t.Errorf("the error does not say what was wrong: %v", err)
	}
}

func TestLongResponseIsRefusedNotTruncated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"padding":"`+strings.Repeat("x", 200)+`"}`)
	}))
	defer server.Close()

	client := New(Options{HTTPClient: server.Client(), MaxBytes: 32})
	_, err := client.GetJSON(context.Background(), server.URL, nil)
	if !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("error = %v, want ErrProviderBadResponse", err)
	}
	if !strings.Contains(err.Error(), "larger than") {
		t.Errorf("the error does not explain the ceiling: %v", err)
	}
}

func TestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	client := New(Options{HTTPClient: server.Client(), Timeout: 30 * time.Millisecond})
	start := time.Now()
	_, err := client.GetJSON(context.Background(), server.URL, nil)
	if !errors.Is(err, domain.ErrProviderTimeout) {
		t.Fatalf("error = %v, want ErrProviderTimeout", err)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Errorf("the request took %s: the timeout did not bound it", elapsed)
	}
}

func TestPostJSONWithOptionsUsesOnlyTheRequestDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(60 * time.Millisecond)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	client := New(Options{HTTPClient: server.Client(), Timeout: 20 * time.Millisecond})
	if _, err := client.PostJSONWithOptions(context.Background(), server.URL, nil, []byte(`{}`), RequestOptions{Timeout: 150 * time.Millisecond}); err != nil {
		t.Fatalf("request-specific deadline was not used: %v", err)
	}
	if _, err := client.PostJSON(context.Background(), server.URL, nil, []byte(`{}`)); !errors.Is(err, domain.ErrProviderTimeout) {
		t.Errorf("ordinary request = %v, want ErrProviderTimeout", err)
	}
}

func TestCanceledContext(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer func() {
		close(release)
		server.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := New(Options{HTTPClient: server.Client()})
	_, err := client.GetJSON(ctx, server.URL, nil)
	if !errors.Is(err, domain.ErrProviderTimeout) {
		t.Fatalf("error = %v, want ErrProviderTimeout", err)
	}
}

func TestUnreachableServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close()

	client := New(Options{HTTPClient: &http.Client{}})
	_, err := client.GetJSON(context.Background(), url, nil)
	if !errors.Is(err, domain.ErrProviderUnavailable) {
		t.Fatalf("error = %v, want ErrProviderUnavailable", err)
	}
}

func TestErrorsNeverCarryTheCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"Authentication Fails"}}`)
	}))
	defer server.Close()

	client := New(Options{HTTPClient: server.Client()})
	endpoint := server.URL + "/user/balance?api_key=sk-super-secret&currency=CNY"
	_, err := client.GetJSON(context.Background(), endpoint, map[string]string{"Authorization": "Bearer sk-super-secret"})
	if err == nil {
		t.Fatal("the request did not fail")
	}
	message := err.Error()
	if strings.Contains(message, "sk-super-secret") {
		t.Fatalf("the error leaks the credential: %s", message)
	}
	if !strings.Contains(message, "api_key=redacted") {
		t.Errorf("the error does not show the redacted parameter: %s", message)
	}
	if !strings.Contains(message, "currency=CNY") {
		t.Errorf("redaction removed an innocent parameter: %s", message)
	}
}

func TestSnippetIsClipped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, strings.Repeat("a", 5000))
	}))
	defer server.Close()

	client := New(Options{HTTPClient: server.Client()})
	_, err := client.GetJSON(context.Background(), server.URL, nil)
	var bad *domain.BadResponseError
	if !errors.As(err, &bad) {
		t.Fatalf("error = %v, want a BadResponseError", err)
	}
	if len(bad.Snippet) > domain.MaxSnippetBytes {
		t.Errorf("the snippet is %d bytes, want at most %d", len(bad.Snippet), domain.MaxSnippetBytes)
	}
}

func TestBadResponseSnippetIsRedacted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"bad request","key":"sk-live-1234567890"}}`)
	}))
	defer server.Close()

	client := New(Options{HTTPClient: server.Client()})
	_, err := client.GetJSON(context.Background(), server.URL, nil)
	if err == nil {
		t.Fatal("the request did not fail")
	}
	if strings.Contains(err.Error(), "sk-live-1234567890") {
		t.Fatalf("the error leaks the credential: %s", err)
	}
}

// The client must not hold its request a moment longer than the deadline it
// promises, whatever the transport is doing.
func TestDefaultTimeoutApplies(t *testing.T) {
	client := New(Options{})
	if client.timeout != DefaultTimeout {
		t.Errorf("timeout = %s, want %s", client.timeout, DefaultTimeout)
	}
	if client.maxBytes != DefaultMaxBytes {
		t.Errorf("maxBytes = %d, want %d", client.maxBytes, DefaultMaxBytes)
	}
	if client.userAgent != "Boku" {
		t.Errorf("userAgent = %q, want Boku", client.userAgent)
	}
}
