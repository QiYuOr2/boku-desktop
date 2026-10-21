// Package bilibili watches explicitly configured public creator profiles.
package bilibili

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"boku/internal/contract/bilibili"
	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

var apiBaseURL = bilibili.APIBaseURL
var liveBaseURL = bilibili.LiveBaseURL

type Service struct {
	client    *httpc.Client
	section   *Section
	store     StoreSettings
	now       func() time.Time
	mu        sync.RWMutex
	items     []domain.FollowedUpdate
	fetchedAt time.Time
	loading   bool
	err       error
	stop      context.CancelFunc
	emit      func(previous, current []domain.FollowedUpdate)
}

func New(client *httpc.Client, section *Section, store StoreSettings) (*Service, error) {
	if client == nil || section == nil {
		return nil, fmt.Errorf("%w: bilibili needs client and settings", domain.ErrInvalidArgument)
	}
	return &Service{client: client, section: section, store: store, now: time.Now}, nil
}
func (s *Service) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.section.Settings()
}

// SetEmit installs the observer called after a successful refresh. It is used
// by the presentation to decide whether a new live session or activity earns
// a notification; the service never imports a window or notification API.
func (s *Service) SetEmit(emit func(previous, current []domain.FollowedUpdate)) {
	s.mu.Lock()
	s.emit = emit
	s.mu.Unlock()
}
func (s *Service) Enabled() bool       { return s.Settings().Enabled }
func (s *Service) UIDs() []int64       { return append([]int64(nil), s.Settings().UIDs...) }
func (s *Service) RefreshMinutes() int { return s.Settings().RefreshMinutes }
func (s *Service) DynamicCount() int   { return s.Settings().DynamicCount }
func (s *Service) Notifications() (bool, bool) {
	v := s.Settings()
	return v.LiveNotifications, v.DynamicNotifications
}
func (s *Service) Configure(ctx context.Context, enabled bool, ids []int64, minutes, dynamics int, liveNotice, dynamicNotice bool) error {
	return s.ConfigureSettings(ctx, Settings{Enabled: enabled, UIDs: ids, RefreshMinutes: minutes, DynamicCount: dynamics, LiveNotifications: liveNotice, DynamicNotifications: dynamicNotice})
}
func (s *Service) ConfigureSettings(ctx context.Context, settings Settings) error {
	if s.store != nil {
		return s.store.Update(ctx, sectionName, func() error {
			s.mu.Lock()
			s.section.Set(settings)
			s.mu.Unlock()
			return nil
		})
	}
	s.mu.Lock()
	s.section.Set(settings)
	s.mu.Unlock()
	return nil
}
func (s *Service) View() ([]domain.FollowedUpdate, time.Time, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]domain.FollowedUpdate(nil), s.items...), s.fetchedAt, s.loading, s.err
}
func (s *Service) NeedsRefresh() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v := s.section.Settings()
	return v.Enabled && len(v.UIDs) > 0 && !s.loading && (s.fetchedAt.IsZero() || s.now().Sub(s.fetchedAt) >= time.Duration(v.RefreshMinutes)*time.Minute || s.err != nil)
}
func (s *Service) RefreshIfNeeded(ctx context.Context) error {
	if !s.NeedsRefresh() {
		return nil
	}
	return s.Refresh(ctx)
}

// Start keeps the configured public profiles fresh while the tray app runs.
// A minute tick is intentionally the smallest supported refresh interval; the
// current setting is checked on every tick, so saving a new interval needs no
// second scheduler or process restart.
func (s *Service) Start(ctx context.Context) {
	s.mu.Lock()
	if s.stop != nil {
		s.mu.Unlock()
		return
	}
	ctx, s.stop = context.WithCancel(ctx)
	s.mu.Unlock()
	go func() {
		_ = s.RefreshIfNeeded(ctx)
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				_ = s.RefreshIfNeeded(ctx)
			}
		}
	}()
}

// Stop ends the monitor's ticker before the application exits.
func (s *Service) Stop() {
	s.mu.Lock()
	stop := s.stop
	s.stop = nil
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
}
func (s *Service) Refresh(ctx context.Context) error {
	v := s.Settings()
	if !v.Enabled || len(v.UIDs) == 0 {
		return nil
	}
	s.mu.Lock()
	if s.loading {
		s.mu.Unlock()
		return nil
	}
	s.loading = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.loading = false; s.mu.Unlock() }()
	items := make([]domain.FollowedUpdate, 0, len(v.UIDs)*v.DynamicCount)
	for _, uid := range v.UIDs {
		creatorItems, err := s.fetchCreator(ctx, uid, v.DynamicCount)
		if err != nil {
			s.record(nil, err)
			return err
		}
		items = append(items, creatorItems...)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Live != items[j].Live {
			return items[i].Live
		}
		return items[i].PublishedAt.After(items[j].PublishedAt)
	})
	s.record(items, nil)
	return nil
}
func (s *Service) fetchCreator(ctx context.Context, uid int64, dynamicCount int) ([]domain.FollowedUpdate, error) {
	dynamic, err := s.client.GetJSON(ctx, fmt.Sprintf("%s/x/polymer/web-dynamic/v1/feed/space?host_mid=%d", apiBaseURL, uid), nil)
	if err != nil {
		return nil, err
	}
	items, err := decodeDynamics(uid, dynamic.Payload, dynamicCount)
	if err != nil {
		return nil, err
	}
	live, err := s.client.GetJSON(ctx, fmt.Sprintf("%s/room/v1/Room/getRoomInfoOld?mid=%d", liveBaseURL, uid), nil)
	if err != nil {
		return nil, err
	}
	if err := decodeLive(&items[0], live.Payload); err != nil {
		return nil, err
	}
	return items, nil
}
func (s *Service) record(items []domain.FollowedUpdate, err error) {
	s.mu.Lock()
	previous, emit := append([]domain.FollowedUpdate(nil), s.items...), s.emit
	if err == nil {
		s.items = append([]domain.FollowedUpdate(nil), items...)
		s.fetchedAt = s.now().UTC()
	}
	s.err = err
	s.mu.Unlock()
	if err == nil && emit != nil {
		emit(previous, append([]domain.FollowedUpdate(nil), items...))
	}
}

