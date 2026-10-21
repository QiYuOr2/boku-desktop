package app

import (
	"boku/internal/presentation"
	"github.com/egoist/mygo/ui"
)

func (s *State) useDesign(c *ui.Context) {
	presentation.ApplyDesign(s.dark)
	c.SetTheme(presentation.Theme(s.dark))
}
