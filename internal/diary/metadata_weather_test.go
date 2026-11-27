package diary

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

func TestMetadataFetcherHappyAndErrorCases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/article":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><head>
				<meta property="og:title" content="秋日 &amp; 散步 &#x27;记录&#x27;">
				<meta property="og:description" content="一段摘要">
				<meta property="og:site_name" content="Example Blog">
				<meta property="og:image" content="https://cdn.example.com/cover.png">
			</head><body></body></html>`))
		case "/http-img":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><head><title>纯标题</title><meta property="og:image" content="http://insecure.example.com/a.png"></head></html>`))
		case "/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"not":"html"}`))
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := httpc.New(httpc.Options{HTTPClient: srv.Client(), Timeout: 5 * time.Second})
	fetcher := NewMetadataFetcher(client, fixedClock{at: time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)})

	card, err := fetcher.Fetch(context.Background(), srv.URL+"/article")
	if err != nil {
		t.Fatalf("Fetch article: %v", err)
	}
	if card.Title != "秋日 & 散步 '记录'" || card.Summary != "一段摘要" || card.ImageURL == nil || *card.ImageURL != "https://cdn.example.com/cover.png" {
		t.Fatalf("unexpected card: %+v", card)
	}

	// 非 HTTPS 封面图必须被忽略，标题退回到 <title>。
	card2, err := fetcher.Fetch(context.Background(), srv.URL+"/http-img")
	if err != nil {
		t.Fatalf("Fetch http-img: %v", err)
	}
	if card2.Title != "纯标题" || card2.ImageURL != nil {
		t.Fatalf("http image should be dropped: %+v", card2)
	}

	// 非法协议拒绝。
	if _, err := fetcher.Fetch(context.Background(), "file:///etc/passwd"); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for file://, got %v", err)
	}
	// 非 HTML 响应返回 ErrProviderBadResponse。
	if _, err := fetcher.Fetch(context.Background(), srv.URL+"/json"); !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("expected ErrProviderBadResponse for non-html, got %v", err)
	}
	// 重定向超限失败。
	if _, err := fetcher.Fetch(context.Background(), srv.URL+"/loop"); err == nil {
		t.Fatal("expected redirect loop to fail")
	}
}

func TestWeatherServiceGeocodeForecastAndMalformed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/geo":
			name := r.URL.Query().Get("name")
			switch name {
			case "杭州":
				_, _ = w.Write([]byte(`{"results":[{"name":"杭州","latitude":30.2741,"longitude":120.1551,"admin1":"浙江省","country":"中国"}]}`))
			case "空结果":
				_, _ = w.Write([]byte(`{"results":[]}`))
			default:
				_, _ = w.Write([]byte(`not-json`))
			}
		case "/forecast":
			if r.URL.Query().Get("latitude") == "30.27" && r.URL.Query().Get("longitude") == "120.16" {
				_, _ = w.Write([]byte(`{"current_units":{"temperature_2m":"°C"},"current":{"time":"2026-04-20T04:00","temperature_2m":23.4,"weather_code":2}}`))
				return
			}
			_, _ = w.Write([]byte(`{"current":{"time":"2026-04-20T04:00","temperature_2m":999,"weather_code":0}}`))
		}
	}))
	defer srv.Close()

	client := httpc.New(httpc.Options{HTTPClient: srv.Client(), Timeout: 5 * time.Second})
	ws := NewWeatherServiceWithEndpoints(client, srv.URL+"/geo", srv.URL+"/forecast", fixedClock{at: time.Date(2026, 4, 20, 5, 0, 0, 0, time.UTC)})

	snap, err := ws.SnapshotForPlace(context.Background(), "杭州")
	if err != nil {
		t.Fatalf("SnapshotForPlace: %v", err)
	}
	if snap.Place != "杭州 · 浙江省" || snap.Description != "多云" || snap.SymbolName != "cloud.sun.fill" || snap.TemperatureCelsius != 23.4 || snap.Kind != "current" {
		t.Fatalf("unexpected weather snapshot: %+v", snap)
	}

	if _, err := ws.Geocode(context.Background(), ""); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("empty place should fail with ErrInvalidArgument, got %v", err)
	}
	if _, err := ws.Geocode(context.Background(), "空结果"); !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("empty geocode results should fail with ErrProviderBadResponse, got %v", err)
	}
	if _, err := ws.Geocode(context.Background(), "畸形"); !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("malformed geocode json should fail with ErrProviderBadResponse, got %v", err)
	}
	if _, err := ws.Snapshot(context.Background(), "异常", 10.0, 10.0); !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("out-of-range temp should fail with ErrProviderBadResponse, got %v", err)
	}
}

func TestImageCacheHTTPSOnlyAndCachesOnDisk(t *testing.T) {
	hits := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if strings.HasSuffix(r.URL.Path, ".txt") {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("not an image"))
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("fake-png-content"))
	}))
	defer srv.Close()

	cacheDir := t.TempDir()
	client := httpc.New(httpc.Options{HTTPClient: srv.Client(), Timeout: 5 * time.Second})
	cache := NewImageCache(cacheDir, client)

	if _, err := cache.Load(context.Background(), "http://example.com/a.png"); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("http image must be rejected: %v", err)
	}

	data1, err := cache.Load(context.Background(), srv.URL+"/pic.png#frag1")
	if err != nil || string(data1) != "fake-png-content" {
		t.Fatalf("first load failed: %q err=%v", string(data1), err)
	}
	// 同地址不同 fragment 应命中内存缓存。
	data2, err := cache.Load(context.Background(), srv.URL+"/pic.png#frag2")
	if err != nil || string(data2) != "fake-png-content" || hits != 1 {
		t.Fatalf("expected cache hit (hits=%d): %v", hits, err)
	}
	// 新实例应命中磁盘缓存。
	cache2 := NewImageCache(cacheDir, client)
	data3, err := cache2.Load(context.Background(), srv.URL+"/pic.png")
	if err != nil || string(data3) != "fake-png-content" || hits != 1 {
		t.Fatalf("expected disk cache hit (hits=%d): %v", hits, err)
	}

	if _, err := cache.Load(context.Background(), srv.URL+"/bad.txt"); !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("non-image content-type should return ErrProviderBadResponse, got %v", err)
	}
}