type dynamicPage struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Items []struct {
			ID      string `json:"id_str"`
			Type    string `json:"type"`
			Modules struct {
				Author struct {
					MID   int64  `json:"mid"`
					Name  string `json:"name"`
					Face  string `json:"face"`
					PubTS int64  `json:"pub_ts"`
				} `json:"module_author"`
				Dynamic struct {
					Desc struct {
						Text string `json:"text"`
					} `json:"desc"`
					Major struct {
						Archive struct {
							JumpURL string `json:"jump_url"`
							Title   string `json:"title"`
						} `json:"archive"`
					} `json:"major"`
				} `json:"module_dynamic"`
			} `json:"modules"`
		} `json:"items"`
	} `json:"data"`
}

func decodeDynamic(uid int64, payload []byte) (domain.FollowedUpdate, error) {
	items, err := decodeDynamics(uid, payload, 1)
	if err != nil {
		return domain.FollowedUpdate{}, err
	}
	return items[0], nil
}

func decodeDynamics(uid int64, payload []byte, count int) ([]domain.FollowedUpdate, error) {
	var page dynamicPage
	if err := json.Unmarshal(payload, &page); err != nil {
		return nil, bad("dynamic is not an object", payload)
	}
	if page.Code != 0 {
		return nil, bad("dynamic API rejected request", payload)
	}
	if len(page.Data.Items) == 0 {
		return nil, bad("dynamic list is empty", payload)
	}
	if count < 1 {
		count = 1
	}
	if count > len(page.Data.Items) {
		count = len(page.Data.Items)
	}
	items := make([]domain.FollowedUpdate, 0, count)
	for _, v := range page.Data.Items[:count] {
		a := v.Modules.Author
		if a.MID != uid || strings.TrimSpace(a.Name) == "" || a.PubTS <= 0 || strings.TrimSpace(v.ID) == "" {
			return nil, bad("dynamic has no valid author", payload)
		}
		summary := strings.TrimSpace(v.Modules.Dynamic.Desc.Text)
		if summary == "" {
			summary = strings.TrimSpace(v.Modules.Dynamic.Major.Archive.Title)
		}
		if summary == "" {
			summary = "发布了新动态"
		}
		url := "https://t.bilibili.com/" + v.ID
		if jump := strings.TrimSpace(v.Modules.Dynamic.Major.Archive.JumpURL); jump != "" {
			if strings.HasPrefix(jump, "//") {
				jump = "https:" + jump
			}
			url = jump
		}
		kind := "动态"
		if strings.Contains(v.Type, "AV") {
			kind = "视频"
		}
		items = append(items, domain.FollowedUpdate{CreatorID: uid, CreatorName: strings.TrimSpace(a.Name), AvatarURL: strings.TrimSpace(a.Face), Kind: kind, Summary: summary, URL: url, PublishedAt: time.Unix(a.PubTS, 0).UTC()})
	}
	return items, nil
}

type liveResponse struct {
	Code int `json:"code"`
	Data struct {
		RoomID     int64  `json:"room_id"`
		Title      string `json:"title"`
		LiveStatus int    `json:"live_status"`
		Online     int64  `json:"online"`
	} `json:"data"`
}

func decodeLive(item *domain.FollowedUpdate, payload []byte) error {
	var v liveResponse
	if err := json.Unmarshal(payload, &v); err != nil {
		return bad("live response is not an object", payload)
	}
	if v.Code != 0 {
		return bad("live API rejected request", payload)
	}
	if v.Data.LiveStatus == 1 {
		if v.Data.RoomID <= 0 {
			return bad("live room id is invalid", payload)
		}
		item.Live = true
		item.LiveRoomID = v.Data.RoomID
		item.LiveTitle = strings.TrimSpace(v.Data.Title)
		item.LiveAudience = fmt.Sprint(v.Data.Online)
		item.URL = fmt.Sprintf("https://live.bilibili.com/%d", v.Data.RoomID)
		item.Kind = "直播中"
		if item.LiveTitle != "" {
			item.Summary = item.LiveTitle
		}
	}
	return nil
}
func bad(reason string, payload []byte) error {
	return &domain.BadResponseError{ProviderID: bilibili.ProviderID, Reason: reason, Snippet: domain.Snippet(payload)}
}
