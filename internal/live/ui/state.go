package ui

import (
	"context"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/live"
	"boku/internal/presentation"
)

// Config 是 shell 交给浮动窗口与设置区的内容。
type Config struct {
	Service      *live.Service
	Host         presentation.Host
	Dark         func() bool
	OpenSettings func()
	Mark         *ui.SVG
	CloseMark    *ui.SVG
	// Tray 在字幕打开时才读取：shell 在这个 state 之后才创建图标，
	// Windows 合成窗口栏时需要它。
	Tray func() *mygo.Tray
}

// State 持有浮动窗口与设置区。窗口不是翻译卡片：失焦后仍保持显示，
// 也不编辑术语。
type State struct {
	live         *live.Service
	host         presentation.Host
	dark         func() bool
	openSettings func()
	mark         *ui.SVG
	closeMark    *ui.SVG
	tray         func() *mygo.Tray

	window *mygo.Window
	// clipped 是最近一次 Windows 区域高度（DIP），绘制前为 -1。
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

// Repaint 在窗口存在时重绘浮动窗口。
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
