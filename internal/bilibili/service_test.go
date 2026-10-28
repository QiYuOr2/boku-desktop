package bilibili

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

// feedJSON wraps items in the space feed's envelope.
func feedJSON(items string) string {
	return `{"code":0,"message":"0","data":{"has_more":false,"items":[` + items + `]}}`
}

// itemJSON builds one space feed item in the shape the web endpoint returns:
// one modules object, the author under module_author, the body under
// module_dynamic.
func itemJSON(id, kind string, mid int64, name, face string, pubTS int64, desc, archiveBVID, archiveTitle string) string {
	parts := make([]string, 0, 2)
	if desc != "" {
		parts = append(parts, `"desc":{"text":"`+desc+`"}`)
	}
	if archiveTitle != "" {
		parts = append(parts, `"major":{"archive":{"bvid":"`+archiveBVID+`","title":"`+archiveTitle+`","jump_url":"//www.bilibili.com/video/`+archiveBVID+`"}}`)
	}
	return `{"id_str":"` + id + `","type":"` + kind + `","modules":{` +
		`"module_author":{"mid":` + strconv.FormatInt(mid, 10) + `,"name":"` + name + `","face":"` + face + `","pub_ts":` + strconv.FormatInt(pubTS, 10) + `},` +
		`"module_dynamic":{` + strings.Join(parts, ",") + `}}}`
}

const navKeys = `{"code":-101,"message":"账号未登录","data":{"wbi_img":{"img_url":"https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png","sub_url":"https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png"}}}`

// feedServer stands in for Bilibili: the site bootstrap, the nav keys, the
// space feed and the live status.
type feedServer struct {
	mu         sync.Mutex
	feeds      map[string]string
	live       map[string]string
	liveStatus map[string]string
	requests   []string
	feedStatus int
	navBody    string
	feedCalls  int
}

func (f *feedServer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.feedCalls
}

// newFeedServer points the package at a test server and restores it afterwards.
func newFeedServer(t *testing.T) *feedServer {
	t.Helper()
	f := &feedServer{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.URL.Path)
		f.mu.Unlock()
		switch {
		case r.URL.Path == "/":
			w.Header().Add("Set-Cookie", "buvid3=test-buvid; Path=/; Max-Age=86400")
			_, _ = w.Write([]byte("<html></html>"))
		case r.URL.Path == "/x/web-interface/nav":
			f.mu.Lock()
			body := f.navBody
			f.mu.Unlock()
			if body == "" {
				body = navKeys
			}
			_, _ = w.Write([]byte(body))
		case r.URL.Path == dynamicFeedPath:
			f.mu.Lock()
			f.feedCalls++
			status := f.feedStatus
			body, ok := f.feeds[r.URL.Query().Get("host_mid")]
			f.mu.Unlock()
			// A request without a signature or without the identity cookie is
			// what Bilibili blocks, so the stand-in blocks it the same way.
			if r.URL.Query().Get("w_rid") == "" || r.URL.Query().Get("wts") == "" ||
				!strings.Contains(r.Header.Get("Cookie"), "buvid3=") {
				_, _ = w.Write([]byte(`{"code":-412,"message":"request was banned"}`))
				return
			}
			if status != 0 {
				w.WriteHeader(status)
				_, _ = w.Write([]byte("<html>blocked</html>"))
				return
			}
			if !ok {
				body = feedJSON("")
			}
			_, _ = w.Write([]byte(body))
		case strings.HasPrefix(r.URL.Path, "/bfs/face/"):
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(testPNG)
		case r.URL.Path == "/room/v1/Room/get_status_info_by_uids":
			uid := r.URL.Query().Get("uids[]")
			f.mu.Lock()
			body, ok := f.liveStatus[uid]
			f.mu.Unlock()
			if !ok {
				// Quiet by default: a room that is not on air.
				body = `{"code":0,"data":{"` + uid + `":{"live_status":0,"uid":` + uid + `}}}`
			}
			_, _ = w.Write([]byte(body))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	oldAPI, oldLive, oldWeb, oldNav := apiBaseURL, liveBaseURL, webBaseURL, navURL
	apiBaseURL, liveBaseURL = server.URL, server.URL
	webBaseURL = server.URL + "/"
	navURL = server.URL + "/x/web-interface/nav"
	t.Cleanup(func() {
		apiBaseURL, liveBaseURL, webBaseURL, navURL = oldAPI, oldLive, oldWeb, oldNav
		server.Close()
	})
	return f
}

// testPNG is a decodable 1x1 PNG.
var testPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
	0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
	0x42, 0x60, 0x82,
}

