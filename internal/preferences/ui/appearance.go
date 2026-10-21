// Package ui contains the presentation owned by application preferences.
package ui

import (
	"github.com/egoist/mygo/ui"

	"boku/internal/preferences"
	"boku/internal/presentation"
)

// AppearancePicker draws the preference-owned theme selector. The Shell owns
// persistence and applying the native theme; this view only reports a choice.
func AppearancePicker(c *ui.Context, current preferences.AppearanceMode, changed func(preferences.AppearanceMode)) {
	choices := []struct {
		label string
		mode  preferences.AppearanceMode
	}{
		{"浅色", preferences.AppearanceLight},
		{"深色", preferences.AppearanceDark},
		{"自动", preferences.AppearanceSystem},
	}
	selected := len(choices) - 1
	labels := make([]string, len(choices))
	for index, choice := range choices {
		labels[index] = choice.label
		if current.Normalize() == choice.mode {
			selected = index
		}
	}
	picker := presentation.Segmented(c, &selected, labels...)
	if picker.Changed() && selected >= 0 && selected < len(choices) && changed != nil {
		changed(choices[selected].mode)
	}
}
