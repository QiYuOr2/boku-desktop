package playtime

import (
	"encoding/json"
	"fmt"
	"time"
)

// The keys this feature owns in settings.json. Spelled once, here.
const (
	keyEnabled      = "playtimeEnabled"
	keyPollInterval = "playtimePollIntervalMs"
)

const sectionName = "playtime"

const (
	// MinPollInterval is the fastest look the recorder will take. Shorter
	// does not make a finished session more exact: its end comes from the
	// process exiting, not from the next look.
	MinPollInterval = time.Second
	// MaxPollInterval is the slowest look that still notices a short run.
	MaxPollInterval = 60 * time.Second
	// DefaultPollInterval is the look taken when the file does not say,
	// or says something outside the two bounds.
	DefaultPollInterval = 2 * time.Second
)

// Settings is how often the recorder looks, and whether it looks at all.
type Settings struct {
	Enabled      bool
	PollInterval time.Duration
}

// Normalize puts an interval outside [1s, 60s] back to 2s. Enabled is left
// alone: false is a choice, and the default true is set by NewSection.
func (s Settings) Normalize() Settings {
	if s.PollInterval < MinPollInterval || s.PollInterval > MaxPollInterval {
		s.PollInterval = DefaultPollInterval
	}
	return s
}

// Interval is the poll after Normalize.
func (s Settings) Interval() time.Duration { return s.Normalize().PollInterval }

// Section is this feature's settings as the store sees them.
type Section struct{ current Settings }

// NewSection returns the defaults: recording on, looking every two seconds.
func NewSection() *Section {
	return &Section{current: Settings{Enabled: true, PollInterval: DefaultPollInterval}.Normalize()}
}

// Name is what errors call this section.
func (s *Section) Name() string { return sectionName }

// Current is the settings as they stand.
func (s *Section) Current() Settings { return s.current }

// Set replaces the settings.
func (s *Section) Set(settings Settings) { s.current = settings.Normalize() }

// Decode reads this feature's keys. A key that is absent leaves the
// default: a missing switch must not turn recording off.
func (s *Section) Decode(values map[string]json.RawMessage) error {
	settings := s.current
	if err := into(values, keyEnabled, &settings.Enabled); err != nil {
		return err
	}
	var intervalMs int64
	if err := into(values, keyPollInterval, &intervalMs); err != nil {
		return err
	}
	if intervalMs != 0 {
		settings.PollInterval = time.Duration(intervalMs) * time.Millisecond
	}
	s.current = settings.Normalize()
	return nil
}

// Encode returns the keys this feature owns.
func (s *Section) Encode() (map[string]any, error) {
	settings := s.current.Normalize()
	return map[string]any{
		keyEnabled:      settings.Enabled,
		keyPollInterval: settings.Interval().Milliseconds(),
	}, nil
}

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
