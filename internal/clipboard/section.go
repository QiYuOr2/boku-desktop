// 本文件是剪贴板工具在 settings.json 中的那一半。
//
// 持有该文件的存储对其中内容一无所知：它把整份设置对象交给每个功能，并收回
// 每个功能声明拥有的键。本文件就是剪贴板的那一半——哪些键属于剪贴板，
// 以及本功能自己的 Settings 类型如何落盘。
//
// 把它放在这里而非 internal/settings，才使设置真正归属该功能：新增开关只改这个包、
// 不改别处，其它功能也够不到剪贴板的键，因为它从不持有解码这些键的类型。
package clipboard

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// 本功能在 settings.json 中拥有的键。这些是存储设计 §8 记载、用户可能手工编辑的
// 名字，因此只在此处拼写一次，绝不从 Go 字段名派生。
const (
	keyRegionVisible = "clipboardRegionVisible"
	keyMonitoring    = "clipboardMonitoring"
	keyPollInterval  = "clipboardPollIntervalMs"
	keyHistorySize   = "clipboardHistorySize"
)

// sectionName 是错误信息中称呼本功能设置的名字。
const sectionName = "clipboard"

// StoreSettings 是本功能所需的设置存储部分。
//
// 它在此声明而非取自 internal/settings，使本包不依赖存储实现：装配时注入一个，
// 测试注入自己的。形状刻意取最小可用——全部加载，通过一个先重读文件的函数
// 修改一个 section。
type StoreSettings interface {
	// Load 把文件读入所有已注册的 section。
	Load(ctx context.Context) error
	// Update 读取文件、执行 change、写回，全程不释放文件。change 会先重读文件，
	// 从而避免一个功能的改动把另一个功能的旧值写回。
	Update(ctx context.Context, section string, change func() error) error
}

// Section 是本功能按存储视角的设置：一个名字、一个从文件中读出其所拥有键的方法、
// 一个交还它们的方法。
//
// 它持有当前设置，因此服务在存储加载文件后从这里读取配置，并在请求存储保存前
// 先写到这里。
type Section struct {
	current Settings
}

// NewSection 返回一个空 section，持有本功能的默认值。
func NewSection() *Section {
	return &Section{current: Settings{}.Normalize()}
}

// Name 是错误信息中称呼本 section 的名字。
func (s *Section) Name() string { return sectionName }

// Current 是当前设置。
func (s *Section) Current() Settings { return s.current }

// Set 替换设置。
func (s *Section) Set(settings Settings) { s.current = settings.Normalize() }

// Decode 从 values（含所有功能的键）中读出剪贴板的键。
//
// 缺失的键会让该设置保持原样：文件可能写于该键存在之前，把缺失键当作零值读取，
// 会把用户从未碰过的开关关掉。
func (s *Section) Decode(values map[string]json.RawMessage) error {
	settings := s.current
	if err := into(values, keyRegionVisible, &settings.RegionVisible); err != nil {
		return err
	}
	if err := into(values, keyMonitoring, &settings.Monitoring); err != nil {
		return err
	}
	var intervalMs int64
	if err := into(values, keyPollInterval, &intervalMs); err != nil {
		return err
	}
	if intervalMs != 0 {
		settings.PollInterval = time.Duration(intervalMs) * time.Millisecond
	}
	if err := into(values, keyHistorySize, &settings.HistorySize); err != nil {
		return err
	}
	s.current = settings.Normalize()
	return nil
}

// Encode 返回剪贴板拥有的键，供存储合并进文件。
func (s *Section) Encode() (map[string]any, error) {
	settings := s.current.Normalize()
	return map[string]any{
		keyRegionVisible: settings.RegionVisible,
		keyMonitoring:    settings.Monitoring,
		keyPollInterval:  settings.Interval().Milliseconds(),
		keyHistorySize:   settings.KeepsHistory(),
	}, nil
}

// into 解码设置对象中的一个键，文件未指明它时保持原样。
//
// 它与存储记录的是同一规则，放在此处使本功能无需深入存储包即可遵循。
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
