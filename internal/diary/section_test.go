package diary

import (
	"encoding/json"
	"testing"
)

func TestSectionDecodeEncodeAndSyncConfig(t *testing.T) {
	sec := NewSection()
	if _, ok := sec.Current().SyncConfig(); ok {
		t.Fatal("empty section should not produce a valid SyncConfig")
	}

	raw := map[string]json.RawMessage{
		keyLocationEnabled: json.RawMessage(`true`),
		keyLocationPlace:   json.RawMessage(`" 杭州 "`),
		keyWeatherEnabled:  json.RawMessage(`true`),
		keyWeatherPlace:    json.RawMessage(`" 上海 "`),
		keySyncEndpoint:    json.RawMessage(`"https://sync.example.com/"`),
	}
	if err := sec.Decode(raw); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got := sec.Current()
	if !got.LocationEnabled || got.LocationPlace != "杭州" || !got.WeatherEnabled || got.WeatherPlace != "上海" {
		t.Fatalf("unexpected decoded settings: %+v", got)
	}
	cfg, ok := got.SyncConfig()
	if !ok || cfg.APIBaseURL != "https://sync.example.com" {
		t.Fatalf("unexpected SyncConfig: %+v ok=%v", cfg, ok)
	}

	encoded, err := sec.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if encoded[keySyncEndpoint] != "https://sync.example.com" {
		t.Fatalf("encoded syncEndpoint mismatch: %v", encoded[keySyncEndpoint])
	}

	// 非法类型与非法 URL 校验。
	bad := NewSection()
	if err := bad.Decode(map[string]json.RawMessage{keyLocationEnabled: json.RawMessage(`"yes"`)}); err == nil {
		t.Fatal("expected type error on non-bool diaryLocationEnabled")
	}
	bad.Set(Settings{
		SyncEndpoint: "ftp://invalid.example.com",
	})
	if _, ok := bad.Current().SyncConfig(); ok {
		t.Fatal("ftp endpoint must not produce valid SyncConfig")
	}
}
