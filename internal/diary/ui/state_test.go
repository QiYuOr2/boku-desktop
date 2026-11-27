package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"boku/internal/diary"
	"boku/internal/domain"
	"boku/internal/platform/httpc"
	"boku/internal/presentation"
	"boku/internal/settings"
)

type stepClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *stepClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

type memSecrets struct {
	mu    sync.Mutex
	items map[string][]byte
}

func newMemSecrets() *memSecrets { return &memSecrets{items: map[string][]byte{}} }

func (m *memSecrets) Unlock(context.Context, string) error { return nil }
func (m *memSecrets) Lock()                                {}
func (m *memSecrets) Unlocked() bool                       { return true }
func (m *memSecrets) Put(_ context.Context, key string, value []byte, _ domain.SecretMeta) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[key] = append([]byte(nil), value...)
	return nil
}
func (m *memSecrets) Get(_ context.Context, key string) (domain.Secret, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.items[key]
	if !ok {
		return domain.Secret{}, domain.ErrSecretNotFound
	}
	return domain.Secret{Name: key, Value: append([]byte(nil), v...)}, nil
}
func (m *memSecrets) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.items, key)
	return nil
}
func (m *memSecrets) List(context.Context) ([]string, error) { return nil, nil }
func (m *memSecrets) Meta(context.Context, string) (domain.SecretMeta, error) {
	return domain.SecretMeta{}, domain.ErrSecretNotFound
}

type noopSecrets struct{}

func (noopSecrets) Unlock(context.Context, string) error                         { return nil }
func (noopSecrets) Lock()                                                        {}
func (noopSecrets) Unlocked() bool                                               { return true }
func (noopSecrets) Put(context.Context, string, []byte, domain.SecretMeta) error { return nil }
func (noopSecrets) Get(context.Context, string) (domain.Secret, error) {
	return domain.Secret{}, domain.ErrSecretNotFound
}
func (noopSecrets) Delete(context.Context, string) error   { return domain.ErrSecretNotFound }
func (noopSecrets) List(context.Context) ([]string, error) { return nil, nil }
func (noopSecrets) Meta(context.Context, string) (domain.SecretMeta, error) {
	return domain.SecretMeta{}, domain.ErrSecretNotFound
}

func newTestState(t *testing.T, now time.Time) *State {
	t.Helper()
	dir := t.TempDir()
	sec := diary.NewSection()
	prefs, err := settings.New(filepath.Join(dir, "settings.json"), sec)
	if err != nil {
		t.Fatalf("settings.New: %v", err)
	}
	svc, err := diary.OpenService(dir, sec, prefs, noopSecrets{}, httpc.New(httpc.Options{}), fixedClock{at: now})
	if err != nil {
		t.Fatalf("OpenService: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	host := presentation.Host{
		RunOnMain: func(fn func()) {
			// 测试里没有主线程事件循环；需要异步回调的用例应显式排水，这里不直接在后台 goroutine 改状态。
			_ = fn
		},
	}
	return New(svc, host, nil, nil)
}

func TestStateCalendarNavigationAndFutureGuard(t *testing.T) {
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.Local)
	s := newTestState(t, now)

	// 不能翻到未来月份。
	s.NextMonth()
	if s.shownMonth != (diary.Month{Year: 2026, Month: 4}) {
		t.Fatalf("NextMonth must not advance past current month: %+v", s.shownMonth)
	}
	s.PrevMonth()
	if s.shownMonth != (diary.Month{Year: 2026, Month: 3}) {
		t.Fatalf("PrevMonth failed: %+v", s.shownMonth)
	}
	s.NextMonth()
	if s.shownMonth != (diary.Month{Year: 2026, Month: 4}) {
		t.Fatalf("NextMonth back to April failed: %+v", s.shownMonth)
	}

	// 不能选中未来日期。
	future := diary.DateKey{Year: 2026, Month: 4, Day: 21}
	s.SelectDay(future)
	if s.filterByDate {
		t.Fatal("SelectDay must reject future date")
	}

	// 点击同一天两次应切换筛选开关。
	past := diary.DateKey{Year: 2026, Month: 4, Day: 18}
	s.SelectDay(past)
	if !s.filterByDate || s.selectedDate != past {
		t.Fatalf("expected filter on 2026-04-18, got filter=%v date=%v", s.filterByDate, s.selectedDate)
	}
	s.SelectDay(past)
	if s.filterByDate {
		t.Fatal("clicking selected day again should clear date filter")
	}
}

