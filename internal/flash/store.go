// Package flash stores text flashes in the same JSON document Apple writes.
//
// The file stays version 1. Apple's decoder ignores a source it has not been
// taught, then its next save drops unknown keys, so Apple must know source
// before the two sides write one file. Times are whole UTC seconds: Apple's
// ISO-8601 decoder rejects a fractional second. Identifiers are kept in the
// case they were stored in; comparison is case-insensitive, rewriting case
// would make one note look like two.
package flash

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"boku/internal/platform/atomicfile"
)

const (
	// FileName is the document, beside the application's other user files.
	FileName = "flashes.json"
	// SourceApple is a flash recorded on Apple. A missing source means the same
	// thing: files written before the field existed are Apple's.
	SourceApple = "apple"
	// SourceDesktop is a flash recorded on this desktop.
	SourceDesktop = "desktop"

	documentVersion = 1
)

var (
	// ErrUnreadable means the file is there and must not be overwritten.
	ErrUnreadable = errors.New("unreadable flash file")
	// ErrEmpty means the text is blank and there is no audio to keep.
	ErrEmpty = errors.New("empty flash")
	// ErrMissing means the note being edited is not an active entry.
	ErrMissing = errors.New("missing flash")
)

// Entry is one flash. AudioFileName is the file name only, never a path.
type Entry struct {
	ID            string
	Text          string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     *time.Time
	AudioFileName string
	Source        string
}

// Store is the flashes document. A failed read refuses every later write.
type Store struct {
	path string
}

// NewStore reads and writes path.
func NewStore(path string) *Store { return &Store{path: path} }

// List returns active flashes, newest first.
func (s *Store) List() ([]Entry, error) {
	entries, err := s.read()
	if err != nil {
		return nil, err
	}
	visible := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if entry.DeletedAt == nil {
			visible = append(visible, entry)
		}
	}
	for i, j := 0, len(visible)-1; i < j; i, j = i+1, j-1 {
		visible[i], visible[j] = visible[j], visible[i]
	}
	// read() keeps file order, which is oldest first. Reverse is newest only
	// when the file itself is in creation order. Sort by the timestamp so a
	// hand-edited file still lists newest first.
	for i := 1; i < len(visible); i++ {
		item := visible[i]
		j := i
		for j > 0 && visible[j-1].CreatedAt.Before(item.CreatedAt) {
			visible[j] = visible[j-1]
			j--
		}
		visible[j] = item
	}
	return visible, nil
}

// SaveText creates a desktop flash, or replaces the text of an existing one.
// An existing source and audio file name are left as they are.
func (s *Store) SaveText(text, id string, now time.Time) (Entry, error) {
	text = strings.TrimSpace(text)
	entries, err := s.read()
	if err != nil {
		return Entry{}, err
	}
	now = now.UTC().Truncate(time.Second)
	if id == "" {
		if text == "" {
			return Entry{}, ErrEmpty
		}
		created, err := newID(now)
		if err != nil {
			return Entry{}, err
		}
		saved := Entry{ID: created, Text: text, CreatedAt: now, UpdatedAt: now, Source: SourceDesktop}
		entries = append(entries, saved)
		if err := s.write(entries); err != nil {
			return Entry{}, err
		}
		return saved, nil
	}
	index := findActive(entries, id)
	if index < 0 {
		return Entry{}, ErrMissing
	}
	if text == "" && entries[index].AudioFileName == "" {
		return Entry{}, ErrEmpty
	}
	entries[index].Text = text
	entries[index].UpdatedAt = now
	saved := entries[index]
	if err := s.write(entries); err != nil {
		return Entry{}, err
	}
	return saved, nil
}

// Delete marks an active flash deleted. The row stays in the file.
func (s *Store) Delete(id string, now time.Time) error {
	entries, err := s.read()
	if err != nil {
		return err
	}
	index := findActive(entries, id)
	if index < 0 {
		return ErrMissing
	}
	now = now.UTC().Truncate(time.Second)
	entries[index].DeletedAt = &now
	entries[index].UpdatedAt = now
	return s.write(entries)
}

