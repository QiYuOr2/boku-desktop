// Package settings stores the application's own configuration: the switches
// that are about Boku itself — the clipboard options, which optional panels
// are on, and the translation tool's settings.
//
// One file, under the user data directory:
//
//	settings.json    the switches that are about Boku itself
//
// It holds one domain.ToolStore and nothing else. The clipboard history used
// to live here too, in a second file, because the two were written by the same
// object; it is the clipboard feature's file and now lives with it
// (internal/clipboard/store.go).
//
// The file is written through internal/platform/atomicfile, so a crash
// mid-write leaves the previous file rather than half of a new one. A settings
// file that cannot be read is an error: the window would otherwise show
// defaults the user never chose and then write them back.
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"boku/internal/domain"
	"boku/internal/kit"
	"boku/internal/platform/atomicfile"
)

// fileVersion is the shape this build reads and writes. A file from another
// version is refused rather than partly understood.
const fileVersion = 1

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

// Store is the application's settings as a file. It is a domain.ToolStore,
// and it is safe to use from several goroutines.
type Store struct {
	Path string

	mu       sync.Mutex
	settings domain.ToolSettings
	loaded   bool
}

// New returns the store over a path. A missing file is not an error: the
// store starts from the defaults and writes on the first change.
func New(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("%w: the settings store has no path", domain.ErrInvalidArgument)
	}
	return &Store{Path: path}, nil
}

// Load reads the settings, and returns the defaults when there is no file.
//
// A caller that means to change something should use Update instead: reading
// here and writing with Save is two steps, and a change made between them is
// lost.
func (s *Store) Load(ctx context.Context) (domain.ToolSettings, error) {
	if err := kit.ContextErr(ctx); err != nil {
		return domain.ToolSettings{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}

// Update reads the settings, lets change modify them, and writes the result —
// all under one lock, so that a change made while another one is in flight
// cannot be lost.
//
// This is the only way the application changes a setting it already has. A
// caller that reads with Load, changes what it read and writes it back with
// Save is doing the same thing in three steps, and two such callers overwrite
// each other: both read the file as it was, both write back what they read
// plus their own change, and the second write undoes the first. Keeping the
// three steps in one critical section is what makes that impossible, and it is
// why Save is not enough on its own for a change to existing settings.
//
// A missing file is not an error: change is handed the defaults.
func (s *Store) Update(ctx context.Context, change func(*domain.ToolSettings) error) error {
	if err := kit.ContextErr(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	settings, err := s.read()
	if err != nil {
		return err
	}
	if err := change(&settings); err != nil {
		return err
	}
	return s.write(settings.Normalize())
}

// read returns the settings on disk, or the defaults when there is no file.
//
// It is the body of Load without the locking: the lock is taken by whoever
// calls it, so that a read and the write that follows it are one step.
func (s *Store) read() (domain.ToolSettings, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return defaultTools(), nil
	}
	if err != nil {
		return domain.ToolSettings{}, fmt.Errorf("settings: read %s: %w", s.Path, err)
	}
	var doc toolDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return domain.ToolSettings{}, fmt.Errorf("settings: %s is not a readable settings file: %w", s.Path, err)
	}
	if doc.Version != fileVersion {
		return domain.ToolSettings{}, fmt.Errorf("settings: %s is version %d, this build reads version %d",
			s.Path, doc.Version, fileVersion)
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
	return domain.ToolSettings{
		Clipboard: domain.ClipboardSettings{
			RegionVisible: doc.Tools.ClipboardRegionVisible,
			Monitoring:    doc.Tools.ClipboardMonitoring,
			PollInterval:  time.Duration(doc.Tools.ClipboardPollIntervalMs) * time.Millisecond,
			HistorySize:   doc.Tools.ClipboardHistorySize,
		},
		Panels:      domain.PanelSettings{Choices: panelChoices(doc.Tools.Panels)},
		Translation: translation,
	}.Normalize(), nil
}

// Save writes the settings whole.
//
// A caller that means to change one thing should use Update instead. Save
// replaces the file with what it was given, so a value this build read earlier
// and put back here is a value that overwrites whatever was written in
// between.
func (s *Store) Save(ctx context.Context, settings domain.ToolSettings) error {
	if err := kit.ContextErr(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write(settings)
}

// write puts the settings on disk. The caller holds the lock: a write is the
// second half of a read-modify-write, and is never done on its own by a caller
// that meant to change something.
func (s *Store) write(settings domain.ToolSettings) error {
	settings = settings.Normalize()
	translation := settings.Translation
	enabled, furigana, gloss := translation.Enabled, translation.Furigana, translation.LoanwordGloss
	doc := toolDocument{
		Version: fileVersion,
		Tools: tool{
			ClipboardRegionVisible:  settings.Clipboard.RegionVisible,
			ClipboardMonitoring:     settings.Clipboard.Monitoring,
			ClipboardPollIntervalMs: settings.Clipboard.Interval().Milliseconds(),
			ClipboardHistorySize:    settings.Clipboard.KeepsHistory(),
			Panels:                  panelChoices(settings.Panels.Choices),

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
		return fmt.Errorf("settings: encode %s: %w", s.Path, err)
	}
	if err := atomicfile.Write(s.Path, append(data, '\n')); err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	s.settings, s.loaded = settings, true
	return nil
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

// ensure the port is satisfied where it is defined, not at the call site.
var _ domain.ToolStore = (*Store)(nil)
