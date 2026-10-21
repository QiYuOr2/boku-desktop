package bilibili

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

func TestRefreshPlacesLiveCreatorFirst(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/polymer/web-dynamic/v1/feed/space":
			if r.URL.Query().Get("host_mid") == "2" {
				_, _ = w.Write([]byte(`{"code":0,"data":{"items":[{"id_str":"22","type":"DYNAMIC_TYPE_AV","modules":{"module_author":{"mid":2,"name":"视频 UP","face":"https://example.test/2","pub_ts":1700000000},"module_dynamic":{"desc":{"text":"新视频"},"major":{"archive":{"jump_url":"//www.bilibili.com/video/BV1","title":"标题"}}}}}]}}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"items":[{"id_str":"11","type":"DYNAMIC_TYPE_WORD","modules":{"module_author":{"mid":1,"name":"直播 UP","face":"https://example.test/1","pub_ts":1600000000},"module_dynamic":{"desc":{"text":"动态"}}}}]}}`))
		case "/room/v1/Room/getRoomInfoOld":
			if r.URL.Query().Get("mid") == "1" {
				_, _ = w.Write([]byte(`{"code":0,"data":{"room_id":7,"title":"正在播","live_status":1,"online":12}}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"room_id":8,"live_status":0}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	oldAPI, oldLive := apiBaseURL, liveBaseURL
	apiBaseURL, liveBaseURL = server.URL, server.URL
	defer func() { apiBaseURL, liveBaseURL = oldAPI, oldLive }()
	section := NewSection()
	section.Set(Settings{Enabled: true, UIDs: []int64{1, 2}, RefreshMinutes: 5, DynamicCount: 1})
	service, err := New(httpc.New(httpc.Options{}), section, nil)
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return time.Unix(1800000000, 0) }
	if err := service.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	items, _, _, err := service.View()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || !items[0].Live || items[0].CreatorID != 1 {
		t.Fatalf("items = %#v", items)
	}
	if items[0].URL != "https://live.bilibili.com/7" || items[1].URL != "https://www.bilibili.com/video/BV1" {
		t.Fatalf("urls = %#v", items)
	}
}

func TestRefreshRejectsMalformedDynamic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"code":0,"data":{"items":[]}}`)) }))
	defer server.Close()
	old := apiBaseURL
	apiBaseURL = server.URL
	defer func() { apiBaseURL = old }()
	section := NewSection()
	section.Set(Settings{Enabled: true, UIDs: []int64{1}})
	service, _ := New(httpc.New(httpc.Options{}), section, nil)
	err := service.Refresh(context.Background())
	if !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("error=%v", err)
	}
}

func TestDecodeDynamicsReturnsConfiguredNewestItems(t *testing.T) {
	payload := []byte(`{"code":0,"data":{"items":[
		{"id_str":"new","type":"DYNAMIC_TYPE_WORD","modules":{"module_author":{"mid":1,"name":"UP","pub_ts":1700000100},"module_dynamic":{"desc":{"text":"最新"}}}},
		{"id_str":"old","type":"DYNAMIC_TYPE_AV","modules":{"module_author":{"mid":1,"name":"UP","pub_ts":1700000000},"module_dynamic":{"major":{"archive":{"jump_url":"//www.bilibili.com/video/BV1","title":"旧视频"}}}}}
	]}}`)
	items, err := decodeDynamics(1, payload, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Summary != "最新" || items[1].Kind != "视频" || items[1].URL != "https://www.bilibili.com/video/BV1" {
		t.Fatalf("items = %#v", items)
	}
}

func TestParseUID(t *testing.T) {
	for _, raw := range []string{"672328094", "https://space.bilibili.com/672328094?spm_id_from=333", "space.bilibili.com/672328094"} {
		if got, err := ParseUID(raw); err != nil || got != 672328094 {
			t.Fatalf("ParseUID(%q) = %d, %v", raw, got, err)
		}
	}
	if _, err := ParseUID("space.bilibili.com/not-a-number"); err == nil {
		t.Fatal("invalid UID accepted")
	}
}
