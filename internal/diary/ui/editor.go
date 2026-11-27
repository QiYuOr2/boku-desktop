package ui

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"boku/internal/diary"
	"boku/internal/presentation"
)

func (s *State) editorView(c *ui.Context, p diaryPalette, dark bool) {
	ui.Column(c).Fill().Children(func() {
		// 顶部工具栏。
		ui.Row(c).FillWidth().Shrink(0).
			Padding(12, 24, 12, 24).
			BorderWidth(0, 0, 1, 0).
			BorderColor(p.rule).
			AlignItems(ui.Center).
			Gap(10).
			Children(func() {
				if smallQuietBtn(c, "← 返回", p).Clicked() {
					s.CancelEditing()
				}
				if smallQuietBtn(c, s.ed.dateKey.String(), p).Clicked() {
					s.OpenPanel(panelDate)
				}
				if status := s.editorSaveLabel(); status != "" {
					col := p.faint
					if s.ed.saveState == saveFailed {
						col = presentation.ColorDanger
					}
					ui.Text(c, status).FontSize(11.5).TextColor(col).SingleLine()
				}
				ui.Spacer(c)
				presentation.Segmented(c, &s.ed.tab, "编辑", "分栏", "预览")
				if smallPrimaryBtn(c, "完成", p).Clicked() {
					s.DoneEditing()
				}
			})

		// 格式与插入栏（仅编辑 / 分栏模式展示）。
		if s.ed.tab != 2 {
			ui.Row(c).FillWidth().Shrink(0).
				Padding(8, 24, 8, 24).
				BorderWidth(0, 0, 1, 0).
				BorderColor(p.rule).
				AlignItems(ui.Center).
				Wrap().
				Gap(6).
				Children(func() {
					fmtBtn := func(label string, cmd diary.FormatCommand) {
						if toolbarBtn(c, label, false, p).Clicked() {
							s.ApplyFormat(cmd)
						}
					}
					fmtBtn("B", diary.FormatBold)
					fmtBtn("I", diary.FormatItalic)
					fmtBtn("S", diary.FormatStrike)
					fmtBtn("</>", diary.FormatCode)
					fmtBtn("H2", diary.FormatHeading)
					fmtBtn("•", diary.FormatBullet)
					fmtBtn("“", diary.FormatQuote)
					if toolbarBtn(c, "字色/字号", s.ed.panel == panelStyle, p).Clicked() {
						s.OpenPanel(panelStyle)
					}
					ui.Box(c).Width(1).Height(16).Background(p.rule)
					if toolbarBtn(c, "图片", false, p).Clicked() {
						s.PickImages()
					}
					if toolbarBtn(c, "链接", s.ed.panel == panelLink, p).Clicked() {
						s.OpenPanel(panelLink)
					}
					if toolbarBtn(c, "标签", s.ed.panel == panelTag, p).Clicked() {
						s.OpenPanel(panelTag)
					}
					if toolbarBtn(c, "地点", s.ed.panel == panelLocation, p).Clicked() {
						s.OpenPanel(panelLocation)
					}
					if toolbarBtn(c, "天气", s.ed.panel == panelWeather, p).Clicked() {
						s.OpenPanel(panelWeather)
					}
				})
		}

		// 内联弹出的单项输入条（一次最多展示一个）。
		if s.ed.panel != panelNone {
			s.editorInlinePanel(c, p, dark)
		}

		// 错误或自动采集提示。
		if s.ed.saveErr != "" || s.ed.metaNotice != "" {
			ui.Column(c).FillWidth().Shrink(0).Padding(8, 24, 0, 24).Gap(4).Children(func() {
				if s.ed.saveErr != "" {
					presentation.Notice(c, presentation.NoteDanger, s.ed.saveErr)
				}
				if s.ed.metaNotice != "" {
					presentation.Notice(c, presentation.NotePlain, s.ed.metaNotice)
				}
			})
		}

		// 编辑/分栏/预览主体。
		ui.Scroll(c).Fill().Children(func() {
			ui.Column(c).FillWidth().Padding(20, 28, 32, 28).Gap(14).Children(func() {
				// 标题。
				if s.ed.tab == 2 {
					title := strings.TrimSpace(s.ed.title)
					if title != "" {
						ui.Text(c, title).FontSize(22).FontWeight(700).TextColor(p.ink).Wrap()
					}
				} else {
					titleField := ui.TextInput(c, &s.ed.title).
						Placeholder("标题（可选）").
						SingleLine().
						FillWidth().
						Height(38).
						Background(ui.Transparent).
						Border(0, ui.Transparent).
						FocusRing(false).
						FontSize(22).
						FontWeight(600).
						FixedLineHeight(32).
						TextColor(p.ink)
					titleField.Bind(&s.ed.titleHandle)
					if s.ed.focusTitle {
						s.ed.titleHandle.Focus(c)
						s.ed.focusTitle = false
					}
				}

				// 上下文标签条（地点 / 天气 / 标签）。
				if s.ed.location != nil || s.ed.weather != nil || len(s.ed.tags) > 0 {
					ui.Row(c).FillWidth().Wrap().Gap(6).AlignItems(ui.Center).Children(func() {
						if s.ed.location != nil {
							removablePill(c, p, s.ed.location.Place, s.ClearLocation)
						}
						if s.ed.weather != nil {
							removablePill(c, p, formatWeather(*s.ed.weather), s.ClearWeather)
						}
						for _, tag := range s.ed.tags {
							t := tag
							removablePill(c, p, "#"+t, func() { s.RemoveTag(t) })
						}
					})
				}

				ui.Box(c).FillWidth().Height(1).Background(p.rule)

				// 正文区：编辑 / 分栏 / 预览。
				switch s.ed.tab {
				case 0:
					s.editorTextArea(c, p)
				case 1:
					ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(20).Children(func() {
						ui.Column(c).Grow(1).Children(func() {
							s.editorTextArea(c, p)
						})
						ui.Box(c).Width(1).MinHeight(320).Background(p.rule)
						ui.Column(c).Grow(1).Padding(4, 0, 0, 0).Children(func() {
							s.editorPreviewBody(c, p, dark)
						})
					})
				case 2:
					s.editorPreviewBody(c, p, dark)
				}

				// 本地图片附件条。
				if len(s.ed.attachments) > 0 {
					ui.Column(c).FillWidth().Gap(8).Padding(8, 0, 0, 0).Children(func() {
						ui.Row(c).FillWidth().Wrap().Gap(10).Children(func() {
							for _, att := range s.ed.attachments {
								c.Key(att.ID)
								a := att
								ui.Column(c).AlignItems(ui.Center).Gap(4).Children(func() {
									thumb := ui.ButtonBase(c).Label(a.Filename).Cursor(ui.CursorPointer)
									thumb.Children(func() { s.thumbnailBox(c, p, a.Filename, 92) })
									if thumb.Clicked() {
										s.OpenLightbox(a.Filename)
									}
									if presentation.TextAction(c, "移除", presentation.ColorDanger).Clicked() {
										s.RemoveAttachment(a.ID)
									}
								})
							}
						})
					})
				}

				// 外链卡片条。
				if len(s.ed.linkCards) > 0 {
					ui.Column(c).FillWidth().Gap(8).Padding(4, 0, 0, 0).Children(func() {
						for _, card := range s.ed.linkCards {
							s.linkCardRow(c, p, card, true)
						}
					})
				}
			})
		})
	})
}

