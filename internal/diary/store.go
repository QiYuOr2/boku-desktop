package diary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/atomicfile"
	"boku/internal/platform/sqlite"
)

const (
	DBFileName         = "diary.sqlite"
	AttachmentsDirName = "diary_attachments"
	ConflictsFileName  = "diary-sync-conflicts.json"
	ImageCacheDirName  = "diary_images_cache"
	schemaVersion      = 2
)

// Store 独占访问 diary.sqlite 与本地附件目录。
// 所有读写都在 mu 保护下序列化，保证读-改-写与单事务提交的一致性。
type Store struct {
	mu             sync.Mutex
	db             *sqlite.DB
	dir            string
	dbPath         string
	attachmentsDir string
	conflictsPath  string
}

// Open 打开或创建 dir 下的 diary.sqlite，并按顺序执行事务式迁移。
// 遇到更高版本或损坏库时返回 ErrUnreadableDB，不覆盖原文件。
func Open(dir string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("%w: diary directory is empty", domain.ErrInvalidArgument)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("diary: create data dir: %w", err)
	}
	dbPath := filepath.Join(dir, DBFileName)
	db, err := sqlite.Open(context.Background(), dbPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreadableDB, err)
	}
	store := &Store{
		db:             db,
		dir:            dir,
		dbPath:         dbPath,
		attachmentsDir: filepath.Join(dir, AttachmentsDirName),
		conflictsPath:  filepath.Join(dir, ConflictsFileName),
	}
	if err := store.initAndMigrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

func (s *Store) Dir() string            { return s.dir }
func (s *Store) AttachmentsDir() string { return s.attachmentsDir }
func (s *Store) ConflictsPath() string  { return s.conflictsPath }

func (s *Store) AttachmentPath(filename string) string {
	return filepath.Join(s.attachmentsDir, filename)
}

func (s *Store) AttachmentExists(filename string) bool {
	if !IsSafeAttachmentFilename(filename) {
		return false
	}
	_, err := os.Stat(s.AttachmentPath(filename))
	return err == nil
}

func (s *Store) ReadAttachment(filename string) ([]byte, error) {
	if !IsSafeAttachmentFilename(filename) {
		return nil, ErrInvalidAttachment
	}
	return os.ReadFile(s.AttachmentPath(filename))
}

func (s *Store) WriteAttachment(data []byte, filename string) error {
	if !IsSafeAttachmentFilename(filename) || len(data) == 0 {
		return ErrInvalidAttachment
	}
	if len(data) > MaxAttachmentBytes {
		return ErrAttachmentTooLarge
	}
	if err := os.MkdirAll(s.attachmentsDir, 0o700); err != nil {
		return err
	}
	return atomicfile.Write(s.AttachmentPath(filename), data)
}

