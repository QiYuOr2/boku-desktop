// Package settings stores the application's own configuration and the
// clipboard history.
//
// Both are files of their own under the user data directory:
//
//	settings.json            the switches that are about Boku itself
//	clipboard_history.json   the last few copies, newest first
//
// The history is a file rather than a table because the database does not
// exist yet (Task 002 / 003), and because it is the one thing in the
// application that is sensitive and local: docs/sync.md §2 keeps it out of
// every sync, and a file of its own is the smallest thing that can be
// deleted. Task 016 moves it to the clipboard_entries table of
// docs/database.md §3; the shape of an entry does not change with the move.
//
// Every write goes through a temporary file and a rename, so a crash
// mid-write leaves the previous file rather than half of a new one. A
// settings file that cannot be read is an error; a history file that cannot
// be read is not — see ClipboardStore in the use case.
package settings

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
)

// fileVersion is the shape this build reads and writes. A file from another
// version is refused rather than partly understood.
const fileVersion = 1

const (
	fileMode = 0o600
	dirMode  = 0o700
)

// Settings names the two files. An empty field means that part is not
// stored, which is how a test or an assembly without a user directory runs.
type Settings struct {
	// SettingsPath is settings.json.
	SettingsPath string
	// HistoryPath is clipboard_history.json.
	HistoryPath string

	mu       sync.Mutex
	settings domain.ToolSettings
	entries  []domain.ClipboardEntry
	loaded   bool
}

// toolDocument is what settings.json holds: the switches, under names that
// docs/database.md §8 exports and a person can read.
type toolDocument struct {
	Version int  `json:"version"`
	Tools   tool `json:"tools"`
}

type tool struct {
	ClipboardRegionVisible bool `json:"clipboardRegionVisible"`
	ClipboardMonitoring    bool `json:"clipboardMonitoring"`
	// ClipboardPollIntervalMs is the poll interval in milliseconds, the unit
	// a file a person edits can be written in.
	ClipboardPollIntervalMs int64 `json:"clipboardPollIntervalMs"`
	// ClipboardHistorySize is the record range: how many of the most recent
	// copies are kept, in entries. A file that does not have it (or holds a
	// number that is not one of the ranges) is brought back to the default
	// by Normalize.
	ClipboardHistorySize int `json:"clipboardHistorySize"`
}

// historyDocument is clipboard_history.json.
type historyDocument struct {
	Version int            `json:"version"`
	Entries []historyEntry `json:"entries"`
}

type historyEntry struct {
	ID          string          `json:"id"`
	Content     string          `json:"content"`
	ContentHash string          `json:"contentHash"`
	Kind        domain.ClipKind `json:"kind"`
	Pinned      bool            `json:"pinned"`
	CopiedAt    time.Time       `json:"copiedAt"`
	CreatedAt   time.Time       `json:"createdAt"`
	DeletedAt   *time.Time      `json:"deletedAt"`
	// Display is the one line the interface shows for the entry.
	Display string `json:"display"`
	// Files are the paths the copy holds, when it is a file list: they are
	// what the entry writes back, so that pasting a copy of files pastes
	// the files.
	Files []string `json:"files"`
	// ImageBytes is how large the bitmap the copy held was. The bytes
	// themselves are not stored: the history is a list of what was copied,
	// not an image store.
	ImageBytes int `json:"imageBytes"`
	// Note is what the reading noticed about itself, such as a picture it
	// could not read.
	Note string `json:"note"`
}

// New returns a store over the two paths. A missing file is not an error:
// the store starts from the defaults and writes on the first change.
func New(settingsPath, historyPath string) (*Settings, error) {
	if strings.TrimSpace(settingsPath) == "" && strings.TrimSpace(historyPath) == "" {
		return nil, fmt.Errorf("%w: the settings store has no path", domain.ErrInvalidArgument)
	}
	return &Settings{SettingsPath: settingsPath, HistoryPath: historyPath}, nil
}

// Load reads the settings, and returns the defaults when there is no file.
func (s *Settings) Load(ctx context.Context) (domain.ToolSettings, error) {
	if err := contextErr(ctx); err != nil {
		return domain.ToolSettings{}, err
	}
	if strings.TrimSpace(s.SettingsPath) == "" {
		return domain.ToolSettings{}.Normalize(), nil
	}
	data, err := os.ReadFile(s.SettingsPath)
	if errors.Is(err, os.ErrNotExist) {
		return domain.ToolSettings{}.Normalize(), nil
	}
	if err != nil {
		return domain.ToolSettings{}, fmt.Errorf("settings: read %s: %w", s.SettingsPath, err)
	}
	var doc toolDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return domain.ToolSettings{}, fmt.Errorf("settings: %s is not a readable settings file: %w", s.SettingsPath, err)
	}
	if doc.Version != fileVersion {
		return domain.ToolSettings{}, fmt.Errorf("settings: %s is version %d, this build reads version %d", s.SettingsPath, doc.Version, fileVersion)
	}
	settings := domain.ToolSettings{
		ClipboardRegionVisible: doc.Tools.ClipboardRegionVisible,
		ClipboardMonitoring:    doc.Tools.ClipboardMonitoring,
		ClipboardPollInterval:  time.Duration(doc.Tools.ClipboardPollIntervalMs) * time.Millisecond,
		ClipboardHistorySize:   doc.Tools.ClipboardHistorySize,
	}.Normalize()
	s.mu.Lock()
	s.settings, s.loaded = settings, true
	s.mu.Unlock()
	return settings, nil
}