func newTestService(t *testing.T, settings Settings) *Service {
	t.Helper()
	section := NewSection()
	section.Set(settings)
	service, err := New(httpc.New(httpc.Options{}), section, nil)
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return time.Unix(1800000000, 0) }
	service.session.now = service.now
	return service
}

func TestRefreshPlacesLiveCreatorFirst(t *testing.T) {
	f := newFeedServer(t)
	f.feeds = map[string]string{
		"1": feedJSON(itemJSON("11", "DYNAMIC_TYPE_WORD", 1, "直播 UP", "https://i0.hdslb.com/bfs/face/1.png", 1600000000, "动态", "", "")),
		"2": feedJSON(itemJSON("22", "DYNAMIC_TYPE_AV", 2, "视频 UP", "https://i0.hdslb.com/bfs/face/2.png", 1700000000, "新视频", "BV1", "标题")),
	}
	f.liveStatus = map[string]string{
		"1": `{"code":0,"data":{"1":{"title":"正在播","room_id":7,"uid":1,"online":12,"live_status":1,"live_time":1600000000,"uname":"直播 UP","face":"https://i0.hdslb.com/bfs/face/1.png"}}}`,
	}
	service := newTestService(t, Settings{Enabled: true, UIDs: []int64{1, 2}, RefreshMinutes: 5, DynamicCount: 1})
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
	if items[1].Kind != "视频" || items[1].Summary != "新视频" || items[1].CreatorName != "视频 UP" {
		t.Fatalf("item = %#v", items[1])
	}
}

// The feed request has to be signed and carry the identity cookie; without
// either, Bilibili answers -412.
func TestRefreshSignsTheFeedRequest(t *testing.T) {
	f := newFeedServer(t)
	f.feeds = map[string]string{"1": feedJSON(itemJSON("1", "DYNAMIC_TYPE_WORD", 1, "UP", "", 1700000000, "动态", "", ""))}
	service := newTestService(t, Settings{Enabled: true, UIDs: []int64{1}})
	if err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("a signed request with the identity cookie = %v, want no error", err)
	}
	f.mu.Lock()
	paths := append([]string(nil), f.requests...)
	f.mu.Unlock()
	var sawSite, sawNav, sawFeed bool
	for _, p := range paths {
		switch p {
		case "/":
			sawSite = true
		case "/x/web-interface/nav":
			sawNav = true
		case dynamicFeedPath:
			sawFeed = true
		}
	}
	if !sawSite || !sawNav || !sawFeed {
		t.Fatalf("requests = %v, want the site, the nav keys and the feed", paths)
	}
}

// A creator's avatar is available to the presentation after a refresh.
func TestRefreshFetchesCreatorAvatar(t *testing.T) {
	f := newFeedServer(t)
	// The avatar URL is relative to the host, which is what the endpoint
	// returns; serveImage turns it into the test server's own address.
	f.feeds = map[string]string{"1": feedJSON(itemJSON("1", "DYNAMIC_TYPE_WORD", 1, "UP", "/bfs/face/1.png", 1700000000, "动态", "", ""))}
	service := newTestService(t, Settings{Enabled: true, UIDs: []int64{1}})
	if err := service.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := service.Avatar(1); len(got) == 0 {
		t.Fatal("no avatar was kept for the creator")
	}
}

