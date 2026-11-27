// Package preferences 保存应用级用户偏好（如外观选择），这些不是某个功能的业务数据。
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

// ThemeKey 是外观偏好在磁盘上的稳定键名。
const ThemeKey = appearanceThemeKey

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

// 即使持久化失败，本次运行也保留该选择：Update 的闭包会在存储解码最新的磁盘文件后重新应用它。
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
