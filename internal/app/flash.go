package app

import (
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
)

const (
	flashCaptureWidth  = 420
	flashCaptureHeight = 148
	flashHistoryWidth  = 420
	flashHistoryHeight = 640
)

func (s *State) flashSection(c *ui.Context, head *ui.Handle) {
	s.useDesign(c)
	s.flashUI.SettingsView(c, head)
}

func (s *State) flashCaptureView(c *ui.Context) {
	s.useDesign(c)
	s.flashUI.CaptureView(c)
}

func (s *State) flashHistoryView(c *ui.Context) {
	s.useDesign(c)
	s.flashUI.HistoryView(c)
}

func (s *State) openFlashCapture() {
	if s.flashUI == nil {
		return
	}
	s.flashUI.PrepareCapture()
	if s.flashCapture == nil {
		window := mygo.NewWindow(mygo.WindowOptions{
			Title:         "闪念",
			Width:         flashCaptureWidth,
			Height:        flashCaptureHeight,
			MinWidth:      320,
			MinHeight:     140,
			Hidden:        true,
			DisableResize: true,
			AlwaysOnTop:   true,
			SkipTaskbar:   true,
			// 与翻译卡片一致的无边框透明窗口，圆角卡片即窗口本身。
			Frameless:   true,
			Transparent: true,
			// 普通窗口会激活 Boku 并停留在当前 Space 与全屏应用之外；因此 macOS
			// 上用 panel 窗口，Windows 仍用普通窗口。
			Panel:   runtime.GOOS == "darwin",
			Content: ui.View(s.flashCaptureView),
		})
		s.flashCapture = window
		window.OnClosed(func() {
			if s.flashCapture == window {
				s.flashCapture = nil
			}
		})
		window.OnBlur(func() { s.hideFlashCapture() })
	}
	s.flashCapture.Show()
	s.flashCapture.Focus()
}

func (s *State) hideFlashCapture() {
	if s.flashCapture != nil {
		s.flashCapture.Hide()
	}
}

func (s *State) openFlashHistory() {
	if s.flashUI == nil {
		return
	}
	s.flashUI.Reload()
	s.syncActivationPolicy()
	if s.flashHistory == nil {
		window := mygo.NewWindow(mygo.WindowOptions{
			Title:     "闪念历史",
			Width:     flashHistoryWidth,
			Height:    flashHistoryHeight,
			MinWidth:  340,
			MinHeight: 320,
			Content:   ui.View(s.flashHistoryView),
		})
		s.flashHistory = window
		s.syncActivationPolicy()
		window.OnClosed(func() {
			if s.flashHistory == window {
				s.flashHistory = nil
				s.syncActivationPolicy()
			}
		})
	}
	s.flashHistory.Show()
	s.flashHistory.Focus()
}

func (s *State) copyFlashText(text string) {
	if s.clipboard == nil {
		return
	}
	s.clipboard.CopyAsync(domain.ClipboardEntry{Content: text, Display: text, Kind: domain.ClipKindText}, nil)
}