func TestStateEditorLifecycleBackfillDeleteAndFlush(t *testing.T) {
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.Local)
	s := newTestState(t, now)

	// 空白新日记点完成不应落盘。
	s.NewEntry(diary.DateKey{Year: 2026, Month: 4, Day: 20})
	s.DoneEditing()
	if s.mode != modeTimeline || len(s.entries) != 0 {
		t.Fatalf("blank new entry should be discarded, mode=%v len=%d", s.mode, len(s.entries))
	}

	// 新建补写过去日期、应用格式、添加标签、关闭前自动落盘。
	past := diary.DateKey{Year: 2026, Month: 4, Day: 15}
	s.NewEntry(past)
	s.ed.title = "补写日记"
	s.ed.markdown = "今天读完了书。"
	s.ApplyFormat(diary.FormatColor("blue"))
	s.ed.tagInput = "读书"
	s.AddTag()

	// 校验不能把归属日期改成未来。
	s.ed.dateInput = "2026-04-25"
	s.ApplyDateInput()
	if s.ed.dateKey != past || s.ed.saveErr == "" {
		t.Fatalf("future date input should be rejected, date=%v err=%q", s.ed.dateKey, s.ed.saveErr)
	}

	s.FlushOnClose()
	s.Reload()
	if len(s.entries) != 1 || s.entries[0].DiaryDateKey != past || s.entries[0].Title != "补写日记" {
		t.Fatalf("FlushOnClose should persist dirty entry, got %+v", s.entries)
	}
	if len(s.entries[0].Tags) != 1 || s.entries[0].Tags[0] != "读书" {
		t.Fatalf("tags not persisted: %+v", s.entries[0].Tags)
	}

	// 删除需要二次确认。
	id := s.entries[0].ID
	s.RequestDelete(id)
	if s.confirmDeleteID != id || len(s.entries) != 1 {
		t.Fatalf("first delete click must only arm confirmation")
	}
	s.RequestDelete(id)
	if len(s.entries) != 0 {
		t.Fatalf("second delete click should soft-delete entry, got len=%d", len(s.entries))
	}
}

func TestStateDiarySettingsValidation(t *testing.T) {
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.Local)
	s := newTestState(t, now)

	s.sync.draftSettings.SyncEndpoint = "not-a-url"
	s.SaveDiarySettings()
	if !s.sync.settingsWarn {
		t.Fatal("invalid SyncEndpoint should trigger warning")
	}

	s.sync.draftSettings = diary.Settings{
		LocationEnabled: true,
		LocationPlace:   "杭州",
		WeatherEnabled:  true,
		WeatherPlace:    "杭州",
		SyncEndpoint:    "https://sync.example.com",
	}
	s.SaveDiarySettings()
	if s.sync.settingsWarn || !s.sync.status.Configured {
		t.Fatalf("valid settings should persist and mark sync configured: warn=%v status=%+v", s.sync.settingsWarn, s.sync.status)
	}
}

