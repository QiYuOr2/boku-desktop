package ui

import (
	"errors"
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"

	"boku/internal/flash"
	"boku/internal/presentation"
)

// CaptureView is the capture card. It uses the translation card's parts:
// a rounded sheet, the text, a hairline, and a stone foot.
//
// The field keeps no border. A text input paints its border in the accent
// color while focused, which turned this card into a green box. The shortcut
// belongs to the window, so it still saves when focus is on the foot.
func (s *State) CaptureView(c *ui.Context) {
	c.OnShortcut(ui.Cmd, ui.KeyEnter, s.commitCapture)
	if ui.Cmd != ui.Ctrl {
		c.OnShortcut(ui.Ctrl, ui.KeyEnter, s.commitCapture)
	}
	c.OnShortcut(0, ui.KeyEscape, func() {
		if s.hideCapture != nil {
			s.hideCapture()
		}
	})
	c.Root().Background(ui.Transparent).Children(func() {
		corner := float32(presentation.RadiusCard - 1)
		card := ui.Column(c).Fill().
			Background(presentation.ColorSurface).
			Radius(presentation.RadiusCard).
			Border(1, presentation.ColorBorder)
		card.Children(func() {
			ui.Column(c).FillWidth().Grow(1).
				Padding(float32(presentation.CardHeadTop), float32(presentation.CardEdge), float32(presentation.CardBodyEnd), float32(presentation.CardEdge)).
				Children(func() {
					field := ui.TextArea(c, &s.captureDraft).
						FillWidth().
						Grow(1).
						MinHeight(float32(48)).
						FocusRing(false).
						Border(0, ui.Transparent).
						Background(ui.Transparent).
						FontSize(float32(presentation.SizeCardSource)).
						FontWeight(500).
						TextColor(presentation.ColorSourceInk).
						Placeholder("记一条")
					field.Bind(&s.captureHandle)
					if s.focusCapture {
						s.captureHandle.Focus(c)
						s.focusCapture = false
					}
					if s.captureNotice != "" {
						ui.Spacer(c).Height(float32(8)).Shrink(0)
						presentation.Notice(c, presentation.NoteWarning, s.captureNotice)
					}
				})
			ui.Row(c).FillWidth().Height(1).Shrink(0).Background(presentation.ColorCardLine)
			ui.Row(c).FillWidth().Shrink(0).
				Background(presentation.ColorCardFoot).
				Radius(0, 0, corner, corner).
				Padding(float32(presentation.CardFootTop), float32(12), float32(presentation.CardFootEnd), float32(12)).
				AlignItems(ui.Center).
				Gap(float32(8)).
				Children(func() {
					ui.Text(c, "闪念").
						FontSize(float32(presentation.SizeCardFoot)).
						TextColor(presentation.ColorFooterInk).
						SingleLine()
					ui.Spacer(c)
					s.cardSaveKeys(c)
					if smallPrimary(c, "保存").Clicked() {
						s.commitCapture()
					}
				})
		})
	})
}

// smallPrimary is the capture foot's button. The label is only what assistive
// technology reads; the words are a child, so a short row cannot clip them.
func smallPrimary(c *ui.Context, label string) ui.Element {
	b := ui.ButtonBase(c).Label(label).
		Padding(float32(2), float32(8)).
		Radius(float32(4)).
		Background(presentation.ColorBrand).
		Shrink(0).
		Cursor(ui.CursorPointer)
	switch {
	case b.Pressed():
		b = b.Background(presentation.ColorBrandPressed)
	case b.Hovered() || b.Focused():
		b = b.Background(presentation.ColorBrandHover)
	}
	b.Children(func() {
		ui.Text(c, label).
			FontSize(float32(presentation.SizeFine)).
			FontWeight(500).
			TextColor(presentation.ColorOnBrand).
			SingleLine()
	})
	return b
}