// A creator with no avatar URL is not a failed refresh.
func TestRefreshWithoutAvatarIsFine(t *testing.T) {
	f := newFeedServer(t)
	f.feeds = map[string]string{"1": feedJSON(itemJSON("1", "DYNAMIC_TYPE_WORD", 1, "UP", "", 1700000000, "动态", "", ""))}
	service := newTestService(t, Settings{Enabled: true, UIDs: []int64{1}})
	if err := service.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := service.Avatar(1); got != nil {
		t.Fatalf("avatar = %v, want none", got)
	}
}

// Bilibili answers a throttled request with code 0 and an empty list, so the
// feed retries before it believes an empty answer.
func TestRefreshRetriesAnEmptyFeed(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	// The first feed answer is empty and the second carries an item, which is
	// what a throttled request followed by a good one looks like.
	recovering := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Add("Set-Cookie", "buvid3=test; Path=/")
			_, _ = w.Write([]byte("<html></html>"))
		case "/x/web-interface/nav":
			_, _ = w.Write([]byte(navKeys))
		case dynamicFeedPath:
			mu.Lock()
			calls++
			n := calls
			mu.Unlock()
			if n == 1 {
				_, _ = w.Write([]byte(`{"code":0,"message":"0","data":{"items":[]}}`))
				return
			}
			_, _ = w.Write([]byte(feedJSON(itemJSON("1", "DYNAMIC_TYPE_WORD", 1, "UP", "", 1700000000, "动态", "", ""))))
		default:
			_, _ = w.Write([]byte(`{"code":0,"data":{"1":{"live_status":0,"uid":1}}}`))
		}
	}))
	defer recovering.Close()
	oldAPI, oldLive, oldWeb, oldNav := apiBaseURL, liveBaseURL, webBaseURL, navURL
	apiBaseURL, liveBaseURL = recovering.URL, recovering.URL
	webBaseURL = recovering.URL + "/"
	navURL = recovering.URL + "/x/web-interface/nav"
	defer func() { apiBaseURL, liveBaseURL, webBaseURL, navURL = oldAPI, oldLive, oldWeb, oldNav }()

	service := newTestService(t, Settings{Enabled: true, UIDs: []int64{1}})
	if err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh = %v, want no error", err)
	}
	items, _, _, _ := service.View()
	if len(items) != 1 {
		t.Fatalf("items = %#v; the retry did not recover the feed", items)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 2 {
		t.Fatalf("the feed was asked %d times, want 2", calls)
	}
}

// An empty feed after a real post must not replace that post. Bilibili uses
// the same empty list for throttling, and believing it wipes the last row.
func TestRefreshKeepsThePreviousPostWhenTheFeedIsEmpty(t *testing.T) {
	f := newFeedServer(t)
	f.feeds = map[string]string{"1": feedJSON(itemJSON("1", "DYNAMIC_TYPE_AV", 1, "UP", "", 1700000000, "", "BV1", "新视频"))}
	path := filepath.Join(t.TempDir(), "bilibili_followed.json")
	section := NewSection()
	section.Set(Settings{Enabled: true, UIDs: []int64{1}, DynamicCount: 1})
	service, err := NewWithCache(httpc.New(httpc.Options{}), section, nil, NewCache(path))
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return time.Unix(1800000000, 0) }
	service.session.now = service.now
	if err := service.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.feeds["1"] = feedJSON("")
	if err := service.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	items, _, _, err := service.View()
	if err != nil || len(items) != 1 || items[0].Summary != "新视频" || items[0].Live {
		t.Fatalf("items = %#v, err = %v", items, err)
	}
	stored, _, ok := NewCache(path).Load([]int64{1})
	if !ok || len(stored) != 1 || stored[0].Summary != "新视频" {
		t.Fatalf("record = %#v, ok = %v", stored, ok)
	}
}

