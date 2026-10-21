package app

import (
	"context"
	"testing"
	"time"

	"boku/internal/domain"
)

type testBilibili struct {
	live, dynamic bool
	// noUIDs answers with an empty followed list; otherwise one creator.
	noUIDs bool
}

func (b testBilibili) Enabled() bool { return true }
func (b testBilibili) UIDs() []int64 {
	if b.noUIDs {
		return nil
	}
	return []int64{1}
}
func (b testBilibili) RefreshMinutes() int         { return 5 }
func (b testBilibili) DynamicCount() int           { return 1 }
func (b testBilibili) Notifications() (bool, bool) { return b.live, b.dynamic }
func (b testBilibili) Configure(context.Context, bool, []int64, int, int, bool, bool) error {
	return nil
}
func (b testBilibili) View() ([]domain.FollowedUpdate, time.Time, bool, error) {
	return nil, time.Time{}, false, nil
}
func (b testBilibili) RefreshIfNeeded(context.Context) error { return nil }
func (b testBilibili) Refresh(context.Context) error         { return nil }
func (b testBilibili) Avatar(int64) []byte                   { return nil }

func TestBilibiliNotificationSkipsInitialSnapshotAndNoticesLiveTransition(t *testing.T) {
	notifier := &testNotifier{supported: true}
	state := &State{bilibiliService: testBilibili{live: true}, notifier: notifier}
	current := domain.FollowedUpdate{CreatorID: 1, CreatorName: "UP", Live: true, LiveRoomID: 9, LiveTitle: "开播"}
	state.NoteBilibiliChanges(nil, []domain.FollowedUpdate{current})
	time.Sleep(20 * time.Millisecond)
	if len(notifier.notices) != 0 {
		t.Fatalf("initial notices=%d", len(notifier.notices))
	}
	state.NoteBilibiliChanges([]domain.FollowedUpdate{{CreatorID: 1, CreatorName: "UP", Live: false}}, []domain.FollowedUpdate{current})
	if err := waitForNotices(notifier, 1); err != nil {
		t.Fatal(err)
	}
	if notifier.notices[0].title != "UP 开播了" {
		t.Fatalf("notice=%#v", notifier.notices[0])
	}
}
