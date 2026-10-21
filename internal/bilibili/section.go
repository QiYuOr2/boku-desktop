package bilibili

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	sectionName      = "bilibili"
	keyEnabled       = "bilibiliEnabled"
	keyUIDs          = "bilibiliUIDs"
	keyRefresh       = "bilibiliRefreshMinutes"
	keyDynamics      = "bilibiliDynamicCount"
	keyLiveNotice    = "bilibiliLiveNotifications"
	keyDynamicNotice = "bilibiliDynamicNotifications"
)

type StoreSettings interface {
	Update(context.Context, string, func() error) error
}

type Settings struct {
	Enabled              bool
	UIDs                 []int64
	RefreshMinutes       int
	DynamicCount         int
	LiveNotifications    bool
	DynamicNotifications bool
}

func Defaults() Settings { return Settings{Enabled: true, RefreshMinutes: 5, DynamicCount: 1} }

func (v Settings) Normalize() Settings {
	seen := make(map[int64]bool)
	ids := make([]int64, 0, len(v.UIDs))
	for _, id := range v.UIDs {
		if id > 0 && !seen[id] && len(ids) < 8 {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	v.UIDs = ids
	if v.RefreshMinutes < 1 || v.RefreshMinutes > 60 {
		v.RefreshMinutes = 5
	}
	if v.DynamicCount < 1 || v.DynamicCount > 5 {
		v.DynamicCount = 1
	}
	return v
}

type Section struct{ settings Settings }

func NewSection() *Section            { return &Section{settings: Defaults()} }
func (s *Section) Name() string       { return sectionName }
func (s *Section) Settings() Settings { return s.settings }
func (s *Section) Set(v Settings)     { s.settings = v.Normalize() }
func (s *Section) Decode(values map[string]json.RawMessage) error {
	v := s.settings
	for _, entry := range []struct {
		key    string
		target any
	}{
		{keyEnabled, &v.Enabled}, {keyUIDs, &v.UIDs}, {keyRefresh, &v.RefreshMinutes}, {keyDynamics, &v.DynamicCount}, {keyLiveNotice, &v.LiveNotifications}, {keyDynamicNotice, &v.DynamicNotifications},
	} {
		if raw, ok := values[entry.key]; ok {
			if err := json.Unmarshal(raw, entry.target); err != nil {
				return fmt.Errorf("%s: %w", entry.key, err)
			}
		}
	}
	s.Set(v)
	return nil
}
func (s *Section) Encode() (map[string]any, error) {
	v := s.settings
	return map[string]any{keyEnabled: v.Enabled, keyUIDs: v.UIDs, keyRefresh: v.RefreshMinutes, keyDynamics: v.DynamicCount, keyLiveNotice: v.LiveNotifications, keyDynamicNotice: v.DynamicNotifications}, nil
}

// ParseUID accepts the UID itself or the profile URL copied from Bilibili.
func ParseUID(raw string) (int64, error) {
	v := strings.TrimSpace(raw)
	for _, prefix := range []string{"https://", "http://"} {
		v = strings.TrimPrefix(v, prefix)
	}
	if strings.HasPrefix(v, "space.bilibili.com/") {
		v = strings.TrimPrefix(v, "space.bilibili.com/")
	}
	v = strings.TrimSuffix(strings.SplitN(strings.SplitN(v, "?", 2)[0], "#", 2)[0], "/")
	var id int64
	if _, err := fmt.Sscan(v, &id); err != nil || id <= 0 || fmt.Sprint(id) != v {
		return 0, fmt.Errorf("invalid Bilibili UID")
	}
	return id, nil
}