// A creator that really has nothing is not an error once the retries are spent.
func TestRefreshAcceptsAnEmptyFeedAfterRetries(t *testing.T) {
	f := newFeedServer(t)
	f.feeds = map[string]string{"1": feedJSON("")}
	service := newTestService(t, Settings{Enabled: true, UIDs: []int64{1}})
	if err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("an empty feed = %v, want no error", err)
	}
	items, _, _, err := service.View()
	if err != nil || len(items) != 0 {
		t.Fatalf("items = %#v, err = %v", items, err)
	}
	if f.count() != feedAttempts {
		t.Fatalf("the feed was asked %d times, want %d", f.count(), feedAttempts)
	}
}

// Without WBI keys the query cannot be signed, and that is reported rather
// than silently returning nothing.
func TestRefreshReportsMissingKeys(t *testing.T) {
	f := newFeedServer(t)
	f.navBody = `{"code":-101,"data":{}}`
	service := newTestService(t, Settings{Enabled: true, UIDs: []int64{1}})
	err := service.Refresh(context.Background())
	if !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("error = %v, want ErrProviderBadResponse", err)
	}
	if !strings.Contains(err.Error(), "WBI") {
		t.Fatalf("error = %v, want it to name the missing keys", err)
	}
}

// An HTTP block carries its status to the card: 412 is a block, not a shape
// change.
func TestRefreshRefusesAnHTTPBlock(t *testing.T) {
	f := newFeedServer(t)
	f.feedStatus = http.StatusPreconditionFailed
	service := newTestService(t, Settings{Enabled: true, UIDs: []int64{1}})
	err := service.Refresh(context.Background())
	if !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("error = %v, want ErrProviderBadResponse", err)
	}
	if !strings.Contains(err.Error(), "412") {
		t.Fatalf("error = %v, want it to carry the status", err)
	}
}

// Streaming wins over any post, and the live status is asked for whether or
// not the creator has posted. A creator who streams and posts must not be
// shown as a post: being live is what is happening now.
func TestRefreshShowsLiveEvenWhenTheCreatorHasPosts(t *testing.T) {
	f := newFeedServer(t)
	f.feeds = map[string]string{"1": feedJSON(itemJSON("1", "DYNAMIC_TYPE_AV", 1, "UP", "", 1700000000, "", "BV1", "刚发的视频"))}
	f.liveStatus = map[string]string{
		"1": `{"code":0,"data":{"1":{"title":"正在直播","room_id":7,"uid":1,"online":12,"live_status":1,"live_time":1699999000,"uname":"UP","face":""}}}`,
	}
	service := newTestService(t, Settings{Enabled: true, UIDs: []int64{1}, DynamicCount: 1})
	if err := service.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	items, _, _, _ := service.View()
	if len(items) != 1 {
		t.Fatalf("items = %#v, want one row", items)
	}
	if !items[0].Live {
		t.Fatalf("row = %#v, want the live session rather than the post", items[0])
	}
	if items[0].Summary != "正在直播" || items[0].URL != "https://live.bilibili.com/7" {
		t.Fatalf("row = %#v", items[0])
	}
}

// The live question is asked on every refresh, not only when the timeline came
// back empty.
func TestRefreshAlwaysAsksTheLiveStatus(t *testing.T) {
	f := newFeedServer(t)
	f.feeds = map[string]string{"1": feedJSON(itemJSON("1", "DYNAMIC_TYPE_AV", 1, "UP", "", 1700000000, "", "BV1", "视频"))}
	service := newTestService(t, Settings{Enabled: true, UIDs: []int64{1}, DynamicCount: 1})
	if err := service.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, path := range f.requests {
		if path == "/room/v1/Room/get_status_info_by_uids" {
			return
		}
	}
	t.Fatalf("requests = %v, want the live status asked even though posts exist", f.requests)
}

