// 本文件是面板功能在 settings.json 中的一侧：用户关闭了哪些可选面板。
//
// 拥有该文件的存储对内容一无所知：它把整份设置对象交给每个功能，并收回每个功能
// 声称拥有的键。本文件就是面板功能的那一半。
//
// 选择用 map 而非开关列表，因为它们以面板 id 为键，且文件未提及的面板是开启的：
// 缺失的键不是选择，在某个面板存在之前写入的文件不能被读成有人关闭了它。
package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// keyPanels 是存储选择所用的设置键。
const keyPanels = "panels"

// sectionName 是错误信息中称呼本功能设置的名字。
const sectionName = "panel"

// StoreSettings 是本功能所需的那部分设置存储。
//
// 它在此声明而非取自 internal/settings，使本包不依赖存储实现：装配注入一个实现，
// 测试注入自己的。
type StoreSettings interface {
	Load(ctx context.Context) error
	Update(ctx context.Context, section string, change func() error) error
}

// Section 是本功能设置对存储所呈现的样子：一个名字、从文件中读出选择的方式，
// 以及交回它们的方式。
type Section struct {
	choices map[string]bool
}

// NewSection 返回一个空 section：所有面板开启，也就是从未关闭过任何面板的用户的状态。
func NewSection() *Section { return &Section{} }

// Name 是错误信息中称呼本 section 的名字。
func (s *Section) Name() string { return sectionName }

// On 报告面板是否开启。默认开启，因为面板由装配列出，用户的开关才拿掉一个。
func (s *Section) On(panelID string) bool {
	on, chosen := s.choices[strings.TrimSpace(panelID)]
	return !chosen || on
}

// Choices 是用户所做的决定，按面板 id；返回副本，调用方永远无法写入本 section
// 持有的 map。
func (s *Section) Choices() map[string]bool {
	if len(s.choices) == 0 {
		return nil
	}
	copied := make(map[string]bool, len(s.choices))
	for id, on := range s.choices {
		copied[id] = on
	}
	return copied
}

// Set 记录对某个面板的选择。
func (s *Section) Set(panelID string, on bool) {
	id := strings.TrimSpace(panelID)
	if id == "" {
		return
	}
	choices := s.Choices()
	if choices == nil {
		choices = make(map[string]bool, 1)
	}
	choices[id] = on
	s.choices = choices
}

// Decode 从 values（含所有功能的键）中读出面板选择。
//
// map 被整体采用而非合并进已有内容：文件是用户决定的记录，手动从文件里删掉一条
// 选择意味着该面板重新开启，也就是默认值。
func (s *Section) Decode(values map[string]json.RawMessage) error {
	raw, present := values[keyPanels]
	if !present {
		return nil
	}
	var choices map[string]bool
	if err := json.Unmarshal(raw, &choices); err != nil {
		return fmt.Errorf("%s: %w", keyPanels, err)
	}
	if len(choices) == 0 {
		s.choices = nil
		return nil
	}
	s.choices = choices
	return nil
}

// Encode 返回本功能拥有的键，供存储合并进文件。
//
// 没有选择的文件不写该键，而不是写一个空键：两者含义相同——所有面板开启——而
// nil 正是从未有过选择的文件的样子。
func (s *Section) Encode() (map[string]any, error) {
	choices := s.Choices()
	if choices == nil {
		return map[string]any{}, nil
	}
	return map[string]any{keyPanels: choices}, nil
}
