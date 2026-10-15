// This file is the translation tool's side of settings.json.
//
// The store that owns that file knows nothing about what is in it: it hands
// every feature the whole of the file's settings object and takes back the keys
// each feature says it owns. This file is the translation tool's half of that —
// which keys are its own, and how its own Settings type is written down.
//
// Keeping it here rather than in internal/settings is what makes the settings
// belong to the feature: a new switch is a change to this package and to
// nothing else, and no other feature can reach these keys because it never has
// the type they decode into.
package translate

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// The keys this feature owns in settings.json. They are the names
// §8 of the storage design documents and a person may edit, so they are spelled
// once, here, and never derived from a Go field name.
const (
	keyEnabled        = "translateEnabled"
	keyAccelerator    = "translateAccelerator"
	keyProvider       = "translateProvider"
	keyFurigana       = "translateFurigana"
	keyLoanwordGloss  = "translateLoanwordGloss"
	keyPinned         = "translatePinned"
	keyCloseDelay     = "translateCloseDelayMs"
	keyCustomServices = "translators"
)

// sectionName is what errors call this feature's settings.
const sectionName = "translate"

// customID is the translator whose settings the user fills in themselves. It is
// named here, and not imported from the package that implements it, because the
// file's field names are a storage decision: this package must keep reading a
// file whose translator moved.
const customID = "custom"

// StoreSettings is the part of the settings store this feature needs.
//
// It is declared here, and not taken from internal/settings, so that this
// package does not depend on the storage implementation: the assembly injects
// one, and a test injects its own.
type StoreSettings interface {
	Load(ctx context.Context) error
	Update(ctx context.Context, section string, change func() error) error
}

// customDocument is what the file holds for a user-configured translator.
type customDocument struct {
	BaseURL     string `json:"baseURL,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

// Section is this feature's settings as the store sees them: a name, a way to
// read the keys it owns out of the file, and a way to hand them back.
type Section struct {
	current Settings
}

// NewSection returns an empty section, holding this feature's defaults.
func NewSection() *Section {
	return &Section{current: Defaults()}
}

// Name is what errors call this section.
func (s *Section) Name() string { return sectionName }

// Current is the settings as they stand.
func (s *Section) Current() Settings { return s.current }

// Set replaces the settings.
func (s *Section) Set(settings Settings) { s.current = settings.Normalize() }

// Decode reads the translation tool's keys out of values, which holds every
// feature's keys.
//
// A key that is not there leaves the setting as it is. That matters most for
// the two switches whose default is on: a file from before the tool existed has
// no key for them at all, and reading an absent key as false turned both
// Japanese annotations off.
func (s *Section) Decode(values map[string]json.RawMessage) error {
	settings := s.current
	if err := into(values, keyEnabled, &settings.Enabled); err != nil {
		return err
	}
	if err := into(values, keyAccelerator, &settings.Accelerator); err != nil {
		return err
	}
	if err := into(values, keyProvider, &settings.Provider); err != nil {
		return err
	}
	if err := into(values, keyFurigana, &settings.Furigana); err != nil {
		return err
	}
	if err := into(values, keyLoanwordGloss, &settings.LoanwordGloss); err != nil {
		return err
	}
	if err := into(values, keyPinned, &settings.Pinned); err != nil {
		return err
	}
	var delayMs int64
	if err := into(values, keyCloseDelay, &delayMs); err != nil {
		return err
	}
	if delayMs != 0 {
		settings.CloseDelay = time.Duration(delayMs) * time.Millisecond
	}
	var custom map[string]customDocument
	if err := into(values, keyCustomServices, &custom); err != nil {
		return err
	}
	if own, present := custom[customID]; present {
		settings.Custom = CustomSettings{BaseURL: own.BaseURL, DisplayName: own.DisplayName}
	}
	s.current = settings.Normalize()
	return nil
}

// Encode returns the keys the translation tool owns, ready for the store to
// merge into the file.
func (s *Section) Encode() (map[string]any, error) {
	settings := s.current.Normalize()
	return map[string]any{
		keyEnabled:       settings.Enabled,
		keyAccelerator:   settings.AcceleratorOr(),
		keyProvider:      settings.ProviderOr(),
		keyFurigana:      settings.Furigana,
		keyLoanwordGloss: settings.LoanwordGloss,
		keyPinned:        settings.Pinned,
		keyCloseDelay:    settings.CloseDelayOr().Milliseconds(),
		// The map is written even when it holds nothing worth keeping: its
		// presence is what tells a reader that this build wrote the file, and
		// therefore that an absent key inside it is a choice rather than a
		// version's gap.
		keyCustomServices: map[string]customDocument{
			customID: {
				BaseURL:     settings.Custom.BaseURL,
				DisplayName: settings.Custom.DisplayName,
			},
		},
	}, nil
}

// into decodes one key of a settings object, leaving out alone when the file
// does not name it.
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
