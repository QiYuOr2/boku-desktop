package httpc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDownloadWritesPastTheMemoryCeiling(t *testing.T) {
	body := strings.Repeat("a", 2<<20+10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)

	dest := filepath.Join(t.TempDir(), "model.bin")
	if err := Download(context.Background(), server.URL, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("size %d, want %d", len(got), len(body))
	}
}

func TestDownloadResumesWithRange(t *testing.T) {
	payload := []byte("abcdefghijklmnopqrstuvwxyz")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "bytes=10-" {
			t.Errorf("range %q", r.Header.Get("Range"))
			http.Error(w, "range", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Range", "bytes 10-25/26")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[10:])
	}))
	t.Cleanup(server.Close)

	dest := filepath.Join(t.TempDir(), "model.bin")
	if err := os.WriteFile(dest, payload[:10], 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Download(context.Background(), server.URL, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("got %q", got)
	}
}

func TestDownloadCancelKeepsThePartialFile(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write(make([]byte, 32<<10))
		if flusher != nil {
			flusher.Flush()
		}
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	dest := filepath.Join(t.TempDir(), "model.bin")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Download(ctx, server.URL, dest) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("download did not start")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		info, err := os.Stat(dest)
		if err == nil && info.Size() > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("download wrote nothing before cancel")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("cancelled download returned nil")
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("cancelled download removed the partial file")
	}
}

func TestDownloadProgressCountsTheWholeFile(t *testing.T) {
	body := []byte("abcdef")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)

	var got []int64
	dest := filepath.Join(t.TempDir(), "model.bin")
	if err := DownloadProgress(context.Background(), server.URL, dest, func(n int64) {
		got = append(got, n)
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 || got[0] != 0 || got[len(got)-1] != int64(len(body)) {
		t.Fatalf("progress %v", got)
	}
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Fatalf("went backwards: %v", got)
		}
	}
}

func TestDownloadProgressIncludesTheResumedBytes(t *testing.T) {
	payload := []byte("abcdefghijklmnopqrstuvwxyz")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "bytes=10-" {
			http.Error(w, "range", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[10:])
	}))
	t.Cleanup(server.Close)

	dest := filepath.Join(t.TempDir(), "model.bin")
	if err := os.WriteFile(dest, payload[:10], 0o600); err != nil {
		t.Fatal(err)
	}
	var got []int64
	if err := DownloadProgress(context.Background(), server.URL, dest, func(n int64) {
		got = append(got, n)
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0] != 10 || got[len(got)-1] != int64(len(payload)) {
		t.Fatalf("progress %v", got)
	}
}

func TestDownloadStatusIsTheCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)

	err := Download(context.Background(), server.URL, filepath.Join(t.TempDir(), "model.bin"))
	var status *StatusError
	if !errors.As(err, &status) || status.Code != http.StatusBadGateway {
		t.Fatal(err)
	}
}
