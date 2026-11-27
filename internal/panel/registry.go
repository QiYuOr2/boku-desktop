// Package panel 是面板功能：读取 provider 驱动的面板所展示的数字，以及把每个
// provider 与解释它的 adapter 配对的注册表。
//
// 本文件是注册表。两者一起注册并在启动时校验，因为缺少 adapter 的 provider 只能
// 在之后、在用户面前以一张空卡片的形式失败。panels.go 是服务：缓存、单飞
// （single-flight），以及失败时保留上一次良好读数。
//
// 这里不认识任何具体 provider：main.go 的装配注册它们，每个厂商自己的代码位于
// 本包下的子包（panel/deepseek、panel/codex）。
package panel

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"boku/internal/domain"
)

// Registry 按 provider id 保存 provider 及其 adapter。
type Registry struct {
	mu        sync.RWMutex
	providers map[string]domain.Provider
	adapters  map[string]domain.Adapter
}

// NewRegistry 返回一个空注册表。
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]domain.Provider),
		adapters:  make(map[string]domain.Adapter),
	}
}

// Register 添加一个 provider 及映射它的 adapter。
//
// 它拒绝重复和错配而不是覆盖：两个 provider 响应同一个 id 意味着对同一问题有两个
// 答案，只有应用的装配层能决定哪个正确。
func (r *Registry) Register(provider domain.Provider, adapter domain.Adapter) error {
	if provider == nil {
		return fmt.Errorf("%w: a provider is nil", domain.ErrInvalidArgument)
	}
	if adapter == nil {
		return fmt.Errorf("%w: the adapter of provider %s is nil", domain.ErrInvalidArgument, provider.ID())
	}
	id := strings.TrimSpace(provider.ID())
	if id == "" {
		return fmt.Errorf("%w: a provider has no id", domain.ErrInvalidArgument)
	}
	if err := provider.Descriptor().Validate(); err != nil {
		return err
	}
	if adapter.ProviderID() != id {
		return fmt.Errorf("%w: adapter maps %s but provider is %s", domain.ErrInvalidArgument, adapter.ProviderID(), id)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[id]; exists {
		return fmt.Errorf("%w: provider %s is registered twice", domain.ErrAlreadyExists, id)
	}
	if _, exists := r.adapters[id]; exists {
		return fmt.Errorf("%w: adapter for provider %s is registered twice", domain.ErrAlreadyExists, id)
	}
	r.providers[id] = provider
	r.adapters[id] = adapter
	return nil
}

// Provider 返回按 id 注册的 provider，否则返回 ErrProviderNotFound。
func (r *Registry) Provider(id string) (domain.Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	provider, ok := r.providers[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", domain.ErrProviderNotFound, id)
	}
	return provider, nil
}

// Adapter 返回按 id 注册的 adapter，否则返回 ErrProviderNotFound。
func (r *Registry) Adapter(id string) (domain.Adapter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	adapter, ok := r.adapters[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", domain.ErrProviderNotFound, id)
	}
	return adapter, nil
}

// IDs 返回已注册的 provider id，按序排列。
func (r *Registry) IDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.providers))
	for id := range r.providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Validate 报告缺少 adapter 的 provider，或缺少 provider 的 adapter。
// 应用在装配完成后调用一次：配置错误值得在启动时失败，而不是等用户打开第一个面板时。
func (r *Registry) Validate() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for id := range r.providers {
		if _, ok := r.adapters[id]; !ok {
			return fmt.Errorf("%w: provider %s has no adapter", domain.ErrProviderNotFound, id)
		}
	}
	for id := range r.adapters {
		if _, ok := r.providers[id]; !ok {
			return fmt.Errorf("%w: adapter %s has no provider", domain.ErrProviderNotFound, id)
		}
	}
	return nil
}