// Save writes the settings.
//
// The write happens under the lock: two writes to one file, through two
// temporary files and two renames, are exactly what the file system refuses
// to interleave, and the file is a few hundred bytes.
func (s *Settings) Save(ctx context.Context, settings domain.ToolSettings) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(s.SettingsPath) == "" {
		s.settings, s.loaded = settings, true
		return nil // kept in memory only
	}
	doc := toolDocument{
		Version: fileVersion,
		Tools: tool{
			ClipboardRegionVisible:  settings.ClipboardRegionVisible,
			ClipboardMonitoring:     settings.ClipboardMonitoring,
			ClipboardPollIntervalMs: settings.Interval().Milliseconds(),
			ClipboardHistorySize:    settings.HistorySize(),
		},
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("settings: encode %s: %w", s.SettingsPath, err)
	}
	if err := writeFile(s.SettingsPath, append(data, '\n')); err != nil {
		return err
	}
	s.settings, s.loaded = settings, true
	return nil
}

// LoadEntries reads the clipboard history, newest first. A missing file is
// an empty history.
func (s *Settings) LoadEntries(ctx context.Context) ([]domain.ClipboardEntry, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if strings.TrimSpace(s.HistoryPath) == "" {
		return nil, nil
	}
	data, err := os.ReadFile(s.HistoryPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("settings: read %s: %w", s.HistoryPath, err)
	}
	var doc historyDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("settings: %s is not a readable history file: %w", s.HistoryPath, err)
	}
	if doc.Version != fileVersion {
		return nil, fmt.Errorf("settings: %s is version %d, this build reads version %d", s.HistoryPath, doc.Version, fileVersion)
	}
	entries := make([]domain.ClipboardEntry, 0, len(doc.Entries))
	for _, stored := range doc.Entries {
		entries = append(entries, domain.ClipboardEntry{
			ID:          stored.ID,
			Content:     stored.Content,
			ContentHash: stored.ContentHash,
			Kind:        stored.Kind,
			Pinned:      stored.Pinned,
			CopiedAt:    stored.CopiedAt.UTC(),
			CreatedAt:   stored.CreatedAt.UTC(),
			DeletedAt:   stored.DeletedAt,
			Display:     stored.Display,
			Files:       append([]string(nil), stored.Files...),
			ImageBytes:  stored.ImageBytes,
			Note:        stored.Note,
		})
	}
	s.mu.Lock()
	s.entries = entries
	s.mu.Unlock()
	return entries, nil
}

// SaveEntries replaces the clipboard history. Like Save, it writes under the
// lock, so two saves cannot race over one file.
func (s *Settings) SaveEntries(ctx context.Context, entries []domain.ClipboardEntry) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(s.HistoryPath) == "" {
		s.entries = append([]domain.ClipboardEntry(nil), entries...)
		return nil // kept in memory only
	}
	doc := historyDocument{Version: fileVersion, Entries: make([]historyEntry, 0, len(entries))}
	for _, entry := range entries {
		doc.Entries = append(doc.Entries, historyEntry{
			ID:          entry.ID,
			Content:     entry.Content,
			ContentHash: entry.ContentHash,
			Kind:        entry.Kind,
			Pinned:      entry.Pinned,
			CopiedAt:    entry.CopiedAt.UTC(),
			CreatedAt:   entry.CreatedAt.UTC(),
			DeletedAt:   entry.DeletedAt,
			Display:     entry.Display,
			Files:       append([]string(nil), entry.Files...),
			ImageBytes:  entry.ImageBytes,
			Note:        entry.Note,
		})
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("settings: encode %s: %w", s.HistoryPath, err)
	}
	if err := writeFile(s.HistoryPath, append(data, '\n')); err != nil {
		return err
	}
	s.entries = append([]domain.ClipboardEntry(nil), entries...)
	return nil
}

// writeFile writes data through a temporary file in the same directory. A
// reader either sees the previous file or the new one, never half of either.
func writeFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("settings: create %s: %w", dir, err)
	}
	temp, err := os.CreateTemp(dir, ".settings-*.tmp")
	if err != nil {
		return fmt.Errorf("settings: create a temporary file in %s: %w", dir, err)
	}
	name := temp.Name()
	defer func() {
		if name != "" {
			_ = os.Remove(name)
		}
	}()
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("settings: write %s: %w", name, err)
	}
	if err := temp.Chmod(fileMode); err != nil {
		_ = temp.Close()
		return fmt.Errorf("settings: restrict %s: %w", name, err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("settings: flush %s: %w", name, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("settings: close %s: %w", name, err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("settings: move %s into place: %w", name, err)
	}
	name = ""
	return nil
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

// The two interfaces the use case asks for.
var (
	_ domain.ToolStore      = (*Settings)(nil)
	_ domain.ClipboardStore = (*Settings)(nil)
)