func (s *Store) initAndMigrate() error {
	ctx := context.Background()
	if _, err := s.db.Query(ctx, `PRAGMA journal_mode = WAL`); err != nil {
		return fmt.Errorf("%w: set WAL mode: %v", ErrUnreadableDB, err)
	}
	current, err := s.schemaVersion(ctx)
	if err != nil {
		return err
	}
	if current > schemaVersion {
		return fmt.Errorf("%w: unsupported version %d", ErrUnreadableDB, current)
	}
	if current < 1 {
		if err := s.applyMigration(ctx, 1, []sqlite.Statement{
			{SQL: `CREATE TABLE schema_migrations (
				version INTEGER PRIMARY KEY,
				applied_at TEXT NOT NULL
			)`},
			{SQL: `CREATE TABLE diary_entries (
				id TEXT PRIMARY KEY NOT NULL,
				title TEXT NOT NULL,
				markdown TEXT NOT NULL,
				diaryDateKey TEXT NOT NULL,
				createdAt TEXT NOT NULL,
				updatedAt TEXT NOT NULL,
				deletedAt TEXT,
				tags TEXT NOT NULL,
				attachments TEXT NOT NULL,
				linkCards TEXT NOT NULL,
				location TEXT,
				weather TEXT
			)`},
			{SQL: `CREATE INDEX diary_entries_day ON diary_entries(deletedAt, diaryDateKey, createdAt)`},
		}); err != nil {
			return err
		}
	}
	if current < 2 {
		if err := s.upgradeToV2(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) schemaVersion(ctx context.Context) (int64, error) {
	rows, err := s.db.Query(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrUnreadableDB, err)
	}
	if len(rows.Values) == 0 {
		tables, err := s.db.Query(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
		if err != nil {
			return 0, fmt.Errorf("%w: %v", ErrUnreadableDB, err)
		}
		if len(tables.Values) > 0 {
			return 0, fmt.Errorf("%w: unexpected existing schema without schema_migrations", ErrUnreadableDB)
		}
		return 0, nil
	}
	rows, err = s.db.Query(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrUnreadableDB, err)
	}
	var current int64
	for _, row := range rows.Values {
		version, ok := row[0].(int64)
		if !ok || version != current+1 {
			return 0, fmt.Errorf("%w: schema versions are not contiguous", ErrUnreadableDB)
		}
		current = version
	}
	return current, nil
}

func (s *Store) applyMigration(ctx context.Context, version int64, stmts []sqlite.Statement) error {
	stmts = append(stmts, sqlite.Statement{
		SQL:  `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		Args: []any{version, time.Now().UTC().Format(time.RFC3339)},
	})
	if _, err := s.db.Transaction(ctx, stmts); err != nil {
		return fmt.Errorf("%w: migrate to v%d: %v", ErrUnreadableDB, version, err)
	}
	return nil
}

// upgradeToV2 在单个事务内增加 syncVersion、sync_outbox、sync_state，
// 为旧附件补算元数据并把已有未删除日记入队。
func (s *Store) upgradeToV2(ctx context.Context) error {
	existing, err := s.queryEntriesLocked(ctx, `SELECT id, title, markdown, diaryDateKey, createdAt, updatedAt, deletedAt, tags, attachments, linkCards, location, weather, 0 FROM diary_entries ORDER BY createdAt`)
	if err != nil {
		return fmt.Errorf("%w: read v1 entries: %v", ErrUnreadableDB, err)
	}

	stmts := []sqlite.Statement{
		{SQL: `ALTER TABLE diary_entries ADD COLUMN syncVersion INTEGER NOT NULL DEFAULT 0`},
		{SQL: `CREATE TABLE sync_outbox (
			seq INTEGER PRIMARY KEY AUTOINCREMENT,
			mutationId TEXT NOT NULL UNIQUE,
			entityId TEXT NOT NULL,
			operation TEXT NOT NULL,
			createdAt TEXT NOT NULL,
			baseVersion INTEGER NOT NULL,
			payload TEXT,
			sentAt TEXT,
			conflictAt TEXT
		)`},
		{SQL: `CREATE INDEX sync_outbox_entity ON sync_outbox(entityId, sentAt)`},
		{SQL: `CREATE TABLE sync_state (
			id INTEGER PRIMARY KEY CHECK(id = 1),
			userId TEXT,
			cursor TEXT,
			lastSyncedAt TEXT
		)`},
		{SQL: `INSERT INTO sync_state (id) VALUES (1)`},
	}

	for _, entry := range existing {
		changed := false
		for i := range entry.Attachments {
			att := &entry.Attachments[i]
			if att.SHA256 != nil && att.Size != nil && att.MimeType != nil {
				continue
			}
			data, readErr := os.ReadFile(s.AttachmentPath(att.Filename))
			if readErr != nil {
				continue
			}
			if att.SHA256 == nil {
				sum := SHA256Hex(data)
				att.SHA256 = &sum
			}
			if att.Size == nil {
				size := int64(len(data))
				att.Size = &size
			}
			if att.MimeType == nil {
				if mime := MimeTypeForFilename(att.Filename); mime != "" {
					att.MimeType = &mime
				}
			}
			changed = true
		}
		if changed {
			attJSON, err := json.Marshal(entry.Attachments)
			if err != nil {
				return err
			}
			stmts = append(stmts, sqlite.Statement{
				SQL:  `UPDATE diary_entries SET attachments = ? WHERE id = ?`,
				Args: []any{string(attJSON), entry.ID},
			})
		}
		if entry.DeletedAt == nil {
			payloadStr, err := encodeOutboxPayload(entry)
			if err != nil {
				return err
			}
			mutID, err := domain.NewID()
			if err != nil {
				return err
			}
			stmts = append(stmts, sqlite.Statement{
				SQL:  `INSERT INTO sync_outbox (mutationId, entityId, operation, createdAt, baseVersion, payload) VALUES (?, ?, ?, ?, ?, ?)`,
				Args: []any{mutID, entry.ID, OperationUpsert, formatUTC(entry.UpdatedAt), int64(0), payloadStr},
			})
		}
	}

	return s.applyMigration(ctx, 2, stmts)
}

func (s *Store) Entry(id string, includeDeleted bool) (*Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, ErrUnreadableDB
	}
	return s.entryLocked(context.Background(), strings.ToLower(strings.TrimSpace(id)), includeDeleted)
}

func (s *Store) entryLocked(ctx context.Context, id string, includeDeleted bool) (*Entry, error) {
	sql := `SELECT id, title, markdown, diaryDateKey, createdAt, updatedAt, deletedAt, tags, attachments, linkCards, location, weather, syncVersion FROM diary_entries WHERE id = ?`
	if !includeDeleted {
		sql += ` AND deletedAt IS NULL`
	}
	entries, err := s.queryEntriesLocked(ctx, sql, id)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}
	entry := entries[0]
	return &entry, nil
}

func (s *Store) EntriesOn(key DateKey) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, ErrUnreadableDB
	}
	return s.queryEntriesLocked(
		context.Background(),
		`SELECT id, title, markdown, diaryDateKey, createdAt, updatedAt, deletedAt, tags, attachments, linkCards, location, weather, syncVersion FROM diary_entries WHERE deletedAt IS NULL AND diaryDateKey = ? ORDER BY createdAt DESC`,
		key.String(),
	)
}

func (s *Store) RecentEntries(limit, offset int) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, ErrUnreadableDB
	}
	if limit <= 0 {
		limit = 30
	}
	if offset < 0 {
		offset = 0
	}
	return s.queryEntriesLocked(
		context.Background(),
		`SELECT id, title, markdown, diaryDateKey, createdAt, updatedAt, deletedAt, tags, attachments, linkCards, location, weather, syncVersion FROM diary_entries WHERE deletedAt IS NULL ORDER BY diaryDateKey DESC, createdAt DESC LIMIT ? OFFSET ?`,
		limit, offset,
	)
}

func (s *Store) SearchEntries(query string, limit int) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, ErrUnreadableDB
	}
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return s.queryEntriesLocked(
			context.Background(),
			`SELECT id, title, markdown, diaryDateKey, createdAt, updatedAt, deletedAt, tags, attachments, linkCards, location, weather, syncVersion FROM diary_entries WHERE deletedAt IS NULL ORDER BY diaryDateKey DESC, createdAt DESC LIMIT ?`,
			max(limit, 30),
		)
	}
	if limit <= 0 {
		limit = 50
	}
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(trimmed)
	pattern := "%" + escaped + "%"
	return s.queryEntriesLocked(
		context.Background(),
		`SELECT id, title, markdown, diaryDateKey, createdAt, updatedAt, deletedAt, tags, attachments, linkCards, location, weather, syncVersion
		 FROM diary_entries
		 WHERE deletedAt IS NULL AND (title LIKE ? ESCAPE '\' OR markdown LIKE ? ESCAPE '\' OR tags LIKE ? ESCAPE '\' OR diaryDateKey LIKE ? ESCAPE '\')
		 ORDER BY diaryDateKey DESC, createdAt DESC LIMIT ?`,
		pattern, pattern, pattern, pattern, limit,
	)
}

func (s *Store) DayCounts() (map[DateKey]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, ErrUnreadableDB
	}
	rows, err := s.db.Query(context.Background(), `SELECT diaryDateKey, COUNT(*) FROM diary_entries WHERE deletedAt IS NULL GROUP BY diaryDateKey`)
	if err != nil {
		return nil, err
	}
	counts := make(map[DateKey]int, len(rows.Values))
	for _, row := range rows.Values {
		rawKey, _ := row[0].(string)
		total, ok := row[1].(int64)
		key, parseErr := ParseDateKey(rawKey)
		if parseErr != nil || !ok {
			return nil, ErrUnreadableDB
		}
		counts[key] = int(total)
	}
	return counts, nil
}

// Save 保存或更新一篇日记并在同一事务中更新 sync_outbox。
// 拒绝未来日期；保留既有条目的 createdAt；附件延迟删除。
func (s *Store) Save(entry Entry, uploads []AttachmentUpload, removedAttachmentIDs map[string]bool, now time.Time) (Entry, error) {
	if now.IsZero() {
		now = time.Now()
	}
	if entry.DiaryDateKey.IsFuture(now) {
		return Entry{}, ErrFutureDate
	}
	saved, err := entry.Normalize(now)
	if err != nil {
		return Entry{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return Entry{}, ErrUnreadableDB
	}
	ctx := context.Background()

	var previousAttachments []Attachment
	var currentVersion int64
	if existing, err := s.entryLocked(ctx, saved.ID, true); err != nil {
		return Entry{}, err
	} else if existing != nil {
		if existing.DeletedAt != nil {
			return Entry{}, ErrMissingEntry
		}
		saved.CreatedAt = existing.CreatedAt
		previousAttachments = existing.Attachments
		currentVersion = existing.SyncVersion
	}

	if len(uploads) > 0 {
		uploadsByID := make(map[string]AttachmentUpload, len(uploads))
		for _, up := range uploads {
			uploadsByID[strings.ToLower(up.ID)] = up
		}
		for i := range saved.Attachments {
			up, ok := uploadsByID[saved.Attachments[i].ID]
			if !ok {
				continue
			}
			sum := SHA256Hex(up.Data)
			size := int64(len(up.Data))
			mime := MimeTypeForFilename(up.Filename)
			saved.Attachments[i].SHA256 = &sum
			saved.Attachments[i].Size = &size
			if mime != "" {
				saved.Attachments[i].MimeType = &mime
			}
		}
	}

	var written []string
	cleanupWritten := func() {
		for _, path := range written {
			_ = os.Remove(path)
		}
	}
	if len(uploads) > 0 {
		if err := os.MkdirAll(s.attachmentsDir, 0o700); err != nil {
			return Entry{}, err
		}
		for _, up := range uploads {
			if len(up.Data) > MaxAttachmentBytes {
				cleanupWritten()
				return Entry{}, ErrAttachmentTooLarge
			}
			if len(up.Data) == 0 || !IsSafeAttachmentFilename(up.Filename) {
				cleanupWritten()
				return Entry{}, ErrInvalidAttachment
			}
			target := s.AttachmentPath(up.Filename)
			if _, statErr := os.Stat(target); statErr == nil {
				continue
			}
			if err := atomicfile.Write(target, up.Data); err != nil {
				cleanupWritten()
				return Entry{}, err
			}
			written = append(written, target)
		}
	}

	upsertStmt, err := buildUpsertStatement(saved, nil)
	if err != nil {
		cleanupWritten()
		return Entry{}, err
	}
	payloadStr, err := encodeOutboxPayload(saved)
	if err != nil {
		cleanupWritten()
		return Entry{}, err
	}
	outboxStmt, err := s.buildReplaceUnsentOutboxStmtLocked(ctx, saved.ID, OperationUpsert, &payloadStr, currentVersion, saved.UpdatedAt)
	if err != nil {
		cleanupWritten()
		return Entry{}, err
	}
	if _, err := s.db.Transaction(ctx, []sqlite.Statement{upsertStmt, outboxStmt}); err != nil {
		cleanupWritten()
		return Entry{}, err
	}
	saved.SyncVersion = currentVersion

	// 延迟删除附件：只有全部条目（含软删除）与 outbox payload 都不再引用时才删文件。
	if len(removedAttachmentIDs) > 0 && len(previousAttachments) > 0 {
		kept := make(map[string]bool, len(saved.Attachments))
		for _, att := range saved.Attachments {
			kept[att.ID] = true
		}
		var candidates []Attachment
		for _, att := range previousAttachments {
			if removedAttachmentIDs[att.ID] && !kept[att.ID] {
				candidates = append(candidates, att)
			}
		}
		if len(candidates) > 0 {
			if referenced, err := s.referencedAttachmentIDsLocked(ctx); err == nil {
				for _, att := range candidates {
					if !referenced[att.ID] {
						_ = os.Remove(s.AttachmentPath(att.Filename))
					}
				}
			}
		}
	}
	return saved, nil
}

// Delete 软删除一篇日记并在同一事务中更新 sync_outbox。
// 若该日记从未到过服务端（syncVersion == 0 且无已发送 mutation），直接撤掉待发送记录，不产生 tombstone。
func (s *Store) Delete(id string, now time.Time) error {
	id = strings.ToLower(strings.TrimSpace(id))
	if !IsID(id) {
		return ErrMissingEntry
	}
	if now.IsZero() {
		now = time.Now()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return ErrUnreadableDB
	}
	ctx := context.Background()

	version, err := s.syncVersionLocked(ctx, id)
	if err != nil {
		return err
	}
	sentRows, err := s.db.Query(ctx, `SELECT seq FROM sync_outbox WHERE entityId = ? AND sentAt IS NOT NULL LIMIT 1`, id)
	if err != nil {
		return err
	}
	hasSent := len(sentRows.Values) > 0

	ts := formatUTC(now)
	stmts := []sqlite.Statement{
		{
			SQL:  `UPDATE diary_entries SET deletedAt = ?, updatedAt = ? WHERE id = ? AND deletedAt IS NULL`,
			Args: []any{ts, ts, id},
		},
	}
	if version == 0 && !hasSent {
		stmts = append(stmts, sqlite.Statement{
			SQL:  `DELETE FROM sync_outbox WHERE entityId = ?`,
			Args: []any{id},
		})
	} else {
		outboxStmt, err := s.buildReplaceUnsentOutboxStmtLocked(ctx, id, OperationDelete, nil, version, now)
		if err != nil {
			return err
		}
		stmts = append(stmts, outboxStmt)
	}
	_, err = s.db.Transaction(ctx, stmts)
	return err
}

func (s *Store) PendingOutbox(limit int) ([]OutboxRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, ErrUnreadableDB
	}
	if limit <= 0 {
		limit = MaxMutationsPerPush
	}
	rows, err := s.db.Query(
		context.Background(),
		`SELECT seq, mutationId, entityId, operation, createdAt, baseVersion, payload, sentAt, conflictAt
		 FROM sync_outbox WHERE conflictAt IS NULL ORDER BY seq LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	return decodeOutboxRows(rows)
}

func (s *Store) ConflictedOutbox() ([]OutboxRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, ErrUnreadableDB
	}
	rows, err := s.db.Query(
		context.Background(),
		`SELECT seq, mutationId, entityId, operation, createdAt, baseVersion, payload, sentAt, conflictAt
		 FROM sync_outbox WHERE conflictAt IS NOT NULL ORDER BY conflictAt`,
	)
	if err != nil {
		return nil, err
	}
	return decodeOutboxRows(rows)
}

func (s *Store) OutboxCounts() (pending, conflicts int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return 0, 0, ErrUnreadableDB
	}
	ctx := context.Background()
	pRows, err := s.db.Query(ctx, `SELECT COUNT(*) FROM sync_outbox WHERE conflictAt IS NULL`)
	if err != nil {
		return 0, 0, err
	}
	cRows, err := s.db.Query(ctx, `SELECT COUNT(*) FROM sync_outbox WHERE conflictAt IS NOT NULL`)
	if err != nil {
		return 0, 0, err
	}
	if len(pRows.Values) > 0 {
		if n, ok := pRows.Values[0][0].(int64); ok {
			pending = int(n)
		}
	}
	if len(cRows.Values) > 0 {
		if n, ok := cRows.Values[0][0].(int64); ok {
			conflicts = int(n)
		}
	}
	return pending, conflicts, nil
}

func (s *Store) FreezeOutbox(seq int64, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return ErrUnreadableDB
	}
	_, err := s.db.Execute(context.Background(), `UPDATE sync_outbox SET sentAt = ? WHERE seq = ? AND sentAt IS NULL`, formatUTC(at), seq)
	return err
}

func (s *Store) ConfirmOutboxApplied(mutationID string, version int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return ErrUnreadableDB
	}
	ctx := context.Background()
	rows, err := s.db.Query(ctx, `SELECT entityId FROM sync_outbox WHERE mutationId = ?`, mutationID)
	if err != nil {
		return err
	}
	if len(rows.Values) == 0 {
		return ErrMissingEntry
	}
	entityID, _ := rows.Values[0][0].(string)
	_, err = s.db.Transaction(ctx, []sqlite.Statement{
		{SQL: `DELETE FROM sync_outbox WHERE mutationId = ?`, Args: []any{mutationID}},
		{SQL: `UPDATE diary_entries SET syncVersion = ? WHERE id = ?`, Args: []any{version, entityID}},
	})
	return err
}

