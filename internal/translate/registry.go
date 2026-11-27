// Package translate 是划词翻译工具：把用户选中的文本翻成另一种语言，
// 以及可调用的翻译器注册表和决定用哪个、怎么用的设置。
//
// 本文件是注册表。它和隔壁的 provider 注册表思路相同，但刻意不是同一个类型：
// provider 与 adapter 成对注册，因为原始响应需要解释器；而翻译器直接用领域自己的
// 语言作答，可以单独存在。这里不认识任何具体翻译器：main.go 里的装配负责注册，
// 每个后端位于本包下的子包中（translate/deepseek、translate/custom，以及它们共用
// 的协议 translate/openai）。
package translate

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"boku/internal/domain"
)

// Registry 按 id 持有翻译器。
type Registry struct {
	mu    sync.RWMutex
	byIDs map[string]domain.Translator
}

// NewRegistry 返回一个空注册表。
func NewRegistry() *Registry {
	return &Registry{byIDs: make(map[string]domain.Translator)}
}

// Register 添加一个翻译器。
//
// 重复 id 会被拒绝而非覆盖：两个翻译器共用 id 意味着同一个问题有两个答案，
// 只有装配点能决定哪个正确。无法通过校验的描述符同样被拒绝——没人能配置的
// 翻译器值得在启动时失败，而不是等到第一次按快捷键。
func (r *Registry) Register(reply domain.Translator) error {
	if reply == nil {
		return fmt.Errorf("%w: a translator is nil", domain.ErrInvalidArgument)
	}
	id := strings.TrimSpace(reply.ID())
	if id == "" {
		return fmt.Errorf("%w: a translator has no id", domain.ErrInvalidArgument)
	}
	if err := reply.Descriptor().Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byIDs[id]; exists {
		return fmt.Errorf("%w: translator %s is registered twice", domain.ErrAlreadyExists, id)
	}
	r.byIDs[id] = reply
	return nil
}

// Translator 返回某 id 下注册的翻译器，否则返回 ErrProviderNotFound。
func (r *Registry) Translator(id string) (domain.Translator, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	reply, ok := r.byIDs[strings.TrimSpace(id)]
	if !ok {
		return nil, fmt.Errorf("%w: translator %s", domain.ErrProviderNotFound, id)
	}
	return reply, nil
}

// Has 报告某个 id 下是否注册了翻译器。
//
// 调用者在注册一个可能已存在的翻译器前用它来询问：用户自己的服务随添加而注册，
// 重复注册同一个不算值得失败的错误。
func (r *Registry) Has(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.byIDs[strings.TrimSpace(id)]
	return ok
}

// Remove 从注册表移除一个翻译器。
//
// 它服务于用户自己运行、应用运行期间来去的服务：移除一个必须连带移除它的翻译器，
// 否则窗口会为一个已不在设置中的服务留下空行——一张没有地址、无从连接的卡片。
//
// 移除一个没人注册的 id 不是错误：调用者是在说「这个已经不在了」，而它本来就不在。
func (r *Registry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byIDs, strings.TrimSpace(id))
}

// TranslatorIDs 返回已注册的 id，排序后即设置窗口提供它们的顺序。
func (r *Registry) TranslatorIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.byIDs))
	for id := range r.byIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Len 是已注册翻译器的数量。
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byIDs)
}

// ensure 在接口定义处而非调用处校验接口已被满足。
var _ domain.TranslatorRegistry = (*Registry)(nil)
