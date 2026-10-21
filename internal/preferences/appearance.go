// Package preferences owns application-wide user preferences that are not a
// feature's business data, such as the chosen appearance.
package preferences

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type AppearanceMode string

const (
	AppearanceSystem AppearanceMode = "system"
	AppearanceLight  AppearanceMode = "light"
	AppearanceDark   AppearanceMode = "dark"
)

const (
	appearanceThemeKey    = "appearanceTheme"
	appearanceSectionName = "appearance"
)

// ThemeKey is the stable on-disk spelling of the appearance preference.
const ThemeKey = appearanceThemeKey

// Appearance is the settings Section for the user's display preference.
type Appearance struct{ mode AppearanceMode }

func NewAppearanceSection() *Appearance       { return &Appearance{mode: AppearanceSystem} }
func (a *Appearance) Name() string            { return appearanceSectionName }
func (a *Appearance) Mode() AppearanceMode    { return a.mode.Normalize() }
func (a *Appearance) Set(mode AppearanceMode) { a.mode = mode.Normalize() }
func (a *Appearance) Dark(systemDark bool) bool {
	switch a.Mode() {
	case AppearanceDark:
		return true
	case AppearanceLight:
		return false
	default:
		return systemDark
	}
}
func (a *Appearance) Decode(values map[string]json.RawMessage) error {
	raw, ok := values[appearanceThemeKey]
	if !ok {
		return nil
	}
	var mode string
	if err := json.Unmarshal(raw, &mode); err != nil {
		return fmt.Errorf("%s: %w", appearanceThemeKey, err)
	}
	a.Set(AppearanceMode(mode))
	return nil
}
func (a *Appearance) Encode() (map[string]any, error) {
	return map[string]any{appearanceThemeKey: string(a.Mode())}, nil
}
func (m AppearanceMode) Normalize() AppearanceMode {
	switch AppearanceMode(strings.TrimSpace(string(m))) {
	case AppearanceLight:
		return AppearanceLight
	case AppearanceDark:
		return AppearanceDark
	default:
		return AppearanceSystem
	}
}

type Store interface {
	Update(ctx context.Context, section string, change func() error) error
}

// Save applies the choice for this run even when persistence fails. Update's
// closure reapplies it after the store has decoded the latest on-disk file.
func (a *Appearance) Save(ctx context.Context, store Store, mode AppearanceMode) error {
	mode = mode.Normalize()
	a.Set(mode)
	if store == nil {
		return nil
	}
	err := store.Update(ctx, a.Name(), func() error { a.Set(mode); return nil })
	a.Set(mode)
	return err
}
