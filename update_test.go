package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo"

	"boku/internal/domain"
)

func TestNewUpdatesAssemblesTheFeature(t *testing.T) {
	previousTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	userData := t.TempDir()
	svc, err := newUpdates(userData, "0.5.1")
	if err != nil {
		t.Fatalf("newUpdates: %v", err)
	}
	if svc == nil {
		t.Fatal("newUpdates returned no service")
	}
	if state := svc.State(); state.Status != 0 {
		t.Errorf("the fresh state = %v, want idle", state.Status)
	}
	if _, err := os.Stat(filepath.Join(userData, "updates")); err == nil {
		t.Error("the download directory was created before anything was downloaded")
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok || transport.Proxy == nil {
		t.Error("newUpdates did not give MyGo's default client the proxy-aware transport")
	}
}

func TestUpdateServiceIsWhatThePresentationAccepts(t *testing.T) {
	svc, err := newUpdates(t.TempDir(), "0.5.1")
	if err != nil {
		t.Fatalf("newUpdates: %v", err)
	}
	if got := updateService(svc); got == nil {
		t.Fatal("the assembled service is nil to the presentation")
	}
	if got := updateService(nil); got != nil {
		t.Error("a build without updates passes a non-nil service")
	}
}

func TestMyGoUpdaterReleaseVersionComparisonAnd404(t *testing.T) {
	// 运行版本已最新或领先时返回 nil。
	for _, c := range []struct{ current, offered string }{
		{"0.5.2", "0.5.2"},
		{"0.5.2", "v0.5.2"},
		{"0.5.3", "0.5.2"},
	} {
		u := &mygoUpdater{
			enabledFn: func() bool { return true },
			checkFn: func(context.Context) (*mygo.Update, error) {
				return &mygo.Update{Version: c.offered, Notes: "notes"}, nil
			},
		}
		got, err := u.Release(context.Background(), c.current)
		if err != nil || got != nil {
			t.Errorf("current=%s offered=%s: got=(%v, %v), want (nil, nil)", c.current, c.offered, got, err)
		}
	}

	// 新版本去掉 v 前缀并缓存待安装对象。
	u := &mygoUpdater{
		enabledFn: func() bool { return true },
		checkFn: func(context.Context) (*mygo.Update, error) {
			return &mygo.Update{Version: "v0.5.2", Notes: "  更新说明  "}, nil
		},
	}
	got, err := u.Release(context.Background(), "0.5.1")
	if err != nil || got == nil || got.Version != "0.5.2" || got.Notes != "更新说明" {
		t.Fatalf("newer release mismatch: got=%+v err=%v", got, err)
	}
	if cached := u.takePending("0.5.2"); cached == nil || strings.TrimPrefix(cached.Version, "v") != "0.5.2" {
		t.Fatalf("expected pending update cached for 0.5.2, got %+v", cached)
	}

	// 404 视为尚无发布版本（已是最新）。
	u404 := &mygoUpdater{
		enabledFn: func() bool { return true },
		checkFn: func(context.Context) (*mygo.Update, error) {
			return nil, errors.New("mygo: checking for updates: GET https://github.com/x/y/releases/latest/download/update.json: 404 Not Found")
		},
	}
	if got, err := u404.Release(context.Background(), "0.5.1"); err != nil || got != nil {
		t.Fatalf("404 should return (nil, nil), got (%+v, %v)", got, err)
	}

	// ErrUpdatesDisabled 映射为 domain.ErrUpdateUnsupported。
	uDisabled := &mygoUpdater{
		enabledFn: func() bool { return true },
		checkFn: func(context.Context) (*mygo.Update, error) {
			return nil, mygo.ErrUpdatesDisabled
		},
	}
	if _, err := uDisabled.Release(context.Background(), "0.5.1"); !errors.Is(err, domain.ErrUpdateUnsupported) {
		t.Fatalf("ErrUpdatesDisabled should map to ErrUpdateUnsupported, got %v", err)
	}
}