// A creator with nothing on their timeline can still be streaming, and that is
// the row: an empty feed must not hide a live session.
func TestRefreshShowsALiveCreatorWithNoPosts(t *testing.T) {
	f := newFeedServer(t)
	f.feeds = map[string]string{"1": feedJSON("")}
	f.liveStatus = map[string]string{
		"1": `{"code":0,"data":{"1":{"title":"市场又发生什么了？","room_id":4629447,"uid":1,"online":205770,"live_status":1,"live_time":1792567080,"uname":"某 UP","face":"https://i0.hdslb.com/bfs/face/1.png"}}}`,
	}
	service := newTestService(t, Settings{Enabled: true, UIDs: []int64{1}})
	if err := service.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	items, _, _, _ := service.View()
	if len(items) != 1 {
		t.Fatalf("items = %#v, want the live row", items)
	}
	row := items[0]
	if !row.Live || row.CreatorName != "某 UP" || row.Summary != "市场又发生什么了？" {
		t.Fatalf("row = %#v", row)
	}
	if row.URL != "https://live.bilibili.com/4629447" || row.LiveAudience != "205770" {
		t.Fatalf("row = %#v", row)
	}
	if row.PublishedAt.Unix() != 1792567080 {
		t.Errorf("publishedAt = %v, want the live start time", row.PublishedAt)
	}
}

// Not streaming and nothing posted: the creator simply has no row.
func TestRefreshWithNoPostsAndNoLiveShowsNothing(t *testing.T) {
	f := newFeedServer(t)
	f.feeds = map[string]string{"1": feedJSON("")}
	f.liveStatus = map[string]string{"1": `{"code":0,"data":{"1":{"live_status":0,"uid":1}}}`}
	service := newTestService(t, Settings{Enabled: true, UIDs: []int64{1}})
	if err := service.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	items, _, _, _ := service.View()
	if len(items) != 0 {
		t.Fatalf("items = %#v, want none", items)
	}
}

// A pinned post is the old one the creator wants noticed, and it arrives first
// in the feed. It must not be taken for the newest, and it must not use up one
// of the requested slots.
func TestPinnedPostsAreSkipped(t *testing.T) {
	pinned := `{"id_str":"pin","type":"DYNAMIC_TYPE_DRAW","modules":{"module_author":{"mid":1,"name":"UP","pub_ts":1600000000},"module_tag":{"text":"置顶"},"module_dynamic":{"major":{"opus":{"jump_url":"//www.bilibili.com/opus/1","summary":{"text":"很早的置顶动态"}}}}}}`
	fresh := itemJSON("fresh", "DYNAMIC_TYPE_AV", 1, "UP", "", 1700000000, "", "BV1", "新视频")
	payload := feedJSON(pinned + "," + fresh)

	items, err := decodeDynamics(1, []byte(payload), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %#v, want one row", items)
	}
	if items[0].Summary == "很早的置顶动态" {
		t.Fatal("the pinned post was chosen")
	}
	if items[0].Summary != "新视频" {
		t.Errorf("row = %#v, want the fresh one", items[0])
	}
}

// A timeline holding nothing but a pinned post has nothing new to show.
func TestOnlyPinnedPostsMeansNoRow(t *testing.T) {
	pinned := `{"id_str":"pin","type":"DYNAMIC_TYPE_DRAW","modules":{"module_author":{"mid":1,"name":"UP","pub_ts":1600000000},"module_tag":{"text":"置顶"},"module_dynamic":{"major":{"opus":{"summary":{"text":"置顶"}}}}}}`
	items, err := decodeDynamics(1, []byte(feedJSON(pinned)), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("items = %#v, want none", items)
	}
}

