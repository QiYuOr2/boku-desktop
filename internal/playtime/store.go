package playtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"boku/internal/platform/sqlite"
)

const (
	dbFile        = "playtime.sqlite"
	legacyFile    = "playtime.json"
	schemaVersion = 2
)

// Store is playtime.sqlite. It is the session list and nothing else.
//
// A playtime.json left from the previous format is imported once, then
// renamed to playtime.json.imported. The bytes stay on disk.
type Store struct {
	db       *sqlite.DB
	jsonPath string
	writable bool
	openErr  error
}

func NewStore(dir string) *Store {
	store := &Store{jsonPath: filepath.Join(dir, legacyFile), writable: true}
	db, err := sqlite.Open(context.Background(), filepath.Join(dir, dbFile))
	if err != nil {
		store.writable = false
		store.openErr = fmt.Errorf("open playtime: %w", err)
		return store
	}
	store.db = db
	if err := store.migrate(); err != nil {
		store.writable = false
		store.openErr = err
	}
	return store
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

type fileSession struct {
	ID         string    `json:"id"`
	AppID      string    `json:"appId"`
	Name       string    `json:"name,omitempty"`
	StartedAt  time.Time `json:"startedAt"`
	EndedAt    time.Time `json:"endedAt,omitempty"`
	LastSeenAt time.Time `json:"lastSeenAt,omitempty"`
}

type fileDocument struct {
	Version  int           `json:"version"`
	Sessions []fileSession `json:"sessions"`
}

// Load reads the session list. A missing database is created.
//
// A database newer than this build, or a legacy JSON file that cannot be
// understood while the database is still empty, is left in place and is
// not written over.
func (s *Store) Load() ([]Session, error) {
	if s == nil || s.openErr != nil {
		if s != nil {
			s.writable = false
		}
		if s == nil {
			return nil, nil
		}
		return nil, s.openErr
	}
	if err := s.importJSON(); err != nil {
		s.writable = false
		return nil, err
	}
	return s.load()
}

// Save replaces the session list in one transaction.
func (s *Store) Save(sessions []Session) error {
	if s == nil {
		return nil
	}
	if !s.writable || s.db == nil {
		return fmt.Errorf("write playtime: the existing file was not readable")
	}
	statements := make([]sqlite.Statement, 0, len(sessions)+1)
	statements = append(statements, sqlite.Statement{SQL: `DELETE FROM sessions`})
	for _, session := range sessions {
		if session.ID == "" || session.AppID == "" || session.StartedAt.IsZero() {
			return fmt.Errorf("write playtime: a session is missing its id, game or start")
		}
		statements = append(statements, sqlite.Statement{
			SQL: `INSERT INTO sessions (id, app_id, name, started_at, ended_at, last_seen_at, devices) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			Args: []any{
				session.ID,
				session.AppID,
				session.Name,
				formatTime(session.StartedAt),
				nullableTime(session.EndedAt),
				nullableTime(session.LastSeenAt),
				session.Devices,
			},
		})
	}
	if _, err := s.db.Transaction(context.Background(), statements); err != nil {
		return fmt.Errorf("write playtime: %w", err)
	}
	return nil
}

func (s *Store) migrate() error {
	current, err := s.schemaVersion()
	if err != nil {
		return err
	}
	if current > schemaVersion {
		return fmt.Errorf("decode playtime: unsupported version %d", current)
	}
	if current < 1 {
		if err := s.apply(1, []sqlite.Statement{
			{SQL: `CREATE TABLE schema_migrations (
				version INTEGER PRIMARY KEY,
				applied_at TEXT NOT NULL
			)`},
			{SQL: `CREATE TABLE sessions (
				id TEXT PRIMARY KEY,
				app_id TEXT NOT NULL,
				name TEXT NOT NULL,
				started_at TEXT NOT NULL,
				ended_at TEXT,
				last_seen_at TEXT
			)`},
			{SQL: `CREATE INDEX sessions_started_at ON sessions (started_at)`},
		}); err != nil {
			return err
		}
	}
	if current < 2 {
		if err := s.apply(2, []sqlite.Statement{
			{SQL: `ALTER TABLE sessions ADD COLUMN devices TEXT NOT NULL DEFAULT ''`},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) schemaVersion() (int64, error) {
	rows, err := s.db.Query(context.Background(), `SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`)
	if err != nil {
		return 0, fmt.Errorf("read playtime schema: %w", err)
	}
	if len(rows.Values) == 0 {
		return 0, nil
	}
	rows, err = s.db.Query(context.Background(), `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return 0, fmt.Errorf("read playtime schema: %w", err)
	}
	var current int64
	for _, row := range rows.Values {
		version, ok := row[0].(int64)
		if !ok || version != current+1 {
			return 0, fmt.Errorf("decode playtime: schema versions are not contiguous")
		}
		current = version
	}
	return current, nil
}

func (s *Store) apply(version int64, statements []sqlite.Statement) error {
	statements = append(statements, sqlite.Statement{
		SQL:  `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		Args: []any{version, time.Now().UTC().Format(time.RFC3339Nano)},
	})
	if _, err := s.db.Transaction(context.Background(), statements); err != nil {
		return fmt.Errorf("migrate playtime to %d: %w", version, err)
	}
	return nil
}

func (s *Store) importJSON() error {
	data, err := os.ReadFile(s.jsonPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read playtime: %w", err)
	}
	sessions, err := decodeSessions(data)
	if err != nil {
		count, countErr := s.count()
		if countErr != nil {
			return countErr
		}
		if count > 0 {
			slog.Warn("playtime.json could not be read; the database already has sessions", "err", err)
			return nil
		}
		return err
	}
	count, err := s.count()
	if err != nil {
		return err
	}
	if count > 0 {
		slog.Warn("playtime.json was left in place because the database already has sessions")
		return nil
	}
	if err := s.Save(sessions); err != nil {
		return err
	}
	imported := s.jsonPath + ".imported"
	if _, err := os.Stat(imported); err == nil {
		slog.Warn("playtime.json was imported; playtime.json.imported already exists")
		return nil
	}
	if err := os.Rename(s.jsonPath, imported); err != nil {
		slog.Warn("playtime.json was imported but could not be renamed", "err", err)
	}
	return nil
}

func (s *Store) load() ([]Session, error) {
	rows, err := s.db.Query(context.Background(), `SELECT id, app_id, name, started_at, ended_at, last_seen_at, devices FROM sessions ORDER BY started_at, id`)
	if err != nil {
		return nil, fmt.Errorf("read playtime: %w", err)
	}
	sessions := make([]Session, 0, len(rows.Values))
	for _, row := range rows.Values {
		session, err := sessionFromRow(row)
		if err != nil {
			s.writable = false
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, nil
}

func (s *Store) count() (int, error) {
	rows, err := s.db.Query(context.Background(), `SELECT COUNT(*) FROM sessions`)
	if err != nil {
		return 0, fmt.Errorf("read playtime: %w", err)
	}
	if len(rows.Values) != 1 {
		return 0, fmt.Errorf("read playtime: session count missing")
	}
	count, ok := rows.Values[0][0].(int64)
	if !ok {
		return 0, fmt.Errorf("read playtime: session count missing")
	}
	return int(count), nil
}

func sessionFromRow(row []any) (Session, error) {
	if len(row) != 7 {
		return Session{}, fmt.Errorf("decode playtime: a session is missing its id, game or start")
	}
	id, _ := row[0].(string)
	appID, _ := row[1].(string)
	name, _ := row[2].(string)
	started, err := parseTime(row[3])
	if err != nil || id == "" || appID == "" || started.IsZero() {
		return Session{}, fmt.Errorf("decode playtime: a session is missing its id, game or start")
	}
	ended, err := parseOptionalTime(row[4])
	if err != nil {
		return Session{}, fmt.Errorf("decode playtime: a session end is not a time")
	}
	seen, err := parseOptionalTime(row[5])
	if err != nil {
		return Session{}, fmt.Errorf("decode playtime: a session look is not a time")
	}
	devices, _ := row[6].(string)
	return Session{ID: id, AppID: appID, Name: name, StartedAt: started, EndedAt: ended, LastSeenAt: seen, Devices: devices}, nil
}

func decodeSessions(data []byte) ([]Session, error) {
	var saved fileDocument
	if err := json.Unmarshal(data, &saved); err != nil {
		return nil, fmt.Errorf("decode playtime: %w", err)
	}
	if saved.Version != 1 {
		return nil, fmt.Errorf("decode playtime: unsupported version %d", saved.Version)
	}
	sessions := make([]Session, 0, len(saved.Sessions))
	for _, item := range saved.Sessions {
		if item.ID == "" || item.AppID == "" || item.StartedAt.IsZero() {
			return nil, fmt.Errorf("decode playtime: a session is missing its id, game or start")
		}
		sessions = append(sessions, Session{
			ID:         item.ID,
			AppID:      item.AppID,
			Name:       item.Name,
			StartedAt:  item.StartedAt.UTC(),
			EndedAt:    utcOrZero(item.EndedAt),
			LastSeenAt: utcOrZero(item.LastSeenAt),
		})
	}
	return sessions, nil
}

func formatTime(at time.Time) string {
	return at.UTC().Format(time.RFC3339Nano)
}

func nullableTime(at time.Time) any {
	if at.IsZero() {
		return nil
	}
	return formatTime(at)
}

func parseTime(value any) (time.Time, error) {
	text, ok := value.(string)
	if !ok || text == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

func parseOptionalTime(value any) (time.Time, error) {
	if value == nil {
		return time.Time{}, nil
	}
	return parseTime(value)
}

func utcOrZero(at time.Time) time.Time {
	if at.IsZero() {
		return time.Time{}
	}
	return at.UTC()
}
