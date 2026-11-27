package diary

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/sqlite"
)

func TestStoreSaveDeleteSearchAndFutureDateRejected(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	id, _ := domain.NewID()

	_, err = store.Save(Entry{
		ID:           id,
		Title:        "未来",
		DiaryDateKey: DateKey{Year: 2026, Month: 4, Day: 21},
	}, nil, nil, now)
	if !errors.Is(err, ErrFutureDate) {
		t.Fatalf("expected ErrFutureDate, got %v", err)
	}

	attID, _ := domain.NewID()
	attBytes := []byte("fake-jpeg-bytes-1234567890")
	upload := AttachmentUpload{
		ID:       attID,
		Filename: attID + ".jpg",
		Data:     attBytes,
		AddedAt:  now,
	}

	saved, err := store.Save(Entry{
		ID:           id,
		Title:        "周日散步",
		Markdown:     "沿西湖走了五公里",
		DiaryDateKey: DateKey{Year: 2026, Month: 4, Day: 19},
		Tags:         []string{"散步", "杭州"},
		Attachments:  []Attachment{upload.Attachment()},
	}, []AttachmentUpload{upload}, nil, now)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !store.AttachmentExists(upload.Filename) {
		t.Fatal("attachment file should exist on disk")
	}
	if len(saved.Attachments) != 1 || saved.Attachments[0].SHA256 == nil || *saved.Attachments[0].SHA256 != SHA256Hex(attBytes) {
		t.Fatalf("attachment metadata not computed: %+v", saved.Attachments)
	}

	rows1, err := store.PendingOutbox(10)
	if err != nil || len(rows1) != 1 {
		t.Fatalf("PendingOutbox: len=%d err=%v", len(rows1), err)
	}
	later := now.Add(5 * time.Minute)
	updated, err := store.Save(Entry{
		ID:           id,
		Title:        "周日傍晚散步",
		Markdown:     "沿西湖走了六公里",
		DiaryDateKey: DateKey{Year: 2026, Month: 4, Day: 19},
		Tags:         []string{"散步"},
		Attachments:  saved.Attachments,
	}, nil, nil, later)
	if err != nil {
		t.Fatalf("Save update: %v", err)
	}
	if !updated.CreatedAt.Equal(saved.CreatedAt) {
		t.Fatalf("createdAt changed on update: %v vs %v", updated.CreatedAt, saved.CreatedAt)
	}
	rows2, err := store.PendingOutbox(10)
	if err != nil || len(rows2) != 1 || rows2[0].MutationID != rows1[0].MutationID {
		t.Fatalf("unsent outbox row should reuse mutationId: before=%+v after=%+v", rows1, rows2)
	}

	found, err := store.SearchEntries("六公里", 20)
	if err != nil || len(found) != 1 || found[0].ID != id {
		t.Fatalf("SearchEntries: len=%d err=%v", len(found), err)
	}
	counts, err := store.DayCounts()
	if err != nil || counts[DateKey{Year: 2026, Month: 4, Day: 19}] != 1 {
		t.Fatalf("DayCounts: %+v err=%v", counts, err)
	}

	if err := store.Delete(id, later.Add(time.Minute)); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	rows3, err := store.PendingOutbox(10)
	if err != nil || len(rows3) != 0 {
		t.Fatalf("deleting never-synced entry must remove outbox, got %+v err=%v", rows3, err)
	}
	got, err := store.Entry(id, false)
	if err != nil || got != nil {
		t.Fatalf("deleted entry must not be returned by Entry(includeDeleted=false)")
	}
}

