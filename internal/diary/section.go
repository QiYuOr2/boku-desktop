package diary

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	sectionName        = "diary"
	keyLocationEnabled = "diaryLocationEnabled"
	keyLocationPlace   = "diaryLocationPlace"
	keyWeatherEnabled  = "diaryWeatherEnabled"
	keyWeatherPlace    = "diaryWeatherPlace"
	keySyncEndpoint    = "diarySyncEndpoint"
)

// Settings 是日记在 settings.json 中拥有的配置项。
// 同步服务端地址保存在设置文件中，不硬编码进公开源码。
type Settings struct {
	LocationEnabled bool
	LocationPlace   string
	WeatherEnabled  bool
	WeatherPlace    string
	SyncEndpoint    string
}

// SyncConfiguration 是同步客户端连接自托管同步服务所需的参数。
type SyncConfiguration struct {
	APIBaseURL string
}

// SyncConfig 返回已校验的云同步配置；地址缺失或非法时返回 false。
func (s Settings) SyncConfig() (SyncConfiguration, bool) {
	api := normalizeBaseURL(s.SyncEndpoint)
	if api == "" {
		return SyncConfiguration{}, false
	}
	return SyncConfiguration{APIBaseURL: api}, true
}

func normalizeBaseURL(raw string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" || strings.ContainsAny(trimmed, " \t\r\n") {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	return parsed.String()
}

// Section 实现 settings.Section，仅读写日记拥有的平铺键。
type Section struct {
	current Settings
}

func NewSection() *Section { return &Section{} }

func (s *Section) Name() string { return sectionName }

func (s *Section) Current() Settings { return s.current }

func (s *Section) Set(settings Settings) {
	settings.LocationPlace = strings.TrimSpace(settings.LocationPlace)
	settings.WeatherPlace = strings.TrimSpace(settings.WeatherPlace)
	settings.SyncEndpoint = strings.TrimRight(strings.TrimSpace(settings.SyncEndpoint), "/")
	s.current = settings
}

func (s *Section) Decode(values map[string]json.RawMessage) error {
	if raw, ok := values[keyLocationEnabled]; ok {
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			return fmt.Errorf("%s: %w", keyLocationEnabled, err)
		}
		s.current.LocationEnabled = v
	}
	if raw, ok := values[keyLocationPlace]; ok {
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return fmt.Errorf("%s: %w", keyLocationPlace, err)
		}
		s.current.LocationPlace = strings.TrimSpace(v)
	}
	if raw, ok := values[keyWeatherEnabled]; ok {
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			return fmt.Errorf("%s: %w", keyWeatherEnabled, err)
		}
		s.current.WeatherEnabled = v
	}
	if raw, ok := values[keyWeatherPlace]; ok {
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return fmt.Errorf("%s: %w", keyWeatherPlace, err)
		}
		s.current.WeatherPlace = strings.TrimSpace(v)
	}
	if raw, ok := values[keySyncEndpoint]; ok {
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return fmt.Errorf("%s: %w", keySyncEndpoint, err)
		}
		s.current.SyncEndpoint = strings.TrimRight(strings.TrimSpace(v), "/")
	}
	return nil
}

func (s *Section) Encode() (map[string]any, error) {
	return map[string]any{
		keyLocationEnabled: s.current.LocationEnabled,
		keyLocationPlace:   strings.TrimSpace(s.current.LocationPlace),
		keyWeatherEnabled:  s.current.WeatherEnabled,
		keyWeatherPlace:    strings.TrimSpace(s.current.WeatherPlace),
		keySyncEndpoint:    strings.TrimRight(strings.TrimSpace(s.current.SyncEndpoint), "/"),
	}, nil
}
