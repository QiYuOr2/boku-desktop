// Package flash 把文字闪念存进 Apple 所写的同一份 JSON 文档。
//
// 文件保持 version 1。Apple 的解码器会忽略它尚不认识的 source，而它下次保存会丢掉未知键，
// 因此两端共写同一份文件前，Apple 必须先认识 source。时间取整为 UTC 整秒：Apple 的
// ISO-8601 解码器拒绝小数秒。ID 按存入时的大小写保留；比较时忽略大小写，改动大小写
// 会让一条笔记看起来像两条。
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
	// FileName 是文档文件名，与应用的其它用户文件同目录。
	FileName = "flashes.json"
	// SourceApple 表示在 Apple 端记录的闪念。缺失 source 含义相同：字段出现之前写下的
	// 文件都是 Apple 的。
	SourceApple = "apple"
	// SourceDesktop 表示在本桌面端记录的闪念。
	SourceDesktop = "desktop"

	documentVersion = 1
)

var (
	// ErrUnreadable 表示文件存在，且不得被覆盖。
	ErrUnreadable = errors.New("unreadable flash file")
	// ErrEmpty 表示文字为空且没有音频可保留。
	ErrEmpty = errors.New("empty flash")
	// ErrMissing 表示要编辑的笔记不是活动条目。
	ErrMissing = errors.New("missing flash")
)

// Entry 是一条闪念。AudioFileName 仅为文件名，绝不是路径。
type Entry struct {
	ID            string
	Text          string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     *time.Time
	AudioFileName string
	Source        string
}

// Store 是闪念文档。读取失败后拒绝后续所有写入。
type Store struct {
	path string
}

func NewStore(path string) *Store { return &Store{path: path} }

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
	// read() 保持文件顺序，即最旧在前。反转后只有在文件本身按创建顺序时才是最新在前，
	// 因此按时间戳排序，使手工编辑过的文件仍以最新在前列出。
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

// SaveText 新建桌面闪念，或替换已有条目的文字；已有的 source 与音频文件名保持不动。
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

// Delete 把活动闪念标记为已删除，条目仍保留在文件中。
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
