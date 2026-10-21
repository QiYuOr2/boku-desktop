package bilibili

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

func sampleRow(uid int64, live bool, at time.Time) domain.FollowedUpdate {
	return domain.FollowedUpdate{
		CreatorID:    uid,
		CreatorName:  "UP",
		AvatarURL:    "https://i0.hdslb.com/bfs/face/1.png",
		Kind:         "视频",
		Summary:      "一条动态",
		URL:          "https://www.bilibili.com/video/BV1",
		PublishedAt:  at,
		Live:         live,
		LiveRoomID:   7,
		LiveTitle:    "标题",
		LiveAudience: "12",
	}
}

// What was written is what is read back, field for field.
func TestCacheRoundTripsTheLastRow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bilibili_followed.json")
	cache := NewCache(path)
	at := time.Unix(1700000000, 0).UTC()
	saved := []domain.FollowedUpdate{sampleRow(1, true, at)}

	if err := cache.Save(saved, at); err != nil {
		t.Fatal(err)
	}
	got, fetchedAt, ok := cache.Load([]int64{1})
	if !ok {
		t.Fatal("the record was not read back")
	}
	if len(got) != 1 {
		t.Fatalf("rows = %#v", got)
	}
	if got[0] != saved[0] {
		t.Errorf("row = %#v, want %#v", got[0], saved[0])
	}
	if !fetchedAt.Equal(at) {
		t.Errorf("fetchedAt = %v, want %v", fetchedAt, at)
	}
}

// A row for a creator who is no longer configured is not returned, even when
// the file still holds it.
func TestCacheSkipsCreatorsThatAreNotFollowed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bilibili_followed.json")
	cache := NewCache(path)
	at := time.Unix(1700000000, 0).UTC()
	if err := cache.Save([]domain.FollowedUpdate{sampleRow(1, false, at), sampleRow(2, false, at)}, at); err != nil {
		t.Fatal(err)
	}
	got, _, _ := cache.Load([]int64{2})
	if len(got) != 1 || got[0].CreatorID != 2 {
		t.Fatalf("rows = %#v, want only creator 2", got)
	}
}

// Removing a creator drops their row from the file and leaves the rest alone.
func TestCacheForgetDropsRemovedCreators(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bilibili_followed.json")
	cache := NewCache(path)
	at := time.Unix(1700000000, 0).UTC()
	if err := cache.Save([]domain.FollowedUpdate{sampleRow(1, false, at), sampleRow(2, false, at)}, at); err != nil {
		t.Fatal(err)
	}
	if err := cache.Forget([]int64{2}); err != nil {
		t.Fatal(err)
	}
	got, _, ok := cache.Load([]int64{1, 2})
	if !ok {
		t.Fatal("the record disappeared")
	}
	if len(got) != 1 || got[0].CreatorID != 2 {
		t.Fatalf("rows = %#v, want only creator 2", got)
	}
}

// Removing the last followed creator removes the file: an empty record is not
// worth keeping, and nothing should be left behind.
func TestCacheForgetRemovesTheFileWhenNothingIsLeft(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bilibili_followed.json")
	cache := NewCache(path)
	at := time.Unix(1700000000, 0).UTC()
	if err := cache.Save([]domain.FollowedUpdate{sampleRow(1, false, at)}, at); err != nil {
		t.Fatal(err)
	}
	if err := cache.Forget(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the record still exists: %v", err)
	}
}

