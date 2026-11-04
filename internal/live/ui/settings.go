package ui

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"boku/internal/live"
	"boku/internal/presentation"
)

// SettingsView is the 实时翻译 section. It is a few rows, not a form: the
// glossary is edited here, and the float window is not.
func (s *State) SettingsView(c *ui.Context, head *ui.Handle) {
	if s == nil || s.live == nil {
		return
	}
	view := s.live.Snapshot()
	presentation.Inset(c, func() {
		presentation.Stacked(c, presentation.GroupGap, func() {
			presentation.SectionHead(c, head, s.mark, presentation.ColorStoneInk, "实时翻译", "日语 → 中文 · 本地")
			open := s.window != nil
			ui.Checkbox(c, &open, "显示字幕").
				FontSize(presentation.SizeBody).
				OnClick(func() {
					if open {
						s.show()
					} else {
						s.close()
					}
					s.host.Refresh()
				})
			s.opacityRow(c, view)
			s.modelRow(c, view)
			s.glossaryBlock(c, view)
			if s.formNote != "" {
				presentation.Notice(c, presentation.NoteWarning, s.formNote)
			}
		})
	})
}

func (s *State) opacityRow(c *ui.Context, view live.View) {
	value := float64(view.Settings.Opacity)
	changed := false
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(12)).Children(func() {
		presentation.SectionLabel(c, "透明度")
		if ui.Slider(c, &value, 30, 100).Step(5).Grow(1).Label("透明度").Changed() {
			changed = true
		}
		ui.Text(c, strconv.Itoa(int(value))+"%").
			FontSize(presentation.SizeSmall).
			TextColor(presentation.ColorTextMuted).
			SingleLine().
			Shrink(0)
	})
	if next := int(value); changed && next != view.Settings.Opacity {
		s.setOpacity(next)
	}
}

func (s *State) glossaryBlock(c *ui.Context, view live.View) {
	presentation.Stacked(c, presentation.FieldGap, func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			presentation.SectionLabel(c, "术语")
			ui.Spacer(c)
			label := "导入"
			if s.importOpen {
				label = "收起导入"
			}
			if presentation.TextAction(c, label, presentation.ColorTextMuted).Clicked() {
				s.importOpen = !s.importOpen
				s.host.Refresh()
			}
		})
		if view.GlossaryBroken {
			presentation.Notice(c, presentation.NoteWarning, "术语文件读不了，原文件没有改。")
		}
		ui.Row(c).FillWidth().AlignItems(ui.End).Gap(presentation.FieldGap).Children(func() {
			ui.Column(c).Grow(1).Gap(float32(4)).Children(func() {
				ui.Text(c, "日文原词").FontSize(presentation.SizeSmall).TextColor(presentation.ColorTextMuted).SingleLine()
				presentation.FieldFrame(c, ui.TextInput(c, &s.source).Label("日文原词").Placeholder("部室").SingleLine()).FillWidth()
			})
			ui.Column(c).Grow(1).Gap(float32(4)).Children(func() {
				ui.Text(c, "中文译法").FontSize(presentation.SizeSmall).TextColor(presentation.ColorTextMuted).SingleLine()
				presentation.FieldFrame(c, ui.TextInput(c, &s.target).Label("中文译法").Placeholder("社团活动室").SingleLine()).FillWidth()
			})
			ui.Row(c).Height(presentation.HeightCardControl).AlignItems(ui.Center).Gap(presentation.FieldGap).Shrink(0).Children(func() {
				label := "添加"
				if s.editing != "" {
					label = "保存"
				}
				if presentation.CardButton(c, label, presentation.ButtonPrimary).
					Disabled(strings.TrimSpace(s.source) == "" || strings.TrimSpace(s.target) == "").Clicked() {
					s.saveTerm()
				}
				if s.editing != "" && presentation.TextAction(c, "取消", presentation.ColorTextMuted).Label("取消术语编辑").Clicked() {
					s.clearTermEditor()
					s.host.Refresh()
				}
			})
		})
		if s.importOpen {
			ui.Text(c, "每行一条：日文 => 中文。空行和 # 开头会跳过。").
				FontSize(presentation.SizeSmall).
				TextColor(presentation.ColorTextMuted).
				Wrap()
			ui.TextArea(c, &s.importText).Label("导入术语").Placeholder("部室 => 社团活动室").FillWidth().Height(float32(96)).
				Padding(float32(10)).Radius(presentation.RadiusField).Border(float32(1), presentation.ColorBorder).
				Background(presentation.ColorField).FontSize(presentation.SizeBody).TextColor(presentation.ColorInputValue)
			if presentation.CardButton(c, "导入术语", presentation.ButtonSecondary).Disabled(strings.TrimSpace(s.importText) == "").Clicked() {
				skipped, err := s.live.ImportTerms(s.importText)
				if err != nil {
					s.termNote = err.Error()
					s.termNoteKind = presentation.NoteWarning
				} else {
					s.importText = ""
					s.importOpen = false
					s.termNote = "导入完成，跳过了 " + strconv.Itoa(skipped) + " 行"
					s.termNoteKind = presentation.NoteSuccess
				}
				s.host.Refresh()
			}
		}
		if s.termNote != "" {
			presentation.Notice(c, s.termNoteKind, s.termNote)
		}
		for _, entry := range view.Entries {
			s.termRow(c, entry)
		}
	})
}

func (s *State) termRow(c *ui.Context, entry live.Entry) {
	c.Key(entry.ID)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
		ui.Text(c, entry.Source+" → "+entry.Target).FontSize(presentation.SizeBody).TextColor(presentation.ColorTextBody).Wrap().Grow(1)
		if presentation.TextAction(c, "编辑", presentation.ColorTextMuted).Clicked() {
			s.editing = entry.ID
			s.source = entry.Source
			s.target = entry.Target
			s.termNote = ""
			s.host.Refresh()
		}
		if presentation.TextAction(c, "删除", presentation.ColorDanger).Clicked() {
			if err := s.live.DeleteTerm(entry.ID); err != nil {
				s.termNote = err.Error()
				s.termNoteKind = presentation.NoteWarning
			} else if s.editing == entry.ID {
				s.clearTermEditor()
			}
			s.host.Refresh()
		}
	})
}

func (s *State) saveTerm() {
	var err error
	if s.editing != "" {
		err = s.live.UpdateTerm(s.editing, s.source, s.target)
	} else {
		err = s.live.AddTerm(s.source, s.target)
	}
	if err != nil {
		s.termNote = err.Error()
		s.termNoteKind = presentation.NoteWarning
		s.host.Refresh()
		return
	}
	s.clearTermEditor()
	s.host.Refresh()
}

func (s *State) clearTermEditor() {
	s.editing = ""
	s.source = ""
	s.target = ""
	s.termNote = ""
}
