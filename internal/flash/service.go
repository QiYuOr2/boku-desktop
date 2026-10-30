package flash

import (
	"context"
	"fmt"
	"strings"
	"time"

	"boku/internal/domain"
)

// SettingsStore is the settings file, as this feature writes one key through it.
type SettingsStore interface {
	Update(ctx context.Context, section string, change func() error) error
}

// Service is the text-flash tool: the document, and the shortcut that opens it.
type Service struct {
	store    *Store
	section  *Section
	settings SettingsStore
	now      func() time.Time
}

// Open binds the document and the settings section already registered with the store.
func Open(path string, section *Section, settings SettingsStore, clock domain.Clock) (*Service, error) {
	if strings.TrimSpace(path) == "" || section == nil || settings == nil {
		return nil, fmt.Errorf("flash: incomplete setup")
	}
	if clock == nil {
		clock = domain.SystemClock{}
	}
	return &Service{store: NewStore(path), section: section, settings: settings, now: clock.Now}, nil
}

func (s *Service) List() ([]Entry, error) { return s.store.List() }

func (s *Service) SaveText(text, id string) (Entry, error) {
	return s.store.SaveText(text, id, s.now())
}

func (s *Service) Delete(id string) error { return s.store.Delete(id, s.now()) }

func (s *Service) Accelerator() string { return s.section.Current().AcceleratorOr() }

// SetAccelerator stores the shortcut. It does not register it; the shell binds
// every global shortcut together.
func (s *Service) SetAccelerator(ctx context.Context, accelerator string) error {
	accelerator = strings.TrimSpace(accelerator)
	// Empty clears the binding. Nothing is registered until the user sets one.
	if accelerator != "" && !ValidAccelerator(accelerator) {
		return fmt.Errorf("%w: shortcut", domain.ErrInvalidArgument)
	}
	return s.settings.Update(ctx, sectionName, func() error {
		current := s.section.Current()
		current.Accelerator = accelerator
		s.section.Set(current)
		return nil
	})
}
