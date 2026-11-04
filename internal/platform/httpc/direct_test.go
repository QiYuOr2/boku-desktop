package httpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestDirectTransportIgnoresTheSystemProxy(t *testing.T) {
	upstreamHit := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHit = true
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)

	proxyHit := false
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHit = true
		http.Error(w, "proxy", http.StatusBadGateway)
	}))
	t.Cleanup(proxy.Close)

	previous := readSystemProxy
	readSystemProxy = func() (proxySettings, error) {
		parsed, err := url.Parse(proxy.URL)
		if err != nil {
			t.Fatal(err)
		}
		return proxySettings{Enabled: true, Server: parsed.Host}, nil
	}
	t.Cleanup(func() { readSystemProxy = previous })
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("http_proxy", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("https_proxy", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("all_proxy", "")

	proxied := New(Options{})
	if _, err := proxied.GetBytes(context.Background(), upstream.URL, nil); err == nil {
		t.Fatal("the system proxy was not used")
	}
	if !proxyHit {
		t.Fatal("the proxied client never reached the proxy")
	}

	proxyHit = false
	upstreamHit = false
	direct := NewDirect(Options{})
	if _, err := direct.GetBytes(context.Background(), upstream.URL, nil); err != nil {
		t.Fatalf("direct request: %v", err)
	}
	if !upstreamHit || proxyHit {
		t.Fatalf("upstream %v proxy %v", upstreamHit, proxyHit)
	}
}