func (s *Store) MarkOutboxConflict(mutationID string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return ErrUnreadableDB
	}
	_, err := s.db.Execute(context.Background(), `UPDATE sync_outbox SET conflictAt = ? WHERE mutationId = ?`, formatUTC(at), mutationID)
	return err
}

func (s *Store) SyncState() (userID, cursor string, lastSyncedAt *time.Time, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return "", "", nil, ErrUnreadableDB
	}
	rows, err := s.db.Query(context.Background(), `SELECT userId, cursor, lastSyncedAt FROM sync_state WHERE id = 1`)
	if err != nil {
		return "", "", nil, err
	}
	if len(rows.Values) == 0 {
		return "", "", nil, nil
	}
	row := rows.Values[0]
	if v, ok := row[0].(string); ok {
		userID = v
	}
	if v, ok := row[1].(string); ok {
		cursor = v
	}
	if v, ok := row[2].(string); ok && v != "" {
		if parsed, parseErr := parseUTC(v); parseErr == nil {
			lastSyncedAt = &parsed
		}
	}
	return userID, cursor, lastSyncedAt, nil
}

func (s *Store) SetSyncUserID(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return ErrUnreadableDB
	}
	var val any
	if strings.TrimSpace(userID) != "" {
		val = strings.TrimSpace(userID)
	}
	_, err := s.db.Execute(context.Background(), `UPDATE sync_state SET userId = ? WHERE id = 1`, val)
	return err
}

