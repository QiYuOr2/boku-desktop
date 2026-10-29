package preferences

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	quickBlockPlaytime     = "playtime"
	quickBlockDeepSeek     = "deepseek"
	quickBlockCodex        = "codex"
	quickBlockBilibili     = "bilibili"
	quickBlockAnime        = "anime"
	quickLayoutKey         = "quickWindowLayout"
	quickLayoutSectionName = "quick-window-layout"
)

var defaultQuickBlockOrder = []string{quickBlockPlaytime, quickBlockDeepSeek, quickBlockCodex, quickBlockAnime, quickBlockBilibili}

type QuickWindowLayout struct {
	order  []string
	hidden map[string]bool
}

func NewQuickWindowLayout() *QuickWindowLayout      { return &QuickWindowLayout{} }
func (l *QuickWindowLayout) Name() string           { return quickLayoutSectionName }
func (l *QuickWindowLayout) Order() []string        { return append([]string(nil), l.normalizedOrder()...) }
func (l *QuickWindowLayout) Visible(id string) bool { return l == nil || !l.hidden[id] }
func (l *QuickWindowLayout) SetVisible(id string, visible bool) {
	if !knownQuickBlock(id) {
		return
	}
	if l.hidden == nil {
		l.hidden = make(map[string]bool)
	}
	if visible {
		delete(l.hidden, id)
	} else {
		l.hidden[id] = true
	}
}
func (l *QuickWindowLayout) MoveBefore(id, before string) {
	if !knownQuickBlock(id) || !knownQuickBlock(before) || id == before {
		return
	}
	order := l.normalizedOrder()
	from, to := quickBlockIndex(order, id), quickBlockIndex(order, before)
	if from < 0 || to < 0 {
		return
	}
	item := order[from]
	order = append(order[:from], order[from+1:]...)
	if from < to {
		to--
	}
	l.order = append(order[:to], append([]string{item}, order[to:]...)...)
}
func (l *QuickWindowLayout) MoveToEnd(id string) {
	if !knownQuickBlock(id) {
		return
	}
	order := l.normalizedOrder()
	from := quickBlockIndex(order, id)
	if from < 0 || from == len(order)-1 {
		return
	}
	item := order[from]
	order = append(order[:from], order[from+1:]...)
	l.order = append(order, item)
}
func (l *QuickWindowLayout) Decode(values map[string]json.RawMessage) error {
	raw, present := values[quickLayoutKey]
	if !present {
		return nil
	}
	var stored struct {
		Order  []string `json:"order"`
		Hidden []string `json:"hidden"`
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		return fmt.Errorf("%s: %w", quickLayoutKey, err)
	}
	l.order, l.hidden = normalizeQuickBlockOrder(stored.Order), make(map[string]bool, len(stored.Hidden))
	for _, id := range stored.Hidden {
		if knownQuickBlock(id) {
			l.hidden[id] = true
		}
	}
	return nil
}
func (l *QuickWindowLayout) Encode() (map[string]any, error) {
	hidden := make([]string, 0, len(l.hidden))
	for _, id := range l.normalizedOrder() {
		if l.hidden[id] {
			hidden = append(hidden, id)
		}
	}
	return map[string]any{quickLayoutKey: struct {
		Order  []string `json:"order"`
		Hidden []string `json:"hidden"`
	}{Order: l.Order(), Hidden: hidden}}, nil
}
func (l *QuickWindowLayout) normalizedOrder() []string { return normalizeQuickBlockOrder(l.order) }
func normalizeQuickBlockOrder(order []string) []string {
	result, seen := make([]string, 0, len(defaultQuickBlockOrder)), make(map[string]bool, len(defaultQuickBlockOrder))
	mentionedPlaytime := false
	for _, id := range order {
		id = strings.TrimSpace(id)
		if id == quickBlockPlaytime {
			mentionedPlaytime = true
		}
		if knownQuickBlock(id) && !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	for _, id := range defaultQuickBlockOrder {
		if !seen[id] {
			result = append(result, id)
		}
	}
	// Playtime used to sit above the ordered blocks. An older file that
	// never named it keeps that place; a file that already placed it does not.
	if !mentionedPlaytime {
		result = moveQuickBlockFront(result, quickBlockPlaytime)
	}
	return result
}

func moveQuickBlockFront(order []string, id string) []string {
	index := quickBlockIndex(order, id)
	if index <= 0 {
		return order
	}
	item := order[index]
	order = append(order[:index], order[index+1:]...)
	return append([]string{item}, order...)
}
func knownQuickBlock(id string) bool {
	for _, known := range defaultQuickBlockOrder {
		if id == known {
			return true
		}
	}
	return false
}
func quickBlockIndex(blocks []string, id string) int {
	for index, block := range blocks {
		if block == id {
			return index
		}
	}
	return -1
}

type LayoutStore interface {
	Update(context.Context, string, func() error) error
}

func (l *QuickWindowLayout) Save(ctx context.Context, store LayoutStore) error {
	if store == nil {
		return nil
	}
	order := l.Order()
	hidden := make(map[string]bool)
	for _, id := range order {
		if !l.Visible(id) {
			hidden[id] = true
		}
	}
	apply := func() {
		l.order = append([]string(nil), order...)
		l.hidden = make(map[string]bool, len(hidden))
		for id, value := range hidden {
			l.hidden[id] = value
		}
	}
	err := store.Update(ctx, l.Name(), func() error { apply(); return nil })
	apply()
	return err
}