// HistoryView is the list of flashes, with the field that writes the next one
// at the bottom. The window's title bar already names it.
//
// Command+Enter, or Ctrl+Enter where that is a different key, saves. The
// shortcut belongs to the window, so it still saves when focus is on the list.
func (s *State) HistoryView(c *ui.Context) {
	c.OnShortcut(ui.Cmd, ui.KeyEnter, s.commit)
	if ui.Cmd != ui.Ctrl {
		c.OnShortcut(ui.Ctrl, ui.KeyEnter, s.commit)
	}
	c.OnShortcut(0, ui.KeyEscape, s.escape)
	presentation.WindowFrame(c).Children(func() {
		// Search and the composer sit on the list. They are the same
		// cards as the settings page, so nothing draws a line between them.
		ui.Scroll(c).Fill().Children(func() {
			ui.Column(c).FillWidth().Padding(float32(62), 0, float32(148), 0).Children(func() {
				if s.loadErr != nil {
					presentation.Inset(c, func() {
						message := "无法读取闪念。"
						if errors.Is(s.loadErr, flash.ErrUnreadable) {
							message = "无法读取闪念，原文件未被覆盖。"
						}
						presentation.Notice(c, presentation.NoteWarning, message)
					})
					return
				}
				entries := s.visible()
				if len(entries) == 0 {
					presentation.Inset(c, func() {
						message := "还没有闪念。"
						if strings.TrimSpace(s.query) != "" {
							message = "没有匹配的闪念。"
						}
						ui.Text(c, message).FontSize(presentation.SizeBody).TextColor(presentation.ColorTextMuted)
					})
					return
				}
				for index, entry := range entries {
					if index > 0 {
						presentation.RowDivider(c)
					}
					s.historyRow(c, entry)
				}
			})
		})
		if s.loadErr == nil {
			s.searchCard(c)
		}
		s.composer(c)
	})
}

func (s *State) searchCard(c *ui.Context) {
	fieldCard(c).Absolute().Top(float32(10)).Left(presentation.PageEdge).Right(presentation.PageEdge).Children(func() {
		ui.TextInput(c, &s.query).
			Placeholder("搜索").
			SingleLine().
			FillWidth().
			Height(float32(presentation.HeightControl)).
			// The line box is centered, but the CJK ink sits low in it.
			// 4 under the line lifts the glyphs into the middle of the control.
			Padding(0, float32(12), float32(4), float32(12)).
			Background(ui.Transparent).
			Border(0, ui.Transparent).
			FocusRing(false).
			FontSize(presentation.SizeBody).
			TextColor(presentation.ColorInputValue)
	})
}

func (s *State) historyRow(c *ui.Context, entry flash.Entry) {
	c.Key(entry.ID)
	text := entry.Text
	if text == "" && entry.AudioFileName != "" {
		text = "（语音，这里还不能播放）"
	}
	row := ui.Column(c).FillWidth().Padding(float32(12), presentation.PageEdge).Gap(float32(4))
	hot := row.Hovered() || s.confirmDelete == entry.ID || s.editingID == entry.ID
	if hot {
		row = row.Background(presentation.ColorRowAlt)
	}
	row.Children(func() {
		ui.Text(c, text).FontSize(presentation.SizeBody).TextColor(presentation.ColorTextBody).Wrap()
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
			ui.Text(c, when(entry.CreatedAt)).
				FontSize(presentation.SizeSmall).
				TextColor(presentation.ColorTextFaint).
				SingleLine()
			ui.Spacer(c)
			// The actions stay in the row when the pointer is elsewhere.
			// Putting them in only while hovered changes the line's height,
			// the text jumps, and the pointer falls off the row.
			actions := ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(float32(8))
			if !hot {
				actions = actions.Opacity(0).PassThrough()
			}
			actions.Children(func() {
				if presentation.TextAction(c, "编辑", presentation.ColorTextMuted).Clicked() {
					s.beginEdit(entry)
				}
				if presentation.TextAction(c, "复制", presentation.ColorTextMuted).Clicked() {
					s.copy(entry.Text)
				}
				label := "删除"
				if s.confirmDelete == entry.ID {
					label = "确认删除"
				}
				if presentation.TextAction(c, label, presentation.ColorDanger).Clicked() {
					s.remove(entry.ID)
				}
			})
		})
	})
}

// composer rests at the bottom of the list. It is a settings card: the
// page's own face, a hairline, and the field's corner. On macOS it sits
// closer to the window so its corner can follow the window's arc.
func (s *State) composer(c *ui.Context) {
	inset, radius := composerChrome()
	fieldCard(c).Absolute().Left(inset).Right(inset).Bottom(inset).
		Radius(radius).
		Padding(float32(10), float32(12)).
		Gap(float32(6)).
		Children(func() {
			field := ui.TextArea(c, &s.draft).
				FillWidth().
				Lines(1, 4).
				FocusRing(false).
				Border(0, ui.Transparent).
				Background(ui.Transparent).
				FontSize(presentation.SizeBody).
				TextColor(presentation.ColorInputValue).
				Placeholder("记一条")
			field.Bind(&s.draftHandle)
			if s.focusDraft {
				s.draftHandle.Focus(c)
				s.focusDraft = false
			}
			if s.notice != "" {
				kind := presentation.NotePlain
				if s.noticeWarn {
					kind = presentation.NoteWarning
				}
				presentation.Notice(c, kind, s.notice)
			}
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
				if s.editingID != "" {
					if presentation.TextAction(c, "取消", presentation.ColorTextMuted).Clicked() {
						s.clearDraft()
					}
				}
				ui.Spacer(c)
				s.saveKeys(c)
				if smallPrimary(c, "保存").Clicked() {
					s.commit()
				}
			})
		})
}