func (s *Store) SetSyncCursor(cursor string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return ErrUnreadableDB
	}
	var val any
	if strings.TrimSpace(cursor) != "" {
		val = strings.TrimSpace(cursor)
	}
	_, err := s.db.Execute(context.Background(), `UPDATE sync_state SET cursor = ? WHERE id = 1`, val)
	return err
}

// ApplyPullPage 在同一个事务里应用一页 pull 变更并更新游标。
// 本地版本不低于变更版本或本地有未确认 outbox 的实体跳过不覆盖。
func (s *Store) ApplyPullPage(changes []Change, nextCursor string, syncedAt time.Time) ([]DiaryEntryPayload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, ErrUnreadableDB
	}
	ctx := context.Background()

	blockedRows, err := s.db.Query(ctx, `SELECT DISTINCT entityId FROM sync_outbox`)
	if err != nil {
		return nil, err
	}
	blocked := make(map[string]bool, len(blockedRows.Values))
	for _, r := range blockedRows.Values {
		if id, ok := r[0].(string); ok {
			blocked[strings.ToLower(id)] = true
		}
	}

	versions := make(map[string]int64)
	stmts := make([]sqlite.Statement, 0, len(changes)+1)
	var applied []DiaryEntryPayload

	for _, change := range changes {
		entityID := strings.ToLower(change.EntityID)
		if blocked[entityID] {
			continue
		}
		localVer, seen := versions[entityID]
		if !seen {
			v, err := s.syncVersionLocked(ctx, entityID)
			if err != nil {
				return nil, err
			}
			localVer = v
		}
		if localVer >= change.Version {
			continue
		}
		changeStmts, payload, err := buildApplyChangeStatements(change, syncedAt)
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, changeStmts...)
		versions[entityID] = change.Version
		if payload != nil {
			applied = append(applied, *payload)
		}
	}

	stmts = append(stmts, sqlite.Statement{
		SQL:  `UPDATE sync_state SET cursor = ?, lastSyncedAt = ? WHERE id = 1`,
		Args: []any{nextCursor, formatUTC(syncedAt)},
	})
	if _, err := s.db.Transaction(ctx, stmts); err != nil {
		return nil, err
	}
	return applied, nil
}

