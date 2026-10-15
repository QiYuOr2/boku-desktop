package domain

import "strings"

// PanelSettings is what the user chose about the panels that can be turned
// off, by panel id.
//
// An absent id is not a choice: a panel is on until somebody turns it off, and
// a file written before a panel existed must not read as somebody having
// turned it off. That is why the zero value means "everything on" rather than
// "everything off", and why Choices is nil rather than an empty map when
// nothing has been chosen — the two mean the same thing.
type PanelSettings struct {
	// Choices holds the panels the user decided about: false is off, true is
	// on, and a panel whose id is not here is on.
	Choices map[string]bool
}

// On reports whether a panel is on. The default is on, because a panel is
// listed by the assembly and the user's switches are what take one away.
func (s PanelSettings) On(panelID string) bool {
	on, chosen := s.Choices[strings.TrimSpace(panelID)]
	return !chosen || on
}

// Set records a choice about one panel and returns the result: the map is
// copied rather than written into, so a caller can never change one a store
// is holding, and two callers cannot race over the same map.
func (s PanelSettings) Set(panelID string, on bool) PanelSettings {
	id := strings.TrimSpace(panelID)
	if id == "" {
		return s
	}
	choices := make(map[string]bool, len(s.Choices)+1)
	for key, value := range s.Choices {
		choices[key] = value
	}
	choices[id] = on
	return PanelSettings{Choices: choices}
}

// Normalize keeps a file that holds no choice as it is: nil and an empty map
// both mean every panel is on, and nil is what the file omits.
func (s PanelSettings) Normalize() PanelSettings {
	if len(s.Choices) == 0 {
		s.Choices = nil
	}
	return s
}
