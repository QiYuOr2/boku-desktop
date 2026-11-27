package live

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	keyEnabled  = "liveEnabled"
	keyInput    = "liveInput"
	keyASRModel = "liveAsrModel"
	keyMTModel  = "liveMtModel"
	keyASRPath  = "liveAsrPath"
	keyMTPath   = "liveMtPath"
	keyOpacity  = "liveOpacity"

	sectionName = "live"

	defaultASRModel = "parakeet-ja"
	defaultMTModel  = "hy-mt2-1.8b-q4"
	defaultOpacity  = 60
	minOpacity      = 30
	maxOpacity      = 100
	InputSystem     = "system"
	InputMicrophone = "microphone"
)

type Settings struct {
	Enabled  bool
	Input    string
	ASRModel string
	MTModel  string
	ASRPath  string
	MTPath   string
	Opacity  int
}

// Defaults 表示文件缺少这些键时的含义。缺失的键不等于零值：输入保持系统音频。
// Enabled 保留只是为了旧文件能原样读写，窗口不读它。
func Defaults() Settings {
	return Settings{
		Input:    InputSystem,
		ASRModel: defaultASRModel,
		MTModel:  defaultMTModel,
		Opacity:  defaultOpacity,
	}
}

type StoreSettings interface {
	Load(ctx context.Context) error
	Update(ctx context.Context, section string, change func() error) error
}

type Section struct{ current Settings }

func NewSection() *Section { return &Section{current: Defaults()} }

func (s *Section) Name() string { return sectionName }

func (s *Section) Current() Settings { return s.current }

func (s *Section) Decode(values map[string]json.RawMessage) error {
	settings := s.current
	if err := decode(values, keyEnabled, &settings.Enabled); err != nil {
		return err
	}
	if raw, ok := values[keyInput]; ok {
		var input string
		if err := json.Unmarshal(raw, &input); err != nil {
			return fmt.Errorf("%s: %w", keyInput, err)
		}
		settings.Input = input
	}
	if err := decode(values, keyASRModel, &settings.ASRModel); err != nil {
		return err
	}
	if err := decode(values, keyMTModel, &settings.MTModel); err != nil {
		return err
	}
	if err := decode(values, keyASRPath, &settings.ASRPath); err != nil {
		return err
	}
	if err := decode(values, keyMTPath, &settings.MTPath); err != nil {
		return err
	}
	if err := decode(values, keyOpacity, &settings.Opacity); err != nil {
		return err
	}
	s.current = normalize(settings)
	return nil
}

func (s *Section) Encode() (map[string]any, error) {
	settings := normalize(s.current)
	s.current = settings
	return map[string]any{
		keyEnabled:  settings.Enabled,
		keyInput:    settings.Input,
		keyASRModel: settings.ASRModel,
		keyMTModel:  settings.MTModel,
		keyASRPath:  settings.ASRPath,
		keyMTPath:   settings.MTPath,
		keyOpacity:  settings.Opacity,
	}, nil
}

func (s *Section) SetEnabled(ctx context.Context, store StoreSettings, enabled bool) error {
	return s.update(ctx, store, func(settings *Settings) { settings.Enabled = enabled })
}

func (s *Section) SetInput(ctx context.Context, store StoreSettings, input string) error {
	return s.update(ctx, store, func(settings *Settings) { settings.Input = input })
}

func (s *Section) SetPaths(ctx context.Context, store StoreSettings, asr, mt string) error {
	return s.update(ctx, store, func(settings *Settings) {
		settings.ASRPath = asr
		settings.MTPath = mt
	})
}

func (s *Section) SetOpacity(ctx context.Context, store StoreSettings, opacity int) error {
	return s.update(ctx, store, func(settings *Settings) { settings.Opacity = opacity })
}

func (s *Section) update(ctx context.Context, store StoreSettings, change func(*Settings)) error {
	if store == nil {
		return fmt.Errorf("live: no settings store")
	}
	return store.Update(ctx, sectionName, func() error {
		settings := s.current
		change(&settings)
		s.current = normalize(settings)
		return nil
	})
}

func normalize(settings Settings) Settings {
	settings.ASRPath = strings.TrimSpace(settings.ASRPath)
	settings.MTPath = strings.TrimSpace(settings.MTPath)
	if strings.TrimSpace(settings.Input) == InputMicrophone {
		settings.Input = InputMicrophone
	} else {
		settings.Input = InputSystem
	}
	if strings.TrimSpace(settings.ASRModel) == "" {
		settings.ASRModel = defaultASRModel
	}
	if strings.TrimSpace(settings.MTModel) == "" {
		settings.MTModel = defaultMTModel
	}
	if settings.Opacity == 0 {
		settings.Opacity = defaultOpacity
	}
	if settings.Opacity < minOpacity {
		settings.Opacity = minOpacity
	}
	if settings.Opacity > maxOpacity {
		settings.Opacity = maxOpacity
	}
	return settings
}

func decode(values map[string]json.RawMessage, key string, dest any) error {
	raw, ok := values[key]
	if !ok {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	return nil
}
