package anime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	keyEnabled  = "bangumiEnabled"
	keyUsername = "bangumiUsername"
	keyRefresh  = "bangumiRefreshMinutes"
	sectionName = "anime"
)

type StoreSettings interface {
	Update(context.Context, string, func() error) error
}

type Settings struct {
	Enabled        bool
	Username       string
	RefreshMinutes int
}

type Section struct{ settings Settings }

func NewSection() *Section {
	return &Section{settings: Settings{Enabled: true, RefreshMinutes: 30}}
}
func (s *Section) Name() string       { return sectionName }
func (s *Section) Settings() Settings { return s.settings }
func (s *Section) Set(v Settings) {
	v.Username = strings.TrimSpace(v.Username)
	if v.RefreshMinutes < 5 || v.RefreshMinutes > 180 || v.RefreshMinutes%5 != 0 {
		v.RefreshMinutes = 30
	}
	s.settings = v
}
func (s *Section) Decode(values map[string]json.RawMessage) error {
	if raw, ok := values[keyEnabled]; ok {
		if err := json.Unmarshal(raw, &s.settings.Enabled); err != nil {
			return fmt.Errorf("%s: %w", keyEnabled, err)
		}
	}
	if raw, ok := values[keyUsername]; ok {
		if err := json.Unmarshal(raw, &s.settings.Username); err != nil {
			return fmt.Errorf("%s: %w", keyUsername, err)
		}
		s.settings.Username = strings.TrimSpace(s.settings.Username)
	}
	if raw, ok := values[keyRefresh]; ok {
		if err := json.Unmarshal(raw, &s.settings.RefreshMinutes); err != nil {
			return fmt.Errorf("%s: %w", keyRefresh, err)
		}
	}
	s.Set(s.settings)
	return nil
}
func (s *Section) Encode() (map[string]any, error) {
	return map[string]any{keyEnabled: s.settings.Enabled, keyUsername: s.settings.Username, keyRefresh: s.settings.RefreshMinutes}, nil
}