func (s *State) editorTextArea(c *ui.Context, p diaryPalette) {
	area := ui.TextArea(c, &s.ed.markdown).
		FillWidth().
		MinHeight(320).
		Background(ui.Transparent).
		Border(0, ui.Transparent).
		FocusRing(false).
		FontSize(14.5).
		LineHeight(1.7).
		TextColor(p.ink).
		Placeholder("记录今天的片段… 支持 Markdown 语法")
	area.Bind(&s.ed.bodyHandle)
}

func (s *State) editorPreviewBody(c *ui.Context, p diaryPalette, dark bool) {
	if strings.TrimSpace(s.ed.markdown) == "" {
		ui.Text(c, "正文为空").FontSize(13.5).TextColor(p.faint)
		return
	}
	markdown.View(c, s.ed.markdown, s.markdownOptions(dark))
}

func (s *State) editorInlinePanel(c *ui.Context, p diaryPalette, dark bool) {
	ui.Column(c).FillWidth().Shrink(0).
		Background(p.raised).
		BorderWidth(0, 0, 1, 0).
		BorderColor(p.rule).
		Padding(10, 24, 10, 24).
		Gap(8).
		Children(func() {
			switch s.ed.panel {
			case panelDate:
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, "归属日期").FontSize(12.5).TextColor(p.secondary).SingleLine()
					inp := presentation.FieldFrame(c, ui.TextInput(c, &s.ed.dateInput).Placeholder("YYYY-MM-DD").SingleLine().Width(140))
					inp.Bind(&s.ed.panelInputHandle)
					if s.ed.focusPanelInput {
						s.ed.panelInputHandle.Focus(c)
						s.ed.focusPanelInput = false
					}
					if presentation.CardButton(c, "确定", presentation.ButtonPrimary).Clicked() {
						s.ApplyDateInput()
					}
					if presentation.CardButton(c, "取消", presentation.ButtonSecondary).Clicked() {
						s.ed.panel = panelNone
						s.host.Refresh()
					}
				})
			case panelTag:
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, "添加标签").FontSize(12.5).TextColor(p.secondary).SingleLine()
					inp := presentation.FieldFrame(c, ui.TextInput(c, &s.ed.tagInput).Placeholder("输入标签名").SingleLine().Width(180))
					inp.Bind(&s.ed.panelInputHandle)
					if s.ed.focusPanelInput {
						s.ed.panelInputHandle.Focus(c)
						s.ed.focusPanelInput = false
					}
					if presentation.CardButton(c, "添加", presentation.ButtonPrimary).Clicked() {
						s.AddTag()
					}
					if presentation.CardButton(c, "关闭", presentation.ButtonSecondary).Clicked() {
						s.ed.panel = panelNone
						s.host.Refresh()
					}
				})
			case panelLink:
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, "外链地址").FontSize(12.5).TextColor(p.secondary).SingleLine()
					inp := presentation.FieldFrame(c, ui.TextInput(c, &s.ed.linkInput).Placeholder("https://example.com").SingleLine().Grow(1))
					inp.Bind(&s.ed.panelInputHandle)
					if s.ed.focusPanelInput {
						s.ed.panelInputHandle.Focus(c)
						s.ed.focusPanelInput = false
					}
					label := "添加"
					if s.ed.linkBusy {
						label = "抓取中…"
					}
					if presentation.CardButton(c, label, presentation.ButtonPrimary).Clicked() && !s.ed.linkBusy {
						s.AddLinkCard()
					}
					if presentation.CardButton(c, "取消", presentation.ButtonSecondary).Clicked() {
						s.ed.panel = panelNone
						s.host.Refresh()
					}
				})
				if s.ed.linkErr != "" {
					presentation.Notice(c, presentation.NoteDanger, s.ed.linkErr)
				}
			case panelEditLink:
				ui.Column(c).FillWidth().Gap(8).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
						ui.Text(c, "标题").FontSize(12.5).TextColor(p.secondary).SingleLine()
						presentation.FieldFrame(c, ui.TextInput(c, &s.ed.editLinkTitle).SingleLine().Grow(1))
						ui.Checkbox(c, &s.ed.editLinkPreview, "显示摘要")
					})
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
						ui.Text(c, "摘要").FontSize(12.5).TextColor(p.secondary).SingleLine()
						presentation.FieldFrame(c, ui.TextInput(c, &s.ed.editLinkSummary).SingleLine().Grow(1))
						if presentation.CardButton(c, "保存", presentation.ButtonPrimary).Clicked() {
							s.SaveEditLinkCard()
						}
						if presentation.CardButton(c, "取消", presentation.ButtonSecondary).Clicked() {
							s.ed.panel = panelNone
							s.host.Refresh()
						}
					})
				})
			case panelLocation:
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, "地点").FontSize(12.5).TextColor(p.secondary).SingleLine()
					inp := presentation.FieldFrame(c, ui.TextInput(c, &s.ed.locationInput).Placeholder("如：杭州 · 西湖").SingleLine().Width(220))
					inp.Bind(&s.ed.panelInputHandle)
					if s.ed.focusPanelInput {
						s.ed.panelInputHandle.Focus(c)
						s.ed.focusPanelInput = false
					}
					if presentation.CardButton(c, "保存", presentation.ButtonPrimary).Clicked() {
						s.ApplyLocationInput()
					}
					if presentation.CardButton(c, "取消", presentation.ButtonSecondary).Clicked() {
						s.ed.panel = panelNone
						s.host.Refresh()
					}
				})
			case panelWeather:
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, "城市").FontSize(12.5).TextColor(p.secondary).SingleLine()
					inp := presentation.FieldFrame(c, ui.TextInput(c, &s.ed.weatherInput).Placeholder("输入城市名查当前天气").SingleLine().Width(200))
					inp.Bind(&s.ed.panelInputHandle)
					if s.ed.focusPanelInput {
						s.ed.panelInputHandle.Focus(c)
						s.ed.focusPanelInput = false
					}
					label := "获取天气"
					if s.ed.weatherBusy {
						label = "获取中…"
					}
					if presentation.CardButton(c, label, presentation.ButtonPrimary).Clicked() && !s.ed.weatherBusy {
						s.FetchWeatherInput()
					}
					if presentation.CardButton(c, "取消", presentation.ButtonSecondary).Clicked() {
						s.ed.panel = panelNone
						s.host.Refresh()
					}
				})
				if s.ed.weatherErr != "" {
					presentation.Notice(c, presentation.NoteDanger, s.ed.weatherErr)
				}
			case panelStyle:
				ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
					colors := []struct{ id, label string }{
						{"gray", "灰"}, {"red", "红"}, {"orange", "橙"}, {"green", "绿"}, {"blue", "蓝"}, {"purple", "紫"},
					}
					for _, col := range colors {
						cItem := col
						b := ui.ButtonBase(c).Label("字色 "+cItem.label).Height(26).Padding(0, 8).Radius(6).Border(1, p.rule).Cursor(ui.CursorPointer)
						if b.Hovered() {
							b.Background(p.hover)
						}
						b.Children(func() {
							ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
								ui.Box(c).Size(8, 8).Radius(999).Background(styleTagColor(cItem.id, dark))
								ui.Text(c, cItem.label).FontSize(11.5).TextColor(p.ink).SingleLine()
							})
						})
						if b.Clicked() {
							s.ApplyFormat(diary.FormatColor(cItem.id))
						}
					}
					ui.Box(c).Width(1).Height(16).Background(p.rule)
					if toolbarBtn(c, "小字", false, p).Clicked() {
						s.ApplyFormat(diary.FormatSize("small"))
					}
					if toolbarBtn(c, "大字", false, p).Clicked() {
						s.ApplyFormat(diary.FormatSize("large"))
					}
				})
			}
		})
}