// fieldCard is the search field and the composer. It is the card the
// settings page draws around one service: the page's face, one hairline,
// and the field's corner. No shadow and no frost.
func fieldCard(c *ui.Context) ui.Element {
	return ui.Column(c).
		Background(presentation.ColorSurface).
		Border(1, presentation.ColorBorder).
		Radius(presentation.RadiusField)
}

// composerChrome is how far the composer sits inside the window, and the
// radius of its corners. macOS rounds the window by 16. A field radius
// inside the page margin is a different center, so the gap pinches; taking
// the whole page margin off the window radius leaves a 2pt corner, which
// reads as a square inside the window's curve. 8 inside a 16 window is
// the same center, and still a round corner.
func composerChrome() (inset, radius float32) {
	if runtime.GOOS != "darwin" {
		return presentation.PageEdge, presentation.RadiusField
	}
	const windowRadius float32 = 16
	const insetFromWindow float32 = 8
	return insetFromWindow, windowRadius - insetFromWindow
}

// SettingsView is the shortcut row on the settings page.
func (s *State) SettingsView(c *ui.Context, head *ui.Handle) {
	presentation.Inset(c, func() {
		presentation.Stacked(c, float32(16), func() {
			presentation.SectionHead(c, head, s.mark, presentation.ColorStoneInk, "闪念", "快捷记录")
			presentation.Stacked(c, float32(8), func() {
				presentation.SectionLabel(c, "快捷键")
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(12)).Children(func() {
					ui.Text(c, "记一条闪念").FontSize(presentation.SizeBody).TextColor(presentation.ColorTextSoft).SingleLine().Grow(1)
					s.shortcutButton(c)
				})
				if s.recording {
					presentation.Notice(c, presentation.NotePlain, "正在录制：按下新的组合键，Esc 取消。")
				}
				if note := s.shortcutNote; note != "" {
					presentation.Notice(c, presentation.NoteWarning, note)
				} else if s.shortcutErr != "" {
					presentation.Notice(c, presentation.NoteWarning, s.shortcutErr)
				}
				if platform := shortcutPlatformNote(); platform != "" {
					presentation.Notice(c, presentation.NotePlain, platform)
				}
			})
		})
	})
}

func (s *State) cardSaveKeys(c *ui.Context) {
	s.saveKeyRow(c, presentation.ColorFooterInk, presentation.ColorCardLine, presentation.ColorSurface, float32(presentation.SizeCardFoot))
}

func (s *State) saveKeys(c *ui.Context) {
	s.saveKeyRow(c, presentation.ColorTextMuted, presentation.ColorBorder, presentation.ColorField, presentation.SizeSmall)
}

func (s *State) saveKeyRow(c *ui.Context, ink, border, face ui.Color, size float32) {
	if s.returnMark == nil || (runtime.GOOS == "darwin" && s.commandMark == nil) {
		ui.Text(c, saveHint()).FontSize(size).TextColor(ink).SingleLine()
		return
	}
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(float32(4)).Children(func() {
		if runtime.GOOS == "darwin" {
			keyCapIcon(c, s.commandMark, border, face, ink)
		} else {
			keyCapText(c, "Ctrl", border, face, ink)
		}
		ui.Text(c, "+").FontSize(size).TextColor(ink).SingleLine()
		keyCapIcon(c, s.returnMark, border, face, ink)
	})
}

func keyCapIcon(c *ui.Context, mark *ui.SVG, border, face, ink ui.Color) {
	ui.Box(c).Width(float32(22)).Height(float32(22)).Shrink(0).
		Radius(float32(4)).
		Border(1, border).
		Background(face).
		Center().
		Children(func() {
			ui.Icon(c, mark).Size(float32(12), float32(12)).TextColor(ink).Shrink(0)
		})
}

func keyCapText(c *ui.Context, label string, border, face, ink ui.Color) {
	ui.Box(c).Height(float32(22)).Shrink(0).
		Padding(0, float32(6)).
		Radius(float32(4)).
		Border(1, border).
		Background(face).
		Center().
		Children(func() {
			ui.Text(c, label).FontSize(float32(presentation.SizeTiny)).TextColor(ink).SingleLine()
		})
}

func saveHint() string {
	if runtime.GOOS == "darwin" {
		return "Cmd+Enter"
	}
	return "Ctrl+Enter"
}