func TestDecodeDynamicsReturnsConfiguredNewestItems(t *testing.T) {
	payload := []byte(feedJSON(itemJSON("new", "DYNAMIC_TYPE_WORD", 1, "UP", "https://example.test/1", 1700000100, "最新", "", "") + "," +
		itemJSON("old", "DYNAMIC_TYPE_AV", 1, "UP", "https://example.test/1", 1700000000, "", "BV1", "旧视频")))
	items, err := decodeDynamics(1, payload, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Summary != "最新" || items[1].Kind != "视频" || items[1].URL != "https://www.bilibili.com/video/BV1" {
		t.Fatalf("items = %#v", items)
	}
	if items[1].Summary != "旧视频" {
		t.Fatalf("a video with no description falls back to its title, got %q", items[1].Summary)
	}
}

func TestDecodeDynamicsFallsBackToAPlaceholder(t *testing.T) {
	items, err := decodeDynamics(1, []byte(feedJSON(itemJSON("1", "DYNAMIC_TYPE_DRAW", 1, "UP", "", 1700000000, "", "", ""))), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Summary != "发布了新动态" || items[0].URL != "https://t.bilibili.com/1" {
		t.Fatalf("items = %#v", items)
	}
}

func TestDecodeDynamicsRejectsAnotherAuthor(t *testing.T) {
	_, err := decodeDynamics(1, []byte(feedJSON(itemJSON("1", "DYNAMIC_TYPE_WORD", 2, "别人", "", 1700000000, "动态", "", ""))), 1)
	if !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("error = %v, want ErrProviderBadResponse", err)
	}
}

func TestFeedRejectionNamesTheBlock(t *testing.T) {
	cases := []struct {
		code int
		want string
	}{
		{-412, "blocked"},
		{riskControlCode, "signature"},
		{-509, "-509"},
	}
	for _, c := range cases {
		if got := feedRejection(c.code, ""); !strings.Contains(got, c.want) {
			t.Errorf("feedRejection(%d) = %q, want it to mention %q", c.code, got, c.want)
		}
	}
}

func TestAvatarURL(t *testing.T) {
	old := apiBaseURL
	apiBaseURL = "https://api.example.test"
	defer func() { apiBaseURL = old }()
	cases := []struct{ in, want string }{
		{"http://i0.hdslb.com/a.png", "https://i0.hdslb.com/a.png"},
		{"//i0.hdslb.com/a.png", "https://i0.hdslb.com/a.png"},
		{"/bfs/face/1.png", "https://api.example.test/bfs/face/1.png"},
		{"https://i0.hdslb.com/a.png", "https://i0.hdslb.com/a.png"},
		{"", ""},
	}
	for _, c := range cases {
		if got := avatarURL(c.in); got != c.want {
			t.Errorf("avatarURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Bilibili sends pub_ts as a number for most items and as a string for some.
// One string-typed item must not make the whole feed undecodable.
func TestDecodeAcceptsAStringTimestamp(t *testing.T) {
	payload := `{"code":0,"message":"0","data":{"items":[{"id_str":"1","type":"DYNAMIC_TYPE_WORD","modules":{"module_author":{"mid":"1","name":"UP","face":"","pub_ts":"1700000000"},"module_dynamic":{"desc":{"text":"动态"}}}}]}}`
	items, err := decodeDynamics(1, []byte(payload), 1)
	if err != nil {
		t.Fatalf("a string pub_ts = %v, want it to decode", err)
	}
	if len(items) != 1 || items[0].PublishedAt.Unix() != 1700000000 {
		t.Fatalf("items = %#v", items)
	}
}

// A timestamp that is not a number at all is not a usable item; the author
// check rejects it rather than showing a row dated 1970.
func TestDecodeRejectsAnUnreadableTimestamp(t *testing.T) {
	payload := `{"code":0,"message":"0","data":{"items":[{"id_str":"1","type":"DYNAMIC_TYPE_WORD","modules":{"module_author":{"mid":1,"name":"UP","face":"","pub_ts":"yesterday"},"module_dynamic":{"desc":{"text":"动态"}}}}]}}`
	_, err := decodeDynamics(1, []byte(payload), 1)
	if !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("error = %v, want ErrProviderBadResponse", err)
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