func (s *State) editorSaveLabel() string {
	switch s.ed.saveState {
	case saveSaving:
		return "保存中…"
	case saveSaved:
		return "已保存"
	case saveFailed:
		return "保存失败"
	default:
		if s.ed.dirty {
			return "未保存"
		}
		return ""
	}
}

func toolbarBtn(c *ui.Context, label string, active bool, p diaryPalette) ui.Element {
	b := ui.ButtonBase(c).Label(label).
		Height(26).
		Padding(0, 8).
		Radius(5).
		Cursor(ui.CursorPointer)
	if active {
		b.Background(p.selectedBg)
	} else if b.Hovered() {
		b.Background(p.hover)
	}
	col := p.secondary
	if active || b.Hovered() {
		col = p.ink
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(col).SingleLine()
	})
	return b
}

func smallPrimaryBtn(c *ui.Context, label string, p diaryPalette) ui.Element {
	b := ui.ButtonBase(c).Label(label).
		Height(28).
		Padding(0, 12).
		Radius(6).
		Background(p.accent).
		Cursor(ui.CursorPointer)
	if b.Pressed() {
		b.Background(presentation.ColorBrandPressed)
	} else if b.Hovered() {
		b.Background(presentation.ColorBrandHover)
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(p.onAccent).SingleLine()
	})
	return b
}

func removablePill(c *ui.Context, p diaryPalette, label string, onRemove func()) {
	ui.Row(c).Padding(2, 8).Radius(999).Background(p.chipFill).Border(1, p.rule).AlignItems(ui.Center).Gap(5).Children(func() {
		ui.Text(c, label).FontSize(11.5).TextColor(p.secondary).SingleLine()
		x := ui.ButtonBase(c).Label("移除 " + label).Cursor(ui.CursorPointer)
		x.Children(func() {
			ui.Text(c, "×").FontSize(12).TextColor(p.faint).SingleLine()
		})
		if x.Clicked() && onRemove != nil {
			onRemove()
		}
	})
}