// ResolveConflict 解决单条冲突：同一事务里删除该实体的全部 outbox 行并落地云端变更；
// change 为 nil 时只把 syncVersion 归零，交由下次从头 pull 应用。
func (s *Store) ResolveConflict(entityID string, change *Change, now time.Time) error {
	entityID = strings.ToLower(strings.TrimSpace(entityID))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return ErrUnreadableDB
	}
	stmts := []sqlite.Statement{
		{SQL: `DELETE FROM sync_outbox WHERE entityId = ?`, Args: []any{entityID}},
	}
	if change != nil {
		changeStmts, _, err := buildApplyChangeStatements(*change, now)
		if err != nil {
			return err
		}
		stmts = append(stmts, changeStmts...)
	} else {
		stmts = append(stmts, sqlite.Statement{
			SQL:  `UPDATE diary_entries SET syncVersion = 0 WHERE id = ?`,
			Args: []any{entityID},
		})
	}
	_, err := s.db.Transaction(context.Background(), stmts)
	return err
}

func (s *Store) MissingAttachmentFiles() ([]Attachment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, ErrUnreadableDB
	}
	rows, err := s.db.Query(context.Background(), `SELECT attachments FROM diary_entries WHERE deletedAt IS NULL`)
	if err != nil {
		return nil, err
	}
	var missing []Attachment
	seen := map[string]bool{}
	for _, r := range rows.Values {
		raw, _ := r[0].(string)
		var atts []Attachment
		if err := json.Unmarshal([]byte(raw), &atts); err != nil {
			continue
		}
		for _, att := range atts {
			if att.SHA256 == nil || seen[att.ID] {
				continue
			}
			if _, statErr := os.Stat(s.AttachmentPath(att.Filename)); errors.Is(statErr, os.ErrNotExist) {
				seen[att.ID] = true
				missing = append(missing, att)
			}
		}
	}
	return missing, nil
}

