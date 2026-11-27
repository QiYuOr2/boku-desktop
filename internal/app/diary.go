package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	diaryWindowWidth     = 980
	diaryWindowHeight    = 680
	diaryWindowMinWidth  = 760
	diaryWindowMinHeight = 520
)

func (s *State) diarySection(c *ui.Context, head *ui.Handle) {
	s.useDesign(c)
	if s.diaryUI != nil {
		s.diaryUI.SettingsView(c, head)
	}
}

func (s *State) diaryView(c *ui.Context) {
	s.useDesign(c)
	if s.diaryUI != nil {
		s.diaryUI.WindowView(c)
	}
}

func (s *State) openDiary() {
	if s.diaryUI == nil {
		return
	}
	if s.panel != nil {
		s.panel.Close()
	}
	s.diaryUI.Reload()
	s.syncActivationPolicy()
	if s.diaryWindow == nil {
		window := mygo.NewWindow(mygo.WindowOptions{
			Title:     "日记",
			Width:     diaryWindowWidth,
			Height:    diaryWindowHeight,
			MinWidth:  diaryWindowMinWidth,
			MinHeight: diaryWindowMinHeight,
			Content:   ui.View(s.diaryView),
		})
		s.diaryWindow = window
		s.syncActivationPolicy()
		window.OnClose(func(*mygo.CloseEvent) {
			s.FlushDiary()
		})
		window.OnClosed(func() {
			if s.diaryWindow == window {
				s.diaryWindow = nil
				s.syncActivationPolicy()
			}
		})
	}
	s.diaryWindow.Show()
	s.diaryWindow.Focus()
}

func (s *State) openDiarySettings() {
	s.openSettings()
	if s.diaryUI != nil {
		s.diaryUI.ReloadSyncSettings()
	}
	s.openSettingsSection(sectionDiary)
}

// FlushDiary 在关闭日记窗口或退出应用前保存编辑器中未落盘的修改。
func (s *State) FlushDiary() {
	if s.diaryUI != nil {
		s.diaryUI.FlushOnClose()
	}
}
