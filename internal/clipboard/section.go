// This file is the clipboard tool's side of settings.json.
//
// The store that owns that file knows nothing about what is in it: it hands
// every feature the whole of the file's settings object and takes back the
// keys each feature says it owns. This file is the clipboard's half of that —
// which keys are the clipboard's, and how this feature's own Settings type is
// written down.
//
// Keeping it here rather than in internal/settings is what makes the settings
// belong to the feature: a new switch is a change to this package and to
// nothing else, and no other feature can reach the clipboard's keys because it
// never has the type they decode into.
package clipboard

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// The keys this feature owns in settings.json. They are the names
// docs/database.md §8 documents and a person may edit, so they are spelled
// once, here, and never derived from a Go field name.
const (
	keyRegionVisible = "clipboardRegionVisible"
	keyMonitoring    = "clipboardMonitoring"
	keyPollInterval  = "clipboardPollIntervalMs"
	keyHistorySize   = "clipboardHistorySize"
)

// sectionName is what errors call this feature's settings.
const sectionName = "clipboard"

// StoreSettings is the part of the settings store this feature needs.
//
// It is declared here, and not taken from internal/settings, so that this
// package does not depend on the storage implementation: the assembly injects
// one, and a test injects its own. The shape is deliberately the smallest
// thing that works — load everything, change one section through a function
// that reads the file again first.
type StoreSettings interface {
	// Load reads the file into every registered section.
	Load(ctx context.Context) error
	// Update reads the file, runs change, and writes it back, all without
	// letting go of the file. change reads the file again first, which is
	// what keeps one feature's change from putting back another's stale
	// values.
	Update(ctx context.Context, section string, change func() error) error
}

// Section is this feature's settings as the store sees them: a name, a way to
// read the keys it owns out of the file, and a way to hand them back.
//
// It holds the current settings, so the service reads its configuration from
// here after the store has loaded the file, and writes it here before asking
// the store to save.
type Section struct {
	current Settings
}

// NewSection returns an empty section, holding this feature's defaults.
func NewSection() *Section {
	return &Section{current: Settings{}.Normalize()}
}

// Name is what errors call this section.
func (s *Section) Name() string { return sectionName }

// Current is the settings as they stand.
func (s *Section) Current() Settings { return s.current }

// Set replaces the settings.
func (s *Section) Set(settings Settings) { s.current = settings.Normalize() }

// Decode reads the clipboard's keys out of values, which holds every feature's
// keys.
//
// A key that is not there leaves the setting as it is: the file may have been
// written before the key existed, and reading an absent key as a zero would
// turn a switch off for a user who never touched it.
func (s *Section) Decode(values map[string]json.RawMessage) error {
	settings := s.current
	if err := into(values, keyRegionVisible, &settings.RegionVisible); err != nil {
		return err
	}
	if err := into(values, keyMonitoring, &settings.Monitoring); err != nil {
		return err
	}
	var intervalMs int64
	if err := into(values, keyPollInterval, &intervalMs); err != nil {
		return err
	}
	if intervalMs != 0 {
		settings.PollInterval = time.Duration(intervalMs) * time.Millisecond
	}
	if err := into(values, keyHistorySize, &settings.HistorySize); err != nil {
		return err
	}
	s.current = settings.Normalize()
	return nil
}

// Encode returns the keys the clipboard owns, ready for the store to merge
// into the file.
func (s *Section) Encode() (map[string]any, error) {
	settings := s.current.Normalize()
	return map[string]any{
		keyRegionVisible: settings.RegionVisible,
		keyMonitoring:    settings.Monitoring,
		keyPollInterval:  settings.Interval().Milliseconds(),
		keyHistorySize:   settings.KeepsHistory(),
	}, nil
}

// into decodes one key of a settings object, leaving out alone when the file
// does not name it.
//
// It is the same rule the store documents, kept here so this feature follows
// it without reaching into the storage package.
func into(values map[string]json.RawMessage, key string, out any) error {
	raw, present := values[key]
	if !present {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	return nil
}
