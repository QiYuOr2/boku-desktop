package anime

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

func TestReleasesFiltersMainEpisodesInWindow(t *testing.T) {
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	items, err := releases(1, "动画", "https://example.test/cover.jpg", []byte(`{"data":[{"id":2,"type":0,"sort":2,"name":"二","airdate":"2026-10-20"},{"id":3,"type":1,"sort":1,"name":"SP","airdate":"2026-10-20"},{"id":4,"type":0,"sort":3,"name":"明天","airdate":"2026-10-21"}]}`), start, start.AddDate(0, 0, 1))
	if err != nil || len(items) != 1 || items[0].EpisodeName != "二" {
		t.Fatalf("releases = %#v, %v", items, err)
	}
}

func TestReleasesRejectsMalformedAirdate(t *testing.T) {
	_, err := releases(1, "动画", "", []byte(`{"data":[{"id":2,"type":0,"name":"二","airdate":"nope"}]}`), time.Now(), time.Now().AddDate(0, 0, 1))
	if !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("error = %v", err)
	}
}

func TestServiceFetchesWatchingAnimeAndTodayEpisodes(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("User-Agent"), "Boku") {
			t.Error("User-Agent missing")
		}
		switch r.URL.Path {
		case "/v0/users/sai/collections":
			_, _ = w.Write([]byte(`{"total":1,"data":[{"subject_id":7,"subject":{"name_cn":"测试动画"}}]}`))
		case "/v0/episodes":
			if r.URL.Query().Get("subject_id") != "7" || r.URL.Query().Get("type") != "0" {
				t.Errorf("query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"data":[{"id":8,"type":0,"sort":2,"name_cn":"昨天的第二集","airdate":"2026-10-19"},{"id":9,"type":0,"sort":3,"name_cn":"第三集","airdate":"2026-10-20"}]}`))
		default:
			t.Errorf("path = %s", r.URL.Path)
		}
	}))
	defer server.Close()
	old := bangumiAPIBaseURL
	bangumiAPIBaseURL = server.URL
	defer func() { bangumiAPIBaseURL = old }()
	section := NewSection()
	section.settings.Username = "sai"
	s, err := New(httpc.New(httpc.Options{UserAgent: "Boku/test"}), section, nil)
	if err != nil {
		t.Fatal(err)
	}
	// At 00:30 Shanghai time, yesterday's date must already be excluded.
	s.now = func() time.Time { return time.Date(2026, 10, 19, 16, 30, 0, 0, time.UTC) }
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	items, _, _, err := s.View()
	if err != nil || len(items) != 1 || items[0].SubjectName != "测试动画" {
		t.Fatalf("view = %#v, %v", items, err)
	}
}

func TestServiceLoadsCollectionCoverWithoutFailingTheReleaseList(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/users/sai/collections":
			_, _ = w.Write([]byte(`{"total":1,"data":[{"subject_id":7,"subject":{"name_cn":"测试动画","images":{"grid":"` + server.URL + `/cover.png"}}}]}`))
		case "/v0/episodes":
			_, _ = w.Write([]byte(`{"data":[{"id":9,"type":0,"sort":3,"name_cn":"第三集","airdate":"2026-10-20"}]}`))
		case "/cover.png":
			w.Header().Set("Content-Type", "image/png")
			cover := image.NewNRGBA(image.Rect(0, 0, 1, 1))
			cover.Set(0, 0, color.NRGBA{R: 234, G: 145, B: 153, A: 255})
			if err := png.Encode(w, cover); err != nil {
				t.Error(err)
			}
		default:
			t.Errorf("path = %s", r.URL.Path)
		}
	}))
	defer server.Close()
	old := bangumiAPIBaseURL
	bangumiAPIBaseURL = server.URL
	defer func() { bangumiAPIBaseURL = old }()
	section := NewSection()
	section.Set(Settings{Enabled: true, Username: "sai"})
	service, err := New(httpc.New(httpc.Options{UserAgent: "Boku/test"}), section, nil)
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC) }
	if err := service.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cover := service.Cover(7); len(cover) == 0 {
		t.Fatal("cover was not cached")
	} else if _, err := png.Decode(bytes.NewReader(cover)); err != nil {
		t.Fatalf("cached cover is invalid: %v", err)
	}
}