// A missing or unreadable file is a cache miss, never an error: the next
// refresh replaces it.
func TestCacheMissIsNotAnError(t *testing.T) {
	missing := NewCache(filepath.Join(t.TempDir(), "absent.json"))
	if rows, _, ok := missing.Load([]int64{1}); ok || len(rows) != 0 {
		t.Errorf("a missing file answered with rows")
	}
	if err := missing.Forget([]int64{1}); err != nil {
		t.Errorf("forgetting an absent file = %v", err)
	}

	broken := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(broken, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	cache := NewCache(broken)
	if rows, _, ok := cache.Load([]int64{1}); ok || len(rows) != 0 {
		t.Errorf("a broken file answered with rows")
	}
	// Nothing readable means nothing worth preserving: the file is replaced.
	if err := cache.Forget(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(broken); !os.IsNotExist(err) {
		t.Errorf("the unreadable record was kept")
	}
}

// A file written by an older shape is not trusted.
func TestCacheRejectsAnUnknownVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bilibili_followed.json")
	body := `{"version":99,"fetchedAt":"2024-01-01T00:00:00Z","creators":[{"creatorId":1,"creatorName":"UP","summary":"s"}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if rows, _, ok := NewCache(path).Load([]int64{1}); ok {
		t.Errorf("an unknown version was accepted: %#v", rows)
	}
}

// A row without the fields that make it a row is dropped rather than shown.
func TestCacheSkipsIncompleteRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bilibili_followed.json")
	body := `{"version":1,"fetchedAt":"2024-01-01T00:00:00Z","creators":[
		{"creatorId":1,"creatorName":"","summary":"s"},
		{"creatorId":2,"creatorName":"UP","summary":""},
		{"creatorId":0,"creatorName":"UP","summary":"s"},
		{"creatorId":3,"creatorName":"UP","summary":"s"}
	]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, _, ok := NewCache(path).Load([]int64{1, 2, 3})
	if !ok {
		t.Fatal("the record was rejected")
	}
	if len(rows) != 1 || rows[0].CreatorID != 3 {
		t.Fatalf("rows = %#v, want only creator 3", rows)
	}
}

// A cold start shows the creators from the record, before any network answer.
func TestServiceRestoresFromTheRecord(t *testing.T) {
	f := newFeedServer(t)
	_ = f
	path := filepath.Join(t.TempDir(), "bilibili_followed.json")
	at := time.Unix(1700000000, 0).UTC()
	if err := NewCache(path).Save([]domain.FollowedUpdate{sampleRow(1, true, at)}, at); err != nil {
		t.Fatal(err)
	}
	section := NewSection()
	section.Set(Settings{Enabled: true, UIDs: []int64{1}})
	service, err := NewWithCache(httpc.New(httpc.Options{}), section, nil, NewCache(path))
	if err != nil {
		t.Fatal(err)
	}
	rows, fetchedAt, _, _ := service.View()
	if len(rows) != 1 || !rows[0].Live || rows[0].CreatorID != 1 {
		t.Fatalf("rows = %#v, want the restored row", rows)
	}
	if !fetchedAt.Equal(at) {
		t.Errorf("fetchedAt = %v, want %v", fetchedAt, at)
	}
}

// A refresh writes the record, so the next start has it.
func TestRefreshWritesTheRecord(t *testing.T) {
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
	rows, _, ok := NewCache(path).Load([]int64{1})
	if !ok || len(rows) != 1 {
		t.Fatalf("the record was not written: %#v", rows)
	}
	if rows[0].Summary != "新视频" {
		t.Errorf("row = %#v", rows[0])
	}
}

// Removing a creator from the settings removes their row from the screen and
// from the record on disk.
func TestRemovingACreatorClearsTheirRecord(t *testing.T) {
	newFeedServer(t)
	path := filepath.Join(t.TempDir(), "bilibili_followed.json")
	at := time.Unix(1700000000, 0).UTC()
	if err := NewCache(path).Save([]domain.FollowedUpdate{sampleRow(1, false, at), sampleRow(2, false, at)}, at); err != nil {
		t.Fatal(err)
	}
	section := NewSection()
	section.Set(Settings{Enabled: true, UIDs: []int64{1, 2}})
	service, err := NewWithCache(httpc.New(httpc.Options{}), section, nil, NewCache(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Configure(context.Background(), true, []int64{2}, 5, 1, false, false); err != nil {
		t.Fatal(err)
	}
	rows, _, _, _ := service.View()
	if len(rows) != 1 || rows[0].CreatorID != 2 {
		t.Fatalf("rows = %#v, want only creator 2", rows)
	}
	stored, _, _ := NewCache(path).Load([]int64{1, 2})
	if len(stored) != 1 || stored[0].CreatorID != 2 {
		t.Fatalf("the record still holds the removed creator: %#v", stored)
	}
}

// Removing every creator leaves no file behind.
func TestRemovingTheLastCreatorDeletesTheRecord(t *testing.T) {
	newFeedServer(t)
	path := filepath.Join(t.TempDir(), "bilibili_followed.json")
	at := time.Unix(1700000000, 0).UTC()
	if err := NewCache(path).Save([]domain.FollowedUpdate{sampleRow(1, false, at)}, at); err != nil {
		t.Fatal(err)
	}
	section := NewSection()
	section.Set(Settings{Enabled: true, UIDs: []int64{1}})
	service, err := NewWithCache(httpc.New(httpc.Options{}), section, nil, NewCache(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Configure(context.Background(), true, nil, 5, 1, false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the record still exists: %v", err)
	}
}

// A cache miss at start is not a failure: the service runs without one.
func TestNewWithCacheToleratesAMissingRecord(t *testing.T) {
	newFeedServer(t)
	path := filepath.Join(t.TempDir(), "absent.json")
	section := NewSection()
	section.Set(Settings{Enabled: true, UIDs: []int64{1}})
	service, err := NewWithCache(httpc.New(httpc.Options{}), section, nil, NewCache(path))
	if err != nil {
		t.Fatalf("NewWithCache = %v", err)
	}
	if rows, _, _, _ := service.View(); len(rows) != 0 {
		t.Fatalf("rows = %#v, want none", rows)
	}
}

// One row per creator is written, even when a refresh produced two.
func TestCacheSaveKeepsOneRowPerCreator(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bilibili_followed.json")
	cache := NewCache(path)
	at := time.Unix(1700000000, 0).UTC()
	first := sampleRow(1, false, at)
	second := sampleRow(1, true, at)
	if err := cache.Save([]domain.FollowedUpdate{first, second}, at); err != nil {
		t.Fatal(err)
	}
	rows, _, _ := cache.Load([]int64{1})
	if len(rows) != 1 {
		t.Fatalf("rows = %#v, want one per creator", rows)
	}
	if !rows[0].Live {
		t.Errorf("row = %#v, want the later one", rows[0])
	}
}
