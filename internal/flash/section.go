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

// Settings is the shortcut that opens the capture line. Empty means the
// shortcut is not registered. There is no default combination on any platform.
type Settings struct {
	Accelerator string
}

// AcceleratorOr is the stored shortcut. Blank means do not register one.
func (s Settings) AcceleratorOr() string {
	return strings.TrimSpace(s.Accelerator)
}

// Section is this feature's keys in settings.json.
type Section struct {
	current Settings
}

// NewSection returns no shortcut.
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

// ValidAccelerator reports whether value is a modifier plus a key, in the
// same spelling the translation shortcut uses.
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

// SameAccelerator reports whether two shortcuts are the same combination.
func SameAccelerator(a, b string) bool {
	left := strings.ToLower(strings.TrimSpace(a))
	right := strings.ToLower(strings.TrimSpace(b))
	return left != "" && left == right
}
