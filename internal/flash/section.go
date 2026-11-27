package flash

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	keyAccelerator = "flashAccelerator"
	sectionName    = "flash"
)

// Settings 保存打开闪念输入行的快捷键。为空表示未注册；各平台都没有默认组合。
type Settings struct {
	Accelerator string
}

// AcceleratorOr 返回已存快捷键，空白表示不注册。
func (s Settings) AcceleratorOr() string {
	return strings.TrimSpace(s.Accelerator)
}

// Section 是本功能在 settings.json 中的键。
type Section struct {
	current Settings
}

func NewSection() *Section { return &Section{} }

func (s *Section) Name() string { return sectionName }

func (s *Section) Current() Settings { return s.current }

func (s *Section) Set(settings Settings) { s.current = settings }

func (s *Section) Decode(values map[string]json.RawMessage) error {
	raw, ok := values[keyAccelerator]
	if !ok {
		return nil
	}
	var accelerator string
	if err := json.Unmarshal(raw, &accelerator); err != nil {
		return fmt.Errorf("%s: %w", keyAccelerator, err)
	}
	s.current.Accelerator = strings.TrimSpace(accelerator)
	return nil
}

func (s *Section) Encode() (map[string]any, error) {
	return map[string]any{keyAccelerator: s.current.AcceleratorOr()}, nil
}

// ValidAccelerator 校验 value 是否为「修饰键 + 按键」，拼写与翻译快捷键一致。
func ValidAccelerator(value string) bool {
	parts := strings.Split(strings.TrimSpace(value), "+")
	if len(parts) < 2 {
		return false
	}
	modifiers := map[string]bool{"ctrl": true, "shift": true, "alt": true, "super": true, "cmd": true, "cmdorctrl": true, "meta": true}
	for _, part := range parts[:len(parts)-1] {
		if !modifiers[strings.ToLower(strings.TrimSpace(part))] {
			return false
		}
	}
	key := strings.TrimSpace(parts[len(parts)-1])
	return key != "" && !modifiers[strings.ToLower(key)]
}

func SameAccelerator(a, b string) bool {
	left := strings.ToLower(strings.TrimSpace(a))
	right := strings.ToLower(strings.TrimSpace(b))
	return left != "" && left == right
}