func TestStoreDelayedAttachmentDeletionAndSyncedDeleteTombstone(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	id, _ := domain.NewID()
	attID, _ := domain.NewID()
	upload := AttachmentUpload{ID: attID, Filename: attID + ".png", Data: []byte("png-data"), AddedAt: now}

	_, err = store.Save(Entry{
		ID:           id,
		Title:        "含附件",
		DiaryDateKey: DateKey{Year: 2026, Month: 4, Day: 20},
		Attachments:  []Attachment{upload.Attachment()},
	}, []AttachmentUpload{upload}, nil, now)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	rows, _ := store.PendingOutbox(10)
	if err := store.FreezeOutbox(rows[0].Seq, now); err != nil {
		t.Fatalf("FreezeOutbox: %v", err)
	}
	_, err = store.Save(Entry{
		ID:           id,
		Title:        "移除附件",
		DiaryDateKey: DateKey{Year: 2026, Month: 4, Day: 20},
		Attachments:  nil,
	}, nil, map[string]bool{attID: true}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Save removing attachment: %v", err)
	}
	if !store.AttachmentExists(upload.Filename) {
		t.Fatal("attachment still referenced by frozen outbox must not be deleted yet")
	}

	if err := store.ConfirmOutboxApplied(rows[0].MutationID, 1); err != nil {
		t.Fatalf("ConfirmOutboxApplied: %v", err)
	}
	if err := store.Delete(id, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("Delete synced entry: %v", err)
	}
	pending, err := store.PendingOutbox(10)
	if err != nil || len(pending) != 1 || pending[0].Operation != OperationDelete || pending[0].BaseVersion != 1 {
		t.Fatalf("expected delete outbox with baseVersion=1, got %+v err=%v", pending, err)
	}
}

