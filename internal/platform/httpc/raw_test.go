package httpc

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"boku/internal/domain"
)

func TestRawReturnsNon2xxBodyAndHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("X-Custom") != "ok" {
			t.Errorf("X-Custom = %q", r.Header.Get("X-Custom"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"device_revoked"}}`))
	}))
	defer server.Close()

	client := New(Options{HTTPClient: server.Client()})
	resp, err := client.Raw(context.Background(), http.MethodPost, server.URL, map[string]string{"X-Custom": "ok"}, []byte(`{}`), RawOptions{})
	if err != nil {
		t.Fatalf("Raw: %v", err)
	}
	if resp.Status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.Status)
	}
	if !strings.Contains(resp.Headers["content-type"], "application/json") {
		t.Errorf("content-type = %q", resp.Headers["content-type"])
	}
	if !strings.Contains(string(resp.Body), "device_revoked") {
		t.Errorf("body = %q", string(resp.Body))
	}
}

func TestRawEnforcesMaxRedirectsAndMaxBytes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/big":
			_, _ = w.Write([]byte("1234567890"))
		}
	}))
	defer server.Close()

	client := New(Options{HTTPClient: server.Client()})
	if _, err := client.Raw(context.Background(), http.MethodGet, server.URL+"/loop", nil, nil, RawOptions{MaxRedirects: 3}); err == nil {
		t.Fatal("expected redirect limit error")
	}
	if _, err := client.Raw(context.Background(), http.MethodGet, server.URL+"/big", nil, nil, RawOptions{MaxBytes: 5}); !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("oversize err = %v, want ErrProviderBadResponse", err)
	}
}

func TestRawExactHeadersSkipsDefaultUserAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); ua == "BokuTest" {
			t.Errorf("unexpected custom default User-Agent: %q", ua)
		}
		if r.ContentLength != 4 {
			t.Errorf("ContentLength = %d, want 4", r.ContentLength)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(Options{HTTPClient: server.Client(), UserAgent: "BokuTest"})
	resp, err := client.Raw(context.Background(), http.MethodPut, server.URL, map[string]string{
		"Content-Length": "4",
		"Content-Type":   "image/jpeg",
	}, []byte("abcd"), RawOptions{ExactHeaders: true})
	if err != nil {
		t.Fatalf("Raw: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Status)
	}
}
