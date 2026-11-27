package playtime

import (
	"encoding/json"
	"fmt"
	"time"
)

// 本功能在 settings.json 里拥有的键名，只在这里写一次。
const (
	keyEnabled      = "playtimeEnabled"
	keyPollInterval = "playtimePollIntervalMs"
)

const sectionName = "playtime"

const (
	// MinPollInterval 是最快的轮询间隔。再快也不会让已结束的一局更精确：
	// 它的结束时间来自进程退出，而非下一次轮询。
	MinPollInterval = time.Second
	// MaxPollInterval 是仍能察觉短暂游玩的最慢轮询间隔。
	MaxPollInterval = 60 * time.Second
	// DefaultPollInterval 是文件未给出、或给出的值超出上下界时的轮询间隔。
	DefaultPollInterval = 2 * time.Second
)

type Settings struct {
	Enabled      bool
	PollInterval time.Duration
}

// Normalize 把超出 [1s, 60s] 的间隔改回 2s。Enabled 不动：false 是用户的选择，
// 默认的 true 由 NewSection 设置。
func (s Settings) Normalize() Settings {
	if s.PollInterval < MinPollInterval || s.PollInterval > MaxPollInterval {
		s.PollInterval = DefaultPollInterval
	}
	return s
}

func (s Settings) Interval() time.Duration { return s.Normalize().PollInterval }

type Section struct{ current Settings }

func NewSection() *Section {
	return &Section{current: Settings{Enabled: true, PollInterval: DefaultPollInterval}.Normalize()}
}

func (s *Section) Name() string { return sectionName }

func (s *Section) Current() Settings { return s.current }

func (s *Section) Set(settings Settings) { s.current = settings.Normalize() }

// Decode 读取本功能的键。缺失的键保持默认值：缺失的开关不能把录制关掉。
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
