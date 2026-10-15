// This file is the panel feature's side of settings.json: which optional
// panels the user turned off.
//
// The store that owns that file knows nothing about what is in it: it hands
// every feature the whole of the file's settings object and takes back the keys
// each feature says it owns. This file is the panel feature's half of that.
//
// The choices are a map rather than a list of switches because they are keyed
// by panel id, and a panel the file does not name is on: an absent key is not a
// choice, and a file written before a panel existed must not read as somebody
// having turned it off.
package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// keyPanels is the settings key the choices are stored under.
const keyPanels = "panels"

// sectionName is what errors call this feature's settings.
const sectionName = "panel"

// StoreSettings is the part of the settings store this feature needs.
//
// It is declared here, and not taken from internal/settings, so that this
// package does not depend on the storage implementation: the assembly injects
// one, and a test injects its own.
type StoreSettings interface {
	Load(ctx context.Context) error
	Update(ctx context.Context, section string, change func() error) error
}

// Section is this feature's settings as the store sees them: a name, a way to
// read the choices out of the file, and a way to hand them back.
type Section struct {
	choices map[string]bool
}

// NewSection returns an empty section: every panel on, which is what a user
// who has never turned one off has.
func NewSection() *Section { return &Section{} }

// Name is what errors call this section.
func (s *Section) Name() string { return sectionName }

// On reports whether a panel is on. The default is on, because a panel is
// listed by the assembly and the user's switches are what take one away.
func (s *Section) On(panelID string) bool {
	on, chosen := s.choices[strings.TrimSpace(panelID)]
	return !chosen || on
}

// Choices is what the user decided about, by panel id, copied so that a caller
// can never write into the map this section is holding.
func (s *Section) Choices() map[string]bool {
	if len(s.choices) == 0 {
		return nil
	}
	copied := make(map[string]bool, len(s.choices))
	for id, on := range s.choices {
		copied[id] = on
	}
	return copied
}

// Set records a choice about one panel.
func (s *Section) Set(panelID string, on bool) {
	id := strings.TrimSpace(panelID)
	if id == "" {
		return
	}
	choices := s.Choices()
	if choices == nil {
		choices = make(map[string]bool, 1)
	}
	choices[id] = on
	s.choices = choices
}

// Decode reads the panel choices out of values, which holds every feature's
// keys.
//
// The map is taken as a whole rather than merged into what is already there:
// the file is the record of what the user decided, and a choice removed from it
// by hand means the panel is on again, which is the default.
func (s *Section) Decode(values map[string]json.RawMessage) error {
	raw, present := values[keyPanels]
	if !present {
		return nil
	}
	var choices map[string]bool
	if err := json.Unmarshal(raw, &choices); err != nil {
		return fmt.Errorf("%s: %w", keyPanels, err)
	}
	if len(choices) == 0 {
		s.choices = nil
		return nil
	}
	s.choices = choices
	return nil
}

// Encode returns the keys this feature owns, ready for the store to merge into
// the file.
//
// A file that holds no choice is left without the key rather than given an
// empty one: both mean the same thing — every panel is on — and nil is what a
// file that never had a choice looks like.
func (s *Section) Encode() (map[string]any, error) {
	choices := s.Choices()
	if choices == nil {
		return map[string]any{}, nil
	}
	return map[string]any{keyPanels: choices}, nil
}