func (s *Store) syncVersionLocked(ctx context.Context, id string) (int64, error) {
	rows, err := s.db.Query(ctx, `SELECT syncVersion FROM diary_entries WHERE id = ?`, id)
	if err != nil {
		return 0, err
	}
	if len(rows.Values) == 0 {
		return 0, nil
	}
	v, _ := rows.Values[0][0].(int64)
	return v, nil
}

func (s *Store) buildReplaceUnsentOutboxStmtLocked(ctx context.Context, entityID, operation string, payload *string, baseVersion int64, createdAt time.Time) (sqlite.Statement, error) {
	rows, err := s.db.Query(ctx, `SELECT seq FROM sync_outbox WHERE entityId = ? AND sentAt IS NULL ORDER BY seq LIMIT 1`, entityID)
	if err != nil {
		return sqlite.Statement{}, err
	}
	var payloadArg any
	if payload != nil {
		payloadArg = *payload
	}
	if len(rows.Values) > 0 {
		seq, _ := rows.Values[0][0].(int64)
		return sqlite.Statement{
			SQL:  `UPDATE sync_outbox SET operation = ?, payload = ?, baseVersion = ? WHERE seq = ?`,
			Args: []any{operation, payloadArg, baseVersion, seq},
		}, nil
	}
	mutID, err := domain.NewIDAt(createdAt)
	if err != nil {
		return sqlite.Statement{}, err
	}
	return sqlite.Statement{
		SQL:  `INSERT INTO sync_outbox (mutationId, entityId, operation, createdAt, baseVersion, payload) VALUES (?, ?, ?, ?, ?, ?)`,
		Args: []any{mutID, entityID, operation, formatUTC(createdAt), baseVersion, payloadArg},
	}, nil
}