func TestStoreMigrationV1ToV2AndUnsupportedSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, DBFileName)
	attDir := filepath.Join(dir, AttachmentsDirName)
	if err := os.MkdirAll(attDir, 0o700); err != nil {
		t.Fatal(err)
	}

	existingAttID := "019655c0-0000-7000-8000-000000000101"
	missingAttID := "019655c0-0000-7000-8000-000000000102"
	attData := []byte("legacy-v1-image-bytes")
	if err := os.WriteFile(filepath.Join(attDir, existingAttID+".jpg"), attData, 0o600); err != nil {
		t.Fatal(err)
	}

	rawDB, err := sqlite.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open raw v1 db: %v", err)
	}
	attsJSON, _ := json.Marshal([]Attachment{
		{ID: existingAttID, Filename: existingAttID + ".jpg", AddedAt: time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC)},
		{ID: missingAttID, Filename: missingAttID + ".png", AddedAt: time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC)},
	})
	liveID := "019655c0-0000-7000-8000-000000000201"
	delID := "019655c0-0000-7000-8000-000000000202"
	_, err = rawDB.Transaction(context.Background(), []sqlite.Statement{
		{SQL: `CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`},
		{SQL: `INSERT INTO schema_migrations (version, applied_at) VALUES (1, '2026-04-01T00:00:00Z')`},
		{SQL: `CREATE TABLE diary_entries (
			id TEXT PRIMARY KEY NOT NULL, title TEXT NOT NULL, markdown TEXT NOT NULL,
			diaryDateKey TEXT NOT NULL, createdAt TEXT NOT NULL, updatedAt TEXT NOT NULL,
			deletedAt TEXT, tags TEXT NOT NULL, attachments TEXT NOT NULL,
			linkCards TEXT NOT NULL, location TEXT, weather TEXT
		)`},
		{
			SQL:  `INSERT INTO diary_entries VALUES (?, ?, ?, ?, ?, ?, NULL, '["旧标签"]', ?, '[]', NULL, NULL)`,
			Args: []any{liveID, "旧版本日记", "迁移测试", "2026-04-01", "2026-04-01T08:00:00Z", "2026-04-01T08:00:00Z", string(attsJSON)},
		},
		{
			SQL:  `INSERT INTO diary_entries VALUES (?, ?, ?, ?, ?, ?, ?, '[]', '[]', '[]', NULL, NULL)`,
			Args: []any{delID, "已删旧日记", "", "2026-04-01", "2026-04-01T08:00:00Z", "2026-04-01T08:00:00Z", "2026-04-02T08:00:00Z"},
		},
	})
	if err != nil {
		t.Fatalf("seed v1 db: %v", err)
	}
	_ = rawDB.Close()

	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open v1 db for migration: %v", err)
	}
	entry, err := store.Entry(liveID, false)
	if err != nil || entry == nil {
		t.Fatalf("migrated entry missing: %v", err)
	}
	if len(entry.Attachments) != 2 {
		t.Fatalf("expected 2 attachments preserved, got %d", len(entry.Attachments))
	}
	if entry.Attachments[0].SHA256 == nil || *entry.Attachments[0].SHA256 != SHA256Hex(attData) {
		t.Fatalf("existing attachment sha256 not backfilled: %+v", entry.Attachments[0])
	}
	if entry.Attachments[0].MimeType == nil || *entry.Attachments[0].MimeType != "image/jpeg" {
		t.Fatalf("existing attachment mimeType not backfilled: %+v", entry.Attachments[0])
	}
	if entry.Attachments[1].SHA256 != nil {
		t.Fatalf("missing attachment file should not fail migration or fabricate hash")
	}
	outbox, err := store.PendingOutbox(10)
	if err != nil || len(outbox) != 1 || outbox[0].EntityID != liveID {
		t.Fatalf("only live v1 entry should be queued in outbox, got %+v err=%v", outbox, err)
	}
	_ = store.Close()

	rawDB2, err := sqlite.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = rawDB2.Execute(context.Background(), `INSERT INTO schema_migrations (version, applied_at) VALUES (3, '2026-04-05T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	_ = rawDB2.Close()

	if _, err := Open(dir); !errors.Is(err, ErrUnreadableDB) {
		t.Fatalf("expected ErrUnreadableDB for higher schema version, got %v", err)
	}
}

func TestStoreApplyPullPageRules(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	e1 := "019655c0-0000-7000-8000-000000000301"
	eBlocked := "019655c0-0000-7000-8000-000000000302"

	_, err = store.Save(Entry{
		ID:           eBlocked,
		Title:        "本地未推版本",
		DiaryDateKey: DateKey{Year: 2026, Month: 4, Day: 20},
	}, nil, nil, now)
	if err != nil {
		t.Fatal(err)
	}

	p1, _ := json.Marshal(DiaryEntryPayload{
		Title:        strPtr("云端 v1"),
		Markdown:     strPtr("正文1"),
		DiaryDateKey: "2026-04-19",
		CreatedAt:    now,
		UpdatedAt:    now,
		Tags:         []string{},
		Attachments:  []AttachmentRef{},
		LinkCards:    json.RawMessage(`[]`),
		Location:     json.RawMessage(`null`),
		Weather:      json.RawMessage(`null`),
	})
	p2, _ := json.Marshal(DiaryEntryPayload{
		Title:        strPtr("云端 v2"),
		Markdown:     strPtr("正文2"),
		DiaryDateKey: "2026-04-19",
		CreatedAt:    now,
		UpdatedAt:    now.Add(time.Minute),
		Tags:         []string{"更新"},
		Attachments:  []AttachmentRef{},
		LinkCards:    json.RawMessage(`[]`),
		Location:     json.RawMessage(`null`),
		Weather:      json.RawMessage(`null`),
	})

	applied, err := store.ApplyPullPage([]Change{
		{Cursor: "1", EntityType: EntityDiaryEntry, EntityID: e1, Version: 1, Operation: OperationUpsert, Payload: p1},
		{Cursor: "2", EntityType: EntityDiaryEntry, EntityID: e1, Version: 2, Operation: OperationUpsert, Payload: p2},
		{Cursor: "3", EntityType: EntityDiaryEntry, EntityID: eBlocked, Version: 5, Operation: OperationUpsert, Payload: p2},
	}, "3", now)
	if err != nil {
		t.Fatalf("ApplyPullPage: %v", err)
	}
	if len(applied) != 2 {
		t.Fatalf("expected 2 applied payloads for e1, got %d", len(applied))
	}
	got1, _ := store.Entry(e1, false)
	if got1 == nil || got1.Title != "云端 v2" || got1.SyncVersion != 2 {
		t.Fatalf("e1 mismatch: %+v", got1)
	}
	gotBlocked, _ := store.Entry(eBlocked, false)
	if gotBlocked == nil || gotBlocked.Title != "本地未推版本" {
		t.Fatalf("blocked entry must not be overwritten: %+v", gotBlocked)
	}

	appliedAgain, err := store.ApplyPullPage([]Change{
		{Cursor: "2", EntityType: EntityDiaryEntry, EntityID: e1, Version: 2, Operation: OperationUpsert, Payload: p2},
	}, "3", now)
	if err != nil || len(appliedAgain) != 0 {
		t.Fatalf("echo change must be skipped, got len=%d err=%v", len(appliedAgain), err)
	}

	_, err = store.ApplyPullPage([]Change{
		{Cursor: "4", EntityType: EntityDiaryEntry, EntityID: e1, Version: 3, Operation: OperationDelete},
	}, "4", now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("ApplyPullPage tombstone: %v", err)
	}
	if got, _ := store.Entry(e1, false); got != nil {
		t.Fatalf("tombstone should soft-delete e1")
	}
}

func strPtr(s string) *string { return &s }
