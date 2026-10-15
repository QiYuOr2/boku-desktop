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

// customTranslatorID is the translator whose settings the user fills in
// themselves. It is named here, and not imported from the package that
// implements it, because the file's field names are a storage decision: this
// package must keep reading a file whose translator moved.
const customTranslatorID = "custom"

// defaultTools is what a user who has never saved anything has: the
// translation defaults, which a struct literal would leave switched off —
// the one default this file cannot express by writing nothing.
func defaultTools() domain.ToolSettings {
	return domain.ToolSettings{Translation: domain.TranslationDefaults()}.Normalize()
}

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

	// Panels holds what the user chose about the panels that can be turned
	// off, by panel id. It is only written when there is a choice in it: a
	// panel with no key is on, which is what every panel is until somebody
	// turns one off, and what a file written before the switch existed says.
	Panels map[string]bool `json:"panels,omitempty"`

	// The selection-translation tool. Every field is optional in the file:
	// one this build wrote always has them, and one a person wrote — or one
	// from before the tool existed — gets its defaults from Normalize.
	//
	// A field whose default is ON is a pointer, because a plain bool cannot
	// tell an absent key from a choice: a file written before the key
	// existed decodes to "false", which reads as a switch the user turned
	// off. Nil means the key was absent, and the default applies.
	TranslateEnabled       *bool  `json:"translateEnabled"`
	TranslateAccelerator   string `json:"translateAccelerator"`
	TranslateProvider      string `json:"translateProvider"`
	TranslateFurigana      *bool  `json:"translateFurigana"`
	TranslateLoanwordGloss *bool  `json:"translateLoanwordGloss"`
	TranslatePinned        bool   `json:"translatePinned"`
	// TranslateCloseDelayMs is how long the window waits before closing
	// itself, in milliseconds. Milliseconds rather than Go's duration
	// string: the file is one a person reads and may edit.
	TranslateCloseDelayMs int64 `json:"translateCloseDelayMs"`
	// Translators holds what the user's own services were configured with,
	// by translator id. Nothing sensitive goes here: a key lives in the
	// credential store.
	Translators map[string]translatorSettings `json:"translators,omitempty"`
}

