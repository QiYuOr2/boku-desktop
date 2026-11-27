package ui

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"boku/internal/diary"
	"boku/internal/domain"
	"boku/internal/platform/httpc"
	"boku/internal/presentation"
	"boku/internal/settings"
)

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

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