func (s *Store) referencedAttachmentIDsLocked(ctx context.Context) (map[string]bool, error) {
	ids := map[string]bool{}
	entryRows, err := s.db.Query(ctx, `SELECT attachments FROM diary_entries`)
	if err != nil {
		return nil, err
	}
	for _, r := range entryRows.Values {
		raw, _ := r[0].(string)
		var atts []Attachment
		if json.Unmarshal([]byte(raw), &atts) == nil {
			for _, att := range atts {
				ids[strings.ToLower(att.ID)] = true
			}
		}
	}
	outboxRows, err := s.db.Query(ctx, `SELECT payload FROM sync_outbox WHERE payload IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	for _, r := range outboxRows.Values {
		raw, _ := r[0].(string)
		if payload, err := DecodeDiaryEntryPayload([]byte(raw)); err == nil {
			for _, ref := range payload.Attachments {
				ids[strings.ToLower(ref.AttachmentID)] = true
			}
		}
	}
	return ids, nil
}

func (s *Store) queryEntriesLocked(ctx context.Context, sql string, args ...any) ([]Entry, error) {
	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(rows.Values))
	for _, row := range rows.Values {
		entry, err := scanEntry(row)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func buildUpsertStatement(entry Entry, forceSyncVersion *int64) (sqlite.Statement, error) {
	tagsJSON, err := json.Marshal(nonNilStrings(entry.Tags))
	if err != nil {
		return sqlite.Statement{}, err
	}
	attsJSON, err := json.Marshal(nonNilAttachments(entry.Attachments))
	if err != nil {
		return sqlite.Statement{}, err
	}
	cardsJSON, err := json.Marshal(nonNilLinkCards(entry.LinkCards))
	if err != nil {
		return sqlite.Statement{}, err
	}
	var locArg any
	if entry.Location != nil {
		b, err := json.Marshal(entry.Location)
		if err != nil {
			return sqlite.Statement{}, err
		}
		locArg = string(b)
	}
	var weatherArg any
	if entry.Weather != nil {
		b, err := json.Marshal(entry.Weather)
		if err != nil {
			return sqlite.Statement{}, err
		}
		weatherArg = string(b)
	}
	var deletedArg any
	if entry.DeletedAt != nil {
		deletedArg = formatUTC(*entry.DeletedAt)
	}

	if forceSyncVersion != nil {
		return sqlite.Statement{
			SQL: `INSERT INTO diary_entries
				(id, title, markdown, diaryDateKey, createdAt, updatedAt, deletedAt, tags, attachments, linkCards, location, weather, syncVersion)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(id) DO UPDATE SET
					title=excluded.title,
					markdown=excluded.markdown,
					diaryDateKey=excluded.diaryDateKey,
					createdAt=excluded.createdAt,
					updatedAt=excluded.updatedAt,
					deletedAt=excluded.deletedAt,
					tags=excluded.tags,
					attachments=excluded.attachments,
					linkCards=excluded.linkCards,
					location=excluded.location,
					weather=excluded.weather,
					syncVersion=excluded.syncVersion`,
			Args: []any{
				entry.ID, entry.Title, entry.Markdown, entry.DiaryDateKey.String(),
				formatUTC(entry.CreatedAt), formatUTC(entry.UpdatedAt), deletedArg,
				string(tagsJSON), string(attsJSON), string(cardsJSON), locArg, weatherArg, *forceSyncVersion,
			},
		}, nil
	}
	return sqlite.Statement{
		SQL: `INSERT INTO diary_entries
			(id, title, markdown, diaryDateKey, createdAt, updatedAt, deletedAt, tags, attachments, linkCards, location, weather)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				title=excluded.title,
				markdown=excluded.markdown,
				diaryDateKey=excluded.diaryDateKey,
				createdAt=excluded.createdAt,
				updatedAt=excluded.updatedAt,
				deletedAt=excluded.deletedAt,
				tags=excluded.tags,
				attachments=excluded.attachments,
				linkCards=excluded.linkCards,
				location=excluded.location,
				weather=excluded.weather`,
		Args: []any{
			entry.ID, entry.Title, entry.Markdown, entry.DiaryDateKey.String(),
			formatUTC(entry.CreatedAt), formatUTC(entry.UpdatedAt), deletedArg,
			string(tagsJSON), string(attsJSON), string(cardsJSON), locArg, weatherArg,
		},
	}, nil
}

func buildApplyChangeStatements(change Change, syncedAt time.Time) ([]sqlite.Statement, *DiaryEntryPayload, error) {
	entityID := strings.ToLower(change.EntityID)
	switch change.Operation {
	case OperationUpsert:
		payload, err := DecodeDiaryEntryPayload(change.Payload)
		if err != nil {
			return nil, nil, err
		}
		entry, err := EntryFromPayload(entityID, payload)
		if err != nil {
			return nil, nil, err
		}
		ver := change.Version
		stmt, err := buildUpsertStatement(entry, &ver)
		if err != nil {
			return nil, nil, err
		}
		return []sqlite.Statement{stmt}, &payload, nil
	case OperationDelete:
		ts := formatUTC(syncedAt)
		return []sqlite.Statement{
			{
				SQL:  `UPDATE diary_entries SET deletedAt = ?, updatedAt = ?, syncVersion = ? WHERE id = ?`,
				Args: []any{ts, ts, change.Version, entityID},
			},
		}, nil, nil
	default:
		return nil, nil, fmt.Errorf("%w: operation %q", ErrMalformed, change.Operation)
	}
}

func scanEntry(row []any) (Entry, error) {
	if len(row) < 13 {
		return Entry{}, ErrUnreadableDB
	}
	id, _ := row[0].(string)
	title, _ := row[1].(string)
	markdown, _ := row[2].(string)
	dateRaw, _ := row[3].(string)
	createdRaw, _ := row[4].(string)
	updatedRaw, _ := row[5].(string)
	tagsRaw, _ := row[7].(string)
	attsRaw, _ := row[8].(string)
	cardsRaw, _ := row[9].(string)
	syncVer, _ := row[12].(int64)

	key, err := ParseDateKey(dateRaw)
	if err != nil {
		return Entry{}, ErrUnreadableDB
	}
	createdAt, err := parseUTC(createdRaw)
	if err != nil {
		return Entry{}, ErrUnreadableDB
	}
	updatedAt, err := parseUTC(updatedRaw)
	if err != nil {
		return Entry{}, ErrUnreadableDB
	}
	var deletedAt *time.Time
	if raw, ok := row[6].(string); ok && raw != "" {
		t, err := parseUTC(raw)
		if err != nil {
			return Entry{}, ErrUnreadableDB
		}
		deletedAt = &t
	}
	var tags []string
	if err := json.Unmarshal([]byte(tagsRaw), &tags); err != nil {
		return Entry{}, ErrUnreadableDB
	}
	var atts []Attachment
	if err := json.Unmarshal([]byte(attsRaw), &atts); err != nil {
		return Entry{}, ErrUnreadableDB
	}
	safeAtts := make([]Attachment, 0, len(atts))
	for _, a := range atts {
		if IsSafeAttachmentFilename(a.Filename) {
			safeAtts = append(safeAtts, a)
		}
	}
	var cards []LinkCard
	if err := json.Unmarshal([]byte(cardsRaw), &cards); err != nil {
		return Entry{}, ErrUnreadableDB
	}
	safeCards := make([]LinkCard, 0, len(cards))
	for _, c := range cards {
		if clean, ok := ValidateWebURL(c.URL); ok {
			c.URL = clean
			safeCards = append(safeCards, c)
		}
	}
	var loc *LocationSnapshot
	if raw, ok := row[10].(string); ok && raw != "" {
		var decoded LocationSnapshot
		if json.Unmarshal([]byte(raw), &decoded) == nil && strings.TrimSpace(decoded.Place) != "" {
			loc = &decoded
		}
	}
	var weather *WeatherSnapshot
	if raw, ok := row[11].(string); ok && raw != "" {
		var decoded WeatherSnapshot
		if json.Unmarshal([]byte(raw), &decoded) == nil && strings.TrimSpace(decoded.Description) != "" {
			weather = &decoded
		}
	}
	return Entry{
		ID:           strings.ToLower(id),
		Title:        title,
		Markdown:     markdown,
		DiaryDateKey: key,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
		DeletedAt:    deletedAt,
		Tags:         nonNilStrings(tags),
		Attachments:  safeAtts,
		LinkCards:    safeCards,
		Location:     loc,
		Weather:      weather,
		SyncVersion:  syncVer,
	}, nil
}

func decodeOutboxRows(rows sqlite.Rows) ([]OutboxRow, error) {
	out := make([]OutboxRow, 0, len(rows.Values))
	for _, r := range rows.Values {
		if len(r) < 9 {
			return nil, ErrUnreadableDB
		}
		seq, ok1 := r[0].(int64)
		mutID, _ := r[1].(string)
		entityID, _ := r[2].(string)
		op, _ := r[3].(string)
		createdRaw, _ := r[4].(string)
		baseVer, ok2 := r[5].(int64)
		createdAt, err := parseUTC(createdRaw)
		if !ok1 || !ok2 || !IsID(strings.ToLower(mutID)) || !IsID(strings.ToLower(entityID)) || err != nil {
			return nil, ErrUnreadableDB
		}
		var payload *string
		if s, ok := r[6].(string); ok {
			payload = &s
		}
		var sentAt *time.Time
		if s, ok := r[7].(string); ok && s != "" {
			t, err := parseUTC(s)
			if err != nil {
				return nil, ErrUnreadableDB
			}
			sentAt = &t
		}
		var conflictAt *time.Time
		if s, ok := r[8].(string); ok && s != "" {
			t, err := parseUTC(s)
			if err != nil {
				return nil, ErrUnreadableDB
			}
			conflictAt = &t
		}
		out = append(out, OutboxRow{
			Seq:         seq,
			MutationID:  strings.ToLower(mutID),
			EntityID:    strings.ToLower(entityID),
			Operation:   op,
			CreatedAt:   createdAt,
			BaseVersion: baseVer,
			Payload:     payload,
			SentAt:      sentAt,
			ConflictAt:  conflictAt,
		})
	}
	return out, nil
}

func encodeOutboxPayload(entry Entry) (string, error) {
	payload, err := EntryToPayload(entry)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func formatUTC(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func parseUTC(raw string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nonNilAttachments(a []Attachment) []Attachment {
	if a == nil {
		return []Attachment{}
	}
	return a
}

func nonNilLinkCards(c []LinkCard) []LinkCard {
	if c == nil {
		return []LinkCard{}
	}
	return c
}