// translatorSettings is one user-configured translator.
type translatorSettings struct {
	BaseURL     string `json:"baseURL,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
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
	// ImageBytes is how large the bitmap the copy held was.
	ImageBytes int `json:"imageBytes"`
	// ImageRef names the picture kept for this entry in the image directory
	// beside this file. The bytes are there rather than here because a
	// screenshot is megabytes and this file is written whole on every copy;
	// a click on the entry loads them and puts the picture back.
	ImageRef string `json:"imageRef,omitempty"`
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
		return defaultTools(), nil
	}
	data, err := os.ReadFile(s.SettingsPath)
	if errors.Is(err, os.ErrNotExist) {
		return defaultTools(), nil
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
	defaults := domain.TranslationDefaults()
	translation := domain.TranslationSettings{
		Enabled:       true,
		Accelerator:   doc.Tools.TranslateAccelerator,
		Provider:      doc.Tools.TranslateProvider,
		Furigana:      defaults.Furigana,
		LoanwordGloss: defaults.LoanwordGloss,
		Pinned:        doc.Tools.TranslatePinned,
		CloseDelay:    time.Duration(doc.Tools.TranslateCloseDelayMs) * time.Millisecond,
		Custom: domain.CustomTranslatorSettings{
			BaseURL:     doc.Tools.Translators[customTranslatorID].BaseURL,
			DisplayName: doc.Tools.Translators[customTranslatorID].DisplayName,
		},
	}
	// An absent key is the default, not a false: the switch is on for a user
	// who has never touched it, and a file from before the tool existed has
	// no key at all — which is every user who has not opened this page since
	// the Japanese annotations were added. Reading those two keys as plain
	// bools left both annotations off, so the app asked for a plain
	// translation and the window drew no readings at all.
	if doc.Tools.TranslateEnabled != nil {
		translation.Enabled = *doc.Tools.TranslateEnabled
	}
	if doc.Tools.TranslateFurigana != nil {
		translation.Furigana = *doc.Tools.TranslateFurigana
	}
	if doc.Tools.TranslateLoanwordGloss != nil {
		translation.LoanwordGloss = *doc.Tools.TranslateLoanwordGloss
	}
	settings := domain.ToolSettings{
		ClipboardRegionVisible: doc.Tools.ClipboardRegionVisible,
		ClipboardMonitoring:    doc.Tools.ClipboardMonitoring,
		ClipboardPollInterval:  time.Duration(doc.Tools.ClipboardPollIntervalMs) * time.Millisecond,
		ClipboardHistorySize:   doc.Tools.ClipboardHistorySize,
		Panels:                 panelChoices(doc.Tools.Panels),
		Translation:            translation,
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
	translation := settings.Translation.Normalize()
	enabled, furigana, gloss := translation.Enabled, translation.Furigana, translation.LoanwordGloss
	doc := toolDocument{
		Version: fileVersion,
		Tools: tool{
			ClipboardRegionVisible:  settings.ClipboardRegionVisible,
			ClipboardMonitoring:     settings.ClipboardMonitoring,
			ClipboardPollIntervalMs: settings.Interval().Milliseconds(),
			ClipboardHistorySize:    settings.HistorySize(),
			Panels:                  panelChoices(settings.Panels),

			TranslateEnabled:       &enabled,
			TranslateAccelerator:   translation.AcceleratorOr(),
			TranslateProvider:      translation.ProviderOr(),
			TranslateFurigana:      &furigana,
			TranslateLoanwordGloss: &gloss,
			TranslatePinned:        translation.Pinned,
			TranslateCloseDelayMs:  translation.CloseDelayOr().Milliseconds(),
			// The map is written even when it holds nothing worth keeping:
			// its presence is what tells a reader that this build wrote the
			// file, and therefore that an absent key inside it is a choice
			// rather than a version's gap.
			Translators: map[string]translatorSettings{
				customTranslatorID: {
					BaseURL:     translation.Custom.BaseURL,
					DisplayName: translation.Custom.DisplayName,
				},
			},
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
			ImageRef:    stored.ImageRef,
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
			ImageRef:    entry.ImageRef,
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

// The pictures of recorded copies live in a directory of their own beside the
// history file, one file per picture, named after the hash of its bytes. They
// are files rather than part of the history file because a screenshot is
// megabytes and the history file is written whole on every copy — and because
// a picture is the one thing in the history that can also just be dropped.
const imageDirName = "clipboard_images"

// SaveImage keeps the bytes of a copied picture under ref.
//
// Saving the same ref twice is not a second copy: the same picture is the same
// file, and the poll may well record it twice over. A store with no history
// path keeps nothing, which is how a test or an assembly without a user
// directory runs.
func (s *Settings) SaveImage(ctx context.Context, ref string, png []byte) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if len(png) == 0 {
		return fmt.Errorf("%w: there are no image bytes to keep", domain.ErrInvalidArgument)
	}
	path, err := s.imagePath(ref)
	if err != nil {
		return err
	}
	if path == "" {
		return nil // kept in memory only, and there is nothing to keep it in
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeFile(path, png)
}

// LoadImage returns the picture kept under ref.
func (s *Settings) LoadImage(ctx context.Context, ref string) ([]byte, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	path, err := s.imagePath(ref)
	if err != nil {
		return nil, err
	}
	if path == "" {
		return nil, fmt.Errorf("%w: no picture was kept", domain.ErrUnsupportedClipboardFormat)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: read the picture: %v", domain.ErrUnsupportedClipboardFormat, err)
	}
	return data, nil
}

// DeleteImage forgets the picture kept under ref. A picture that is already
// gone is not an error: dropping an entry twice is not a failure.
func (s *Settings) DeleteImage(ctx context.Context, ref string) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	path, err := s.imagePath(ref)
	if err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("settings: delete the picture: %w", err)
	}
	return nil
}

// imagePath is where the picture with this ref is kept. An empty path means
// there is no image directory to keep one in.
//
// The ref is a file name, and a file can be edited, so it is checked rather
// than trusted: a reference with a separator or a leading dot in it would name
// a file outside the directory. The colon in "sha256:…" becomes a dash, which
// is the one character a Windows file name cannot hold.
func (s *Settings) imagePath(ref string) (string, error) {
	if strings.TrimSpace(s.HistoryPath) == "" {
		return "", nil
	}
	if ref == "" || len(ref) > 200 || strings.ContainsAny(ref, `/\`) ||
		strings.HasPrefix(ref, ".") {
		return "", fmt.Errorf("%w: %q is not a picture reference", domain.ErrInvalidArgument, ref)
	}
	name := strings.ReplaceAll(ref, ":", "-") + ".png"
	return filepath.Join(filepath.Dir(s.HistoryPath), imageDirName, name), nil
}

// panelChoices copies the panels the user chose about, so that a caller can
// never write into a map the store is holding. A file that holds no choice is
// nil rather than an empty map: both mean the same thing — every panel is on.
func panelChoices(choices map[string]bool) map[string]bool {
	if len(choices) == 0 {
		return nil
	}
	copied := make(map[string]bool, len(choices))
	for id, on := range choices {
		copied[id] = on
	}
	return copied
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