func TestStateAutoSyncSkipsEditingAndRespectsCooldown(t *testing.T) {
	var pushCalls atomic.Int32
	var pullCalls atomic.Int32
	var version atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sync/push":
			pushCalls.Add(1)
			var req diary.PushRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			results := make([]diary.PushResult, 0, len(req.Mutations))
			for _, m := range req.Mutations {
				v := version.Add(1)
				cur := "1"
				results = append(results, diary.PushResult{
					MutationID: m.MutationID,
					Status:     diary.StatusApplied,
					Version:    &v,
					Cursor:     &cur,
				})
			}
			_ = json.NewEncoder(w).Encode(diary.PushResponse{Results: results})
		case "/v1/sync/pull":
			pullCalls.Add(1)
			hasMore := false
			cur := "1"
			_ = json.NewEncoder(w).Encode(diary.PullResponse{
				Changes:    []diary.Change{},
				NextCursor: &cur,
				HasMore:    &hasMore,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	sec := diary.NewSection()
	prefs, err := settings.New(filepath.Join(dir, "settings.json"), sec)
	if err != nil {
		t.Fatalf("settings.New: %v", err)
	}
	if err := prefs.Update(context.Background(), "diary", func() error {
		sec.Set(diary.Settings{SyncEndpoint: srv.URL})
		return nil
	}); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	secrets := newMemSecrets()
	_ = secrets.Put(context.Background(), diary.DeviceCredentialKey, []byte("bkd_test_token"), domain.SecretMeta{})
	_ = secrets.Put(context.Background(), diary.DeviceIDKey, []byte("019655c0-0000-7000-8000-000000000010"), domain.SecretMeta{})
	_ = secrets.Put(context.Background(), diary.AccountEmailKey, []byte("qiyu@example.com"), domain.SecretMeta{})

	clk := &stepClock{at: time.Date(2026, 4, 20, 12, 0, 0, 0, time.Local)}
	svc, err := diary.OpenService(dir, sec, prefs, secrets, httpc.New(httpc.Options{}), clk)
	if err != nil {
		t.Fatalf("OpenService: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })

	mainCh := make(chan func(), 16)
	host := presentation.Host{
		RunOnMain: func(fn func()) {
			mainCh <- fn
		},
	}
	s := New(svc, host, nil, nil)
	s.sync.pollInterval = time.Hour // 测试里不靠后台轮询，显式触发路径。

	drainOne := func() {
		t.Helper()
		select {
		case fn := <-mainCh:
			fn()
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for main-thread sync callback")
		}
	}

	// 打开窗口触发首次拉取。
	s.OnWindowOpen()
	drainOne()
	if pullCalls.Load() != 1 || pushCalls.Load() != 0 {
		t.Fatalf("OnWindowOpen expected 1 pull / 0 push, got pull=%d push=%d", pullCalls.Load(), pushCalls.Load())
	}

	// 冷却期内再次聚焦不应触发同步。
	clk.Advance(10 * time.Second)
	s.OnWindowFocus()
	if s.sync.syncing || pullCalls.Load() != 1 {
		t.Fatalf("OnWindowFocus within cooldown must not sync, syncing=%v pull=%d", s.sync.syncing, pullCalls.Load())
	}

	// 进入编辑态：即使过了冷却期，编辑期间保存（含自动保存）和窗口聚焦都不得触发同步。
	clk.Advance(60 * time.Second)
	s.NewEntry(diary.DateKey{Year: 2026, Month: 4, Day: 20})
	s.ed.title = "写作中"
	s.ed.markdown = "第一段"
	if err := s.SaveNow(); err != nil {
		t.Fatalf("SaveNow: %v", err)
	}
	s.OnWindowFocus()
	s.TriggerAutoSync(false)
	s.TriggerAutoSync(true)
	if s.sync.syncing || pushCalls.Load() != 0 || pullCalls.Load() != 1 {
		t.Fatalf("editing must not trigger sync, syncing=%v push=%d pull=%d", s.sync.syncing, pushCalls.Load(), pullCalls.Load())
	}

	// 退出编辑后立刻触发一次推送。
	s.DoneEditing()
	drainOne()
	if pushCalls.Load() != 1 || pullCalls.Load() != 2 {
		t.Fatalf("DoneEditing expected push=1 pull=2, got push=%d pull=%d", pushCalls.Load(), pullCalls.Load())
	}
	if s.sync.status.PendingCount != 0 {
		t.Fatalf("expected PendingCount=0 after sync, got %d", s.sync.status.PendingCount)
	}

	// 超过冷却期后重新聚焦触发拉取。
	clk.Advance(35 * time.Second)
	s.OnWindowFocus()
	drainOne()
	if pullCalls.Load() != 3 {
		t.Fatalf("OnWindowFocus after cooldown expected pull=3, got %d", pullCalls.Load())
	}

	// 删除日记立即推送墓碑（忽略冷却）。
	id := s.entries[0].ID
	s.RequestDelete(id)
	s.RequestDelete(id)
	drainOne()
	if pushCalls.Load() != 2 {
		t.Fatalf("RequestDelete expected push=2, got %d", pushCalls.Load())
	}

	// 编辑态直接退出应用：ShutdownBeforeQuit 应落盘并同步推送一次。
	s.NewEntry(diary.DateKey{Year: 2026, Month: 4, Day: 20})
	s.ed.title = "退出前草稿"
	s.ed.markdown = "尚未退出编辑"
	s.ed.dirty = true
	s.ShutdownBeforeQuit()
	if pushCalls.Load() != 3 {
		t.Fatalf("ShutdownBeforeQuit expected push=3, got %d", pushCalls.Load())
	}
}
