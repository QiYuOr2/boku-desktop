package flash

import (
	"context"
	"fmt"
	"strings"
	"time"

	"boku/internal/domain"
)

// SettingsStore 是设置文件，本功能借此写入自己那个键。
type SettingsStore interface {
	Update(ctx context.Context, section string, change func() error) error
}

// Service 是文字闪念工具：文档以及打开它的快捷键。
type Service struct {
	store    *Store
	section  *Section
	settings SettingsStore
	now      func() time.Time
}

// Open 绑定文档与已注册到存储的设置段。
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

// SetAccelerator 保存快捷键但不注册；所有全局快捷键由 shell 统一绑定。
func (s *Service) SetAccelerator(ctx context.Context, accelerator string) error {
	accelerator = strings.TrimSpace(accelerator)
	// 空值清除绑定。在用户设置之前不注册任何快捷键。
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
