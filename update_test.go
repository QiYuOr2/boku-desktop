package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 更新装配把源、下载目录与重启串在一起，也是错误的目标名会变成永远找不到的发布的地方。与 services 一样，无需桌面会话即可检查。
func TestNewUpdatesAssemblesTheFeature(t *testing.T) {
	previousTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	userData := t.TempDir()
	svc, err := newUpdates(userData, "0.1.0-beta.5")
	if err != nil {
		t.Fatalf("newUpdates: %v", err)
	}
	if svc == nil {
		t.Fatal("newUpdates returned no service")
	}
	// 尚未询问过：状态为 Idle，目录是调用者指定用户数据下的那个。
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

// 清单 URL 由平台构造，名字是 `mygo build` 上传的那个。目标与构建自身不一致，就是永远返回 404 的源，看起来与「已是最新」一模一样。
func TestReleaseFeedNamesThisBuildsManifest(t *testing.T) {
	feed := releaseFeed()
	want := githubReleasesURL
	if feed != want {
		t.Errorf("releaseFeed() = %q, want %q", feed, want)
	}
	if target := updateTarget(); target == "" {
		t.Error("updateTarget() is empty")
	}
}

// 装配出的服务是 presentation 能接受的：*update.Service 满足 app.UpdateService，而 nil 是没有该功能的构建所传的。
func TestUpdateServiceIsWhatThePresentationAccepts(t *testing.T) {
	svc, err := newUpdates(t.TempDir(), "0.1.0")
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

func TestLatestManifestPrefersStableAndFallsBackToPrerelease(t *testing.T) {
	manifestName := "update-" + updateTarget() + ".json"
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			fmt.Fprintf(w, `[
				{"prerelease":true,"assets":[{"name":%q,"browser_download_url":%q}]},
				{"prerelease":false,"assets":[{"name":%q,"browser_download_url":%q}]}
			]`, manifestName, server.URL+"/beta.json", manifestName, server.URL+"/stable.json")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	u := &mygoUpdater{client: server.Client()}
	got, err := u.latestManifest(context.Background(), server.URL+"/releases")
	if err != nil {
		t.Fatalf("latestManifest: %v", err)
	}
	if want := server.URL + "/stable.json"; got != want {
		t.Errorf("latestManifest = %q, want %q", got, want)
	}
}

func TestLatestManifestUsesPrereleaseWhenNoStableReleaseExists(t *testing.T) {
	manifestName := "update-" + updateTarget() + ".json"
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `[{"prerelease":true,"assets":[{"name":%q,"browser_download_url":%q}]}]`, manifestName, server.URL+"/beta.json")
	}))
	defer server.Close()

	u := &mygoUpdater{client: server.Client()}
	got, err := u.latestManifest(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("latestManifest: %v", err)
	}
	if want := server.URL + "/beta.json"; got != want {
		t.Errorf("latestManifest = %q, want %q", got, want)
	}
}

// serveManifest 返回一个签名清单，版本名为 version。
func serveManifest(t *testing.T, version string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"version":%q,"notes":"n","url":%q,"size":1234,"signature":"c2ln"}`,
			version, "https://example.invalid/boku.tar.gz")
	}))
	t.Cleanup(server.Close)
	return server
}

// 此用例固化的 bug：最新发布的版本通常就是用户正在跑的版本，读回它的清单曾被报告为「更新就绪」。已经是最新即是最新。
func TestReleaseOfTheRunningVersionIsUpToDate(t *testing.T) {
	for _, c := range []struct{ current, feed string }{
		{"0.1.1", "0.1.1"},
		{"0.1.1", "v0.1.1"},
		{"0.1.0-beta.5", "0.1.0-beta.5"},
		// 领先于源的构建不会收到降级。
		{"0.1.2", "0.1.1"},
	} {
		server := serveManifest(t, c.feed)
		u := &mygoUpdater{client: server.Client()}
		got, err := u.release(context.Background(), server.URL, c.current)
		if err != nil {
			t.Fatalf("release(%s, feed %s): %v", c.current, c.feed, err)
		}
		if got != nil {
			t.Errorf("running %s against feed %s offered %q, want no release", c.current, c.feed, got.Version)
		}
	}
}

// 确实更新的源仍是一个发布：修复不得把每次检查都变成「已是最新」。
func TestReleaseOfANewerVersionIsOffered(t *testing.T) {
	for _, c := range []struct{ current, feed string }{
		{"0.1.0", "0.1.1"},
		{"0.1.0", "v0.1.1"},
		{"0.1.1", "0.1.2"},
		{"0.1.0-beta.5", "0.1.0"},
		{"0.1.1", "0.1.2-beta.1"},
	} {
		server := serveManifest(t, c.feed)
		u := &mygoUpdater{client: server.Client()}
		got, err := u.release(context.Background(), server.URL, c.current)
		if err != nil {
			t.Fatalf("release(%s, feed %s): %v", c.current, c.feed, err)
		}
		if got == nil {
			t.Fatalf("running %s against feed %s offered no release", c.current, c.feed)
		}
		// 发布携带的版本去掉了标签的 "v"，界面写的就是这个。
		if want := strings.TrimPrefix(c.feed, "v"); got.Version != want {
			t.Errorf("the offered version = %q, want %q", got.Version, want)
		}
	}
}