func findActive(entries []Entry, id string) int {
	for i, entry := range entries {
		if entry.DeletedAt == nil && strings.EqualFold(entry.ID, id) {
			return i
		}
	}
	return -1
}

type document struct {
	Version int         `json:"version"`
	Entries []fileEntry `json:"entries"`
}

type fileEntry struct {
	ID            string   `json:"id"`
	Text          string   `json:"text"`
	CreatedAt     utcTime  `json:"createdAt"`
	UpdatedAt     utcTime  `json:"updatedAt"`
	DeletedAt     *utcTime `json:"deletedAt,omitempty"`
	AudioFileName string   `json:"audioFileName,omitempty"`
	Source        string   `json:"source,omitempty"`
}

type utcTime time.Time

func (t utcTime) MarshalJSON() ([]byte, error) {
	stamp := time.Time(t).UTC().Truncate(time.Second).Format(time.RFC3339)
	return []byte(`"` + stamp + `"`), nil
}

func (t *utcTime) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return fmt.Errorf("null time")
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return err
	}
	*t = utcTime(parsed.UTC())
	return nil
}

func (s *Store) read() ([]Entry, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	if doc.Version != documentVersion {
		return nil, fmt.Errorf("%w: version %d", ErrUnreadable, doc.Version)
	}
	seen := make(map[string]struct{}, len(doc.Entries))
	entries := make([]Entry, 0, len(doc.Entries))
	for _, raw := range doc.Entries {
		entry, err := raw.entry()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
		}
		key := strings.ToLower(entry.ID)
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("%w: duplicate id", ErrUnreadable)
		}
		seen[key] = struct{}{}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (raw fileEntry) entry() (Entry, error) {
	if !validID(raw.ID) {
		return Entry{}, fmt.Errorf("id")
	}
	text := strings.TrimSpace(raw.Text)
	if raw.AudioFileName != "" && !validAudioName(raw.AudioFileName) {
		return Entry{}, fmt.Errorf("audio")
	}
	if text == "" && raw.AudioFileName == "" {
		return Entry{}, fmt.Errorf("empty")
	}
	created := time.Time(raw.CreatedAt)
	updated := time.Time(raw.UpdatedAt)
	if created.IsZero() || updated.IsZero() {
		return Entry{}, fmt.Errorf("time")
	}
	entry := Entry{
		ID:            raw.ID,
		Text:          text,
		CreatedAt:     created,
		UpdatedAt:     updated,
		AudioFileName: raw.AudioFileName,
		Source:        raw.Source,
	}
	if raw.DeletedAt != nil {
		deleted := time.Time(*raw.DeletedAt)
		if deleted.IsZero() {
			return Entry{}, fmt.Errorf("deleted")
		}
		entry.DeletedAt = &deleted
	}
	return entry, nil
}

func (s *Store) write(entries []Entry) error {
	raw := make([]fileEntry, 0, len(entries))
	for _, entry := range entries {
		item := fileEntry{
			ID:            entry.ID,
			Text:          entry.Text,
			CreatedAt:     utcTime(entry.CreatedAt),
			UpdatedAt:     utcTime(entry.UpdatedAt),
			AudioFileName: entry.AudioFileName,
			Source:        entry.Source,
		}
		if entry.DeletedAt != nil {
			deleted := utcTime(*entry.DeletedAt)
			item.DeletedAt = &deleted
		}
		raw = append(raw, item)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(document{Version: documentVersion, Entries: raw}); err != nil {
		return err
	}
	if err := atomicfile.Write(s.path, buf.Bytes()); err != nil {
		return fmt.Errorf("write flashes: %w", err)
	}
	return nil
}

func validID(id string) bool {
	return uuidPattern(id)
}

func validAudioName(name string) bool {
	stem, ok := strings.CutSuffix(name, ".m4a")
	return ok && uuidPattern(stem)
}

func uuidPattern(value string) bool {
	parts := strings.Split(value, "-")
	if len(parts) != 5 {
		return false
	}
	widths := []int{8, 4, 4, 4, 12}
	for i, part := range parts {
		if len(part) != widths[i] || strings.Trim(part, "0123456789abcdefABCDEF") != "" {
			return false
		}
	}
	return true
}
