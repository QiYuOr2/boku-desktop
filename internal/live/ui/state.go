package ui

import (
	"context"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/live"
	"boku/internal/presentation"
)

// Config is what the shell hands the float window and the settings section.
type Config struct {
	Service      *live.Service
	Host         presentation.Host
	Dark         func() bool
	OpenSettings func()
	Mark         *ui.SVG
	CloseMark    *ui.SVG
	// Tray is read when the caption opens. The shell creates the icon
	// after this state, and Windows needs it to composite the bar.
	Tray func() *mygo.Tray
}

// State is the float window and the settings section. The window is not the
// translation card: it stays up when it loses focus, and it does not edit
// the glossary.
type State struct {
	live         *live.Service
	host         presentation.Host
	dark         func() bool
	openSettings func()
	mark         *ui.SVG
	closeMark    *ui.SVG
	tray         func() *mygo.Tray

	window *mygo.Window
	// clipped is the last Windows region height in DIPs, or -1 before paint.
	clipped int

	modelDetailsOpen bool
	choosingModel    bool
	modelNote        string
	modelNoteKind    presentation.NoteKind
	termNote         string
	termNoteKind     presentation.NoteKind
	source           string
	target           string
	importText       string
	importOpen       bool
	formNote         string
	editing          string
}

func New(config Config) *State {
	if config.Dark == nil {
		config.Dark = func() bool { return false }
	}
	state := &State{
		live:         config.Service,
		host:         config.Host,
		dark:         config.Dark,
		openSettings: config.OpenSettings,
		mark:         config.Mark,
		closeMark:    config.CloseMark,
		tray:         config.Tray,
		clipped:      -1,
	}
	return state
}

func (s *State) useDesign(c *ui.Context) {
	presentation.ApplyDesign(s.dark())
	c.SetTheme(presentation.Theme(s.dark()))
}

// Repaint redraws the float window, if it exists.
func (s *State) Repaint(after func()) bool {
	if s == nil || s.window == nil {
		return false
	}
	s.window.Update(after)
	return true
}

func (s *State) setOpacity(opacity int) {
	if s.live == nil {
		return
	}
	if err := s.live.SetOpacity(context.Background(), opacity); err != nil {
		s.formNote = "没有记住透明度"
	} else {
		s.formNote = ""
	}
	s.host.Refresh()
}

func (s *State) setInput(input string) {
	if s.live == nil {
		return
	}
	if err := s.live.SetInput(context.Background(), input); err != nil {
		s.formNote = "没有记住音源"
		s.host.Refresh()
		return
	}
	s.formNote = ""
	s.host.Refresh()
}
