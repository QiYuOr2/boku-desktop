package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"boku/internal/diary"
	"boku/internal/presentation"
)

// WindowView 构建桌面端独立日记窗口的双栏视图：左侧日历/搜索/快捷操作栏，右侧时间线/详情/编辑器。
func (s *State) WindowView(c *ui.Context) {
	s.onSearchTick()
	if s.mode == modeEditor {
		s.onEditorInputTick()
	}

	c.OnShortcut(ui.Cmd, ui.KeyS, func() {
		if s.mode == modeEditor {
			_ = s.SaveNow()
		}
	})
	if ui.Cmd != ui.Ctrl {
		c.OnShortcut(ui.Ctrl, ui.KeyS, func() {
			if s.mode == modeEditor {
				_ = s.SaveNow()
			}
		})
	}
	c.OnShortcut(0, ui.KeyEscape, func() {
		if s.lightboxFilename != "" {
			s.CloseLightbox()
			return
		}
		if s.confirmDeleteID != "" {
			s.confirmDeleteID = ""
			s.host.Refresh()
			return
		}
		if s.mode == modeEditor && s.ed.panel != panelNone {
			s.ed.panel = panelNone
			s.host.Refresh()
			return
		}
		if s.mode == modeDetail {
			s.BackToTimeline()
		}
	})

	dark := isDarkTheme(c)
	p := paletteFor(dark)

	c.Root().Background(p.paper).Children(func() {
		ui.Row(c).Fill().Children(func() {
			s.sidebarView(c, p)
			ui.Column(c).FillHeight().Grow(1).Background(p.paper).Children(func() {
				if s.svc == nil {
					s.unavailableView(c, p)
					return
				}
				switch s.mode {
				case modeDetail:
					s.detailView(c, p, dark)
				case modeEditor:
					s.editorView(c, p, dark)
				default:
					s.timelineView(c, p)
				}
			})
		})
		if s.lightboxFilename != "" {
			s.lightboxView(c, p)
		}
	})
}

func (s *State) unavailableView(c *ui.Context, p diaryPalette) {
	ui.Column(c).Fill().Center().Padding(32).Gap(10).Children(func() {
		ui.Text(c, "日记数据库无法打开").FontSize(16).FontWeight(600).TextColor(p.ink)
		ui.Text(c, "原文件未被覆盖，请检查应用数据目录下的 diary.sqlite。").
			FontSize(13).TextColor(p.secondary).Wrap()
	})
}

func (s *State) sidebarView(c *ui.Context, p diaryPalette) {
	today := s.today()
	ui.Column(c).
		Width(296).
		FillHeight().
		Shrink(0).
		Background(p.sidebar).
		BorderWidth(0, 1, 0, 0).
		BorderColor(p.rule).
		Padding(18, 16, 14, 16).
		Gap(14).
		Children(func() {
			// 顶部：今天 + 快捷动作。
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Text(c, "日记").FontSize(18).FontWeight(600).TextColor(p.ink).SingleLine()
					ui.Text(c, today.DisplayMonthDay()).FontSize(11.5).TextColor(p.secondary).SingleLine()
				})
				if s.sync.status.LoggedIn {
					label := "同步"
					if s.sync.syncing {
						label = "同步中…"
					}
					if smallQuietBtn(c, label, p).Clicked() && !s.sync.syncing {
						s.SyncNow()
					}
				}
				if s.openSettings != nil {
					if smallQuietBtn(c, "设置", p).Clicked() {
						s.openSettings()
					}
				}
			})

			// 主写日记按钮。
			ctaDate := today
			ctaLabel := "写今天的日记"
			if s.filterByDate && s.selectedDate != today {
				ctaDate = s.selectedDate
				ctaLabel = fmt.Sprintf("补写 %d月%d日", ctaDate.Month, ctaDate.Day)
			}
			if primaryFullBtn(c, ctaLabel, p).Clicked() && s.svc != nil {
				s.NewEntry(ctaDate)
			}

			// 月历卡片。
			s.calendarCardView(c, p, today)

			// 本月统计与连续天数。
			monthTotal := diary.MonthCount(s.dayCounts, s.shownMonth)
			streak := diary.Streak(s.dayCounts, today)
			statsText := fmt.Sprintf("本月 %d 篇 · 连续 %d 天", monthTotal, streak)
			ui.Text(c, statsText).FontSize(11.5).TextColor(p.secondary).SingleLine()

			// 搜索框。
			ui.Row(c).FillWidth().
				Height(34).
				Padding(0, 10).
				Radius(8).
				Background(p.raised).
				Border(1, p.rule).
				AlignItems(ui.Center).
				Gap(6).
				Children(func() {
					ui.TextInput(c, &s.searchQuery).
						Placeholder("搜索标题、正文、标签或日期").
						SingleLine().
						Grow(1).
						Background(ui.Transparent).
						Border(0, ui.Transparent).
						FocusRing(false).
						FontSize(12.5).
						FixedLineHeight(presentation.InputLineHeight(12.5)).
						TextColor(p.ink)
					if strings.TrimSpace(s.searchQuery) != "" {
						if presentation.TextAction(c, "清除", p.secondary).Clicked() {
							s.ClearSearch()
						}
					}
				})

			ui.Spacer(c)

			// 底部同步状态行。
			s.syncFooterView(c, p)
		})
}

func (s *State) calendarCardView(c *ui.Context, p diaryPalette, today diary.DateKey) {
	ui.Column(c).FillWidth().
		Background(p.raised).
		Border(1, p.rule).
		Radius(10).
		Padding(12, 10, 10, 10).
		Gap(8).
		Children(func() {
			// 月份栏。
			canNext := s.shownMonth.AddMonths(1).Compare(today.MonthValue()) <= 0
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(4).Children(func() {
				ui.Text(c, s.shownMonth.DisplayZH()).FontSize(13).FontWeight(600).TextColor(p.ink).SingleLine().Grow(1)
				if calNavBtn(c, "‹", true, p).Clicked() {
					s.PrevMonth()
				}
				if calNavBtn(c, "今", true, p).Clicked() {
					s.JumpToday()
				}
				if calNavBtn(c, "›", canNext, p).Clicked() && canNext {
					s.NextMonth()
				}
			})

			// 星期标题。
			weekdays := []string{"日", "一", "二", "三", "四", "五", "六"}
			ui.Row(c).FillWidth().Children(func() {
				for _, wd := range weekdays {
					ui.Box(c).Grow(1).Center().Height(20).Children(func() {
						ui.Text(c, wd).FontSize(10.5).TextColor(p.faint).SingleLine()
					})
				}
			})

			// 自然月日期网格。
			weeks := diary.CalendarGrid(s.shownMonth)
			now := s.now()
			for _, week := range weeks {
				ui.Row(c).FillWidth().Children(func() {
					for _, day := range week {
						s.calendarDayCell(c, p, day, today, now)
					}
				})
			}
		})
}

func (s *State) calendarDayCell(c *ui.Context, p diaryPalette, day, today diary.DateKey, now time.Time) {
	inMonth := day.MonthValue() == s.shownMonth
	isFuture := day.IsFuture(now)
	isToday := day == today
	isSelected := s.filterByDate && day == s.selectedDate
	count := s.dayCounts[day]

	cell := ui.ButtonBase(c).
		Label(day.String()).
		Grow(1).
		Height(34).
		Radius(8).
		Center()
	if !isFuture {
		cell.Cursor(ui.CursorPointer)
	}
	switch {
	case isSelected:
		cell.Background(p.accent)
	case !isFuture && cell.Hovered():
		cell.Background(p.hover)
	case isToday:
		cell.Border(1, p.accent.Alpha(0.55))
	}

	numColor := p.ink
	if isSelected {
		numColor = p.onAccent
	} else if isFuture {
		numColor = p.faint.Alpha(0.45)
	} else if !inMonth {
		numColor = p.faint
	} else if isToday {
		numColor = p.accent
	}

	dotColor := p.accent
	if isSelected {
		dotColor = p.onAccent
	} else if !inMonth {
		dotColor = p.accent.Alpha(0.45)
	}

	cell.Children(func() {
		ui.Column(c).AlignItems(ui.Center).Gap(3).Children(func() {
			weight := 400
			if isToday || isSelected {
				weight = 600
			}
			ui.Text(c, fmt.Sprintf("%d", day.Day)).
				FontSize(12).
				FontWeight(weight).
				TextColor(numColor).
				SingleLine()

			// 与 Apple 端一致的日记篇数标记：1 篇单点、2 篇双点、3 篇以上短胶囊。
			ui.Row(c).Height(4).AlignItems(ui.Center).Justify(ui.Center).Gap(2).Children(func() {
				switch {
				case count == 1:
					ui.Box(c).Size(4, 4).Radius(999).Background(dotColor)
				case count == 2:
					ui.Box(c).Size(4, 4).Radius(999).Background(dotColor)
					ui.Box(c).Size(4, 4).Radius(999).Background(dotColor)
				case count >= 3:
					ui.Box(c).Size(12, 4).Radius(999).Background(dotColor)
				}
			})
		})
	})

	if !isFuture && cell.Clicked() {
		s.SelectDay(day)
	}
}

func (s *State) syncFooterView(c *ui.Context, p diaryPalette) {
	st := s.sync.status
	ui.Row(c).FillWidth().
		BorderWidth(1, 0, 0, 0).
		BorderColor(p.rule).
		Padding(10, 2, 0, 2).
		AlignItems(ui.Center).
		Gap(6).
		Children(func() {
			if !st.LoggedIn {
				label := "云同步未开启"
				if st.Configured {
					label = "登录云同步"
				}
				if presentation.TextAction(c, label, p.secondary).Clicked() && s.openSettings != nil {
					s.openSettings()
				}
				return
			}
			dot := presentation.ColorSuccess
			text := "已同步"
			if s.sync.syncing {
				dot = p.accent
				text = "正在同步…"
			} else if st.ConflictCount > 0 {
				dot = presentation.ColorWarning
				text = fmt.Sprintf("%d 条冲突待处理", st.ConflictCount)
			} else if st.PendingCount > 0 {
				dot = p.accent
				text = fmt.Sprintf("%d 条待同步", st.PendingCount)
			} else if st.LastSyncedAt != nil {
				text = "上次同步 " + st.LastSyncedAt.Local().Format("15:04")
			}
			ui.Box(c).Size(6, 6).Radius(999).Shrink(0).Background(dot)
			btn := ui.ButtonBase(c).Label(text).Grow(1).Justify(ui.Start).Cursor(ui.CursorPointer)
			btn.Children(func() {
				ui.Text(c, text).FontSize(11.5).TextColor(p.secondary).SingleLine()
			})
			if btn.Clicked() {
				if st.ConflictCount > 0 && s.openSettings != nil {
					s.openSettings()
				} else if !s.sync.syncing {
					s.SyncNow()
				}
			}
		})
}

func (s *State) timelineView(c *ui.Context, p diaryPalette) {
	today := s.today()
	ui.Column(c).Fill().Children(func() {
		// 顶部筛选条（仅在按天筛选或搜索时显示）。
		query := strings.TrimSpace(s.searchQuery)
		if s.filterByDate || query != "" {
			ui.Row(c).FillWidth().Shrink(0).
				Padding(14, 28, 12, 28).
				BorderWidth(0, 0, 1, 0).
				BorderColor(p.rule).
				AlignItems(ui.Center).
				Gap(10).
				Children(func() {
					heading := s.selectedDate.DisplayHeading(today) + " · " + s.selectedDate.DisplayMonthDay()
					if query != "" {
						heading = fmt.Sprintf("搜索“%s” · %d 篇", query, len(s.entries))
					}
					ui.Text(c, heading).FontSize(14).FontWeight(600).TextColor(p.ink).SingleLine().Grow(1)
					if query != "" {
						if presentation.TextAction(c, "清除搜索", p.secondary).Clicked() {
							s.ClearSearch()
						}
					} else if s.filterByDate {
						if presentation.TextAction(c, "显示全部", p.secondary).Clicked() {
							s.ClearDateFilter()
						}
					}
				})
		}

		ui.Scroll(c).Fill().Children(func() {
			ui.Column(c).FillWidth().Padding(24, 28, 32, 28).Gap(20).Children(func() {
				if s.loadErr != nil {
					presentation.Notice(c, presentation.NoteWarning, "无法读取日记，原数据库未被覆盖。")
					return
				}
				if len(s.entries) == 0 {
					s.emptyTimelineView(c, p, today, query)
					return
				}

				groups := groupEntriesByDay(s.entries)
				for _, g := range groups {
					s.dayGroupView(c, p, g, today)
				}

				if s.hasMore {
					ui.Row(c).FillWidth().Justify(ui.Center).Padding(8, 0, 0, 0).Children(func() {
						if presentation.DesignButton(c, "加载更多", presentation.ButtonSecondary).Clicked() {
							s.LoadMore()
						}
					})
				}
			})
		})
	})
}

func (s *State) emptyTimelineView(c *ui.Context, p diaryPalette, today diary.DateKey, query string) {
	ui.Column(c).FillWidth().
		Background(p.raised).
		Border(1, p.rule).
		Radius(12).
		Padding(32, 24, 32, 24).
		AlignItems(ui.Center).
		Gap(12).
		Children(func() {
			switch {
			case query != "":
				ui.Text(c, "没有找到匹配的日记").FontSize(14.5).FontWeight(500).TextColor(p.secondary)
			case s.filterByDate:
				label := s.selectedDate.DisplayHeading(today) + "还没有日记"
				ui.Text(c, label).FontSize(14.5).FontWeight(500).TextColor(p.secondary)
				btnLabel := "写一篇"
				if s.selectedDate != today {
					btnLabel = fmt.Sprintf("补写 %d月%d日", s.selectedDate.Month, s.selectedDate.Day)
				}
				if presentation.DesignButton(c, btnLabel, presentation.ButtonPrimary).Clicked() {
					s.NewEntry(s.selectedDate)
				}
			default:
				ui.Text(c, "还没有写过日记").FontSize(15).FontWeight(500).TextColor(p.secondary)
				if presentation.DesignButton(c, "写今天的日记", presentation.ButtonPrimary).Clicked() {
					s.NewEntry(today)
				}
			}
		})
}

type dayGroup struct {
	date    diary.DateKey
	entries []diary.Entry
}

func groupEntriesByDay(entries []diary.Entry) []dayGroup {
	var groups []dayGroup
	for _, e := range entries {
		if len(groups) == 0 || groups[len(groups)-1].date != e.DiaryDateKey {
			groups = append(groups, dayGroup{date: e.DiaryDateKey, entries: []diary.Entry{e}})
		} else {
			groups[len(groups)-1].entries = append(groups[len(groups)-1].entries, e)
		}
	}
	return groups
}

func (s *State) dayGroupView(c *ui.Context, p diaryPalette, g dayGroup, today diary.DateKey) {
	c.Key(g.date.String())
	ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(16).Children(func() {
		// 左侧日期列（对齐 Apple DiaryTimeline 48pt 宽）。
		ui.Column(c).Width(48).Shrink(0).Padding(4, 0, 0, 0).AlignItems(ui.Center).Gap(2).Children(func() {
			dayCol := p.ink
			if g.date == today {
				dayCol = p.accent
			}
			ui.Text(c, fmt.Sprintf("%02d", g.date.Day)).FontSize(20).FontWeight(600).TextColor(dayCol).SingleLine()
			ui.Text(c, g.date.ShortWeekdayZH()).FontSize(10.5).TextColor(p.secondary).SingleLine()
			ui.Text(c, fmt.Sprintf("%d月", g.date.Month)).FontSize(10).TextColor(p.faint).SingleLine()
		})

		// 右侧卡片列。
		ui.Column(c).Grow(1).Gap(10).Children(func() {
			for _, entry := range g.entries {
				s.timelineCardView(c, p, entry)
			}
		})
	})
}

func (s *State) timelineCardView(c *ui.Context, p diaryPalette, entry diary.Entry) {
	c.Key(entry.ID)
	title := strings.TrimSpace(entry.Title)
	summary := diary.Summary(entry.Markdown, 140)
	timeText := entry.CreatedAt.Local().Format("15:04")

	card := ui.ButtonBase(c).
		Label(entryAccessibleLabel(entry, title, summary)).
		FillWidth().
		Background(p.raised).
		Border(1, p.rule).
		Radius(10).
		Padding(14, 16, 14, 16).
		Cursor(ui.CursorPointer)
	if card.Hovered() {
		card.Border(1, p.accent.Alpha(0.45))
	}

	card.Children(func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			switch {
			case title != "":
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, title).FontSize(15).FontWeight(600).TextColor(p.ink).SingleLine().Grow(1)
					ui.Text(c, timeText).FontSize(11).TextColor(p.faint).SingleLine()
				})
				if summary != "" {
					ui.Text(c, summary).FontSize(13).LineHeight(1.55).TextColor(p.secondary).Wrap()
				}
			case summary != "":
				ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(10).Children(func() {
					ui.Text(c, summary).FontSize(13.5).LineHeight(1.6).TextColor(p.ink).Wrap().Grow(1)
					ui.Text(c, timeText).FontSize(11).TextColor(p.faint).SingleLine().Padding(2, 0, 0, 0)
				})
			default:
				ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
					ui.Text(c, timeText).FontSize(11).TextColor(p.faint).SingleLine()
				})
			}

			// 本地图片缩略图条（最多显示 4 张）。
			if len(entry.Attachments) > 0 {
				ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
					limit := min(len(entry.Attachments), 4)
					for _, att := range entry.Attachments[:limit] {
						s.thumbnailBox(c, p, att.Filename, 56)
					}
					if len(entry.Attachments) > 4 {
						ui.Text(c, fmt.Sprintf("+%d", len(entry.Attachments)-4)).FontSize(11.5).TextColor(p.secondary)
					}
				})
			}

			// 元信息行（地点 / 天气 / 链接数 / 标签）。
			meta := entryMetaChips(entry)
			if len(meta) > 0 || len(entry.Tags) > 0 {
				ui.Row(c).FillWidth().Wrap().Gap(6).AlignItems(ui.Center).Children(func() {
					for _, m := range meta {
						ui.Text(c, m).FontSize(11).TextColor(p.faint).SingleLine()
					}
					for _, tag := range entry.Tags {
						ui.Box(c).Padding(1, 7).Radius(999).Background(p.chipFill).Children(func() {
							ui.Text(c, "#"+tag).FontSize(10.5).TextColor(p.secondary).SingleLine()
						})
					}
				})
			}
		})
	})

	if card.Clicked() {
		s.OpenEntry(entry)
	}
}

func (s *State) detailView(c *ui.Context, p diaryPalette, dark bool) {
	entry := s.currentEntry
	if entry == nil {
		s.BackToTimeline()
		return
	}
	ui.Column(c).Fill().Children(func() {
		// 详情页顶栏。
		ui.Row(c).FillWidth().Shrink(0).
			Padding(12, 24, 12, 24).
			BorderWidth(0, 0, 1, 0).
			BorderColor(p.rule).
			AlignItems(ui.Center).
			Gap(10).
			Children(func() {
				if smallQuietBtn(c, "← 返回", p).Clicked() {
					s.BackToTimeline()
				}
				dateLine := fmt.Sprintf("%d年%s · %s", entry.DiaryDateKey.Year, entry.DiaryDateKey.DisplayMonthDay(), entry.CreatedAt.Local().Format("15:04"))
				ui.Text(c, dateLine).FontSize(12.5).TextColor(p.secondary).SingleLine().Grow(1)
				if smallQuietBtn(c, "编辑", p).Clicked() {
					s.EditEntry(*entry)
				}
				delLabel := "删除"
				if s.confirmDeleteID == entry.ID {
					delLabel = "确认删除"
				}
				if presentation.TextAction(c, delLabel, presentation.ColorDanger).Clicked() {
					s.RequestDelete(entry.ID)
				}
			})

		// 正文阅读区。
		ui.Scroll(c).Fill().Children(func() {
			ui.Row(c).FillWidth().Justify(ui.Center).Padding(26, 28, 40, 28).Children(func() {
				ui.Column(c).FillWidth().MaxWidth(680).Gap(16).Children(func() {
					if title := strings.TrimSpace(entry.Title); title != "" {
						ui.Text(c, title).FontSize(24).FontWeight(700).TextColor(p.ink).Wrap()
					}

					// 上下文行：地点、天气、标签。
					if entry.Location != nil || entry.Weather != nil || len(entry.Tags) > 0 {
						ui.Row(c).FillWidth().Wrap().Gap(8).AlignItems(ui.Center).Children(func() {
							if entry.Location != nil {
								metaPill(c, p, entry.Location.Place)
							}
							if entry.Weather != nil {
								metaPill(c, p, formatWeather(*entry.Weather))
							}
							for _, tag := range entry.Tags {
								metaPill(c, p, "#"+tag)
							}
						})
					}

					ui.Box(c).FillWidth().Height(1).Background(p.rule)

					// 正文由 MyGo 官方 ui/markdown 组件渲染（含 <c>/<s > 兼容钩子与按需 HTTPS 图片）。
					if strings.TrimSpace(entry.Markdown) != "" {
						markdown.View(c, entry.Markdown, s.markdownOptions(dark))
					} else {
						ui.Text(c, "（无正文内容）").FontSize(13.5).TextColor(p.faint)
					}

					// 本地图片附件画廊。
					if len(entry.Attachments) > 0 {
						ui.Row(c).FillWidth().Wrap().Gap(10).Padding(8, 0, 0, 0).Children(func() {
							for _, att := range entry.Attachments {
								c.Key(att.ID)
								btn := ui.ButtonBase(c).Label(att.Filename).Cursor(ui.CursorPointer)
								btn.Children(func() {
									s.thumbnailBox(c, p, att.Filename, 148)
								})
								if btn.Clicked() {
									s.OpenLightbox(att.Filename)
								}
							}
						})
					}

					// 外链卡片列表。
					if len(entry.LinkCards) > 0 {
						ui.Column(c).FillWidth().Gap(8).Padding(8, 0, 0, 0).Children(func() {
							for _, card := range entry.LinkCards {
								s.linkCardRow(c, p, card, false)
							}
						})
					}

					// 天气来源署名。
					if entry.Weather != nil {
						ui.Row(c).FillWidth().Padding(8, 0, 0, 0).Children(func() {
							ui.Link(c, diary.WeatherAttribution, diary.WeatherAttributionURL).
								FontSize(11).TextColor(p.faint)
						})
					}
				})
			})
		})
	})
}

func (s *State) linkCardRow(c *ui.Context, p diaryPalette, card diary.LinkCard, editable bool) {
	c.Key(card.ID)
	box := ui.Row(c).FillWidth().
		Background(p.raised).
		Border(1, p.rule).
		Radius(8).
		Padding(10, 12, 10, 12).
		AlignItems(ui.Center).
		Gap(10)
	box.Children(func() {
		linkBtn := ui.ButtonBase(c).Label(card.Title).Grow(1).Cursor(ui.CursorPointer)
		linkBtn.Children(func() {
			ui.Column(c).FillWidth().Gap(3).Children(func() {
				title := strings.TrimSpace(card.Title)
				if title == "" {
					title = card.Domain
				}
				ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(p.ink).SingleLine()
				if card.ShowsPreview && strings.TrimSpace(card.Summary) != "" {
					ui.Text(c, card.Summary).FontSize(12).TextColor(p.secondary).Wrap()
				}
				ui.Text(c, card.Domain).FontSize(11).TextColor(p.faint).SingleLine()
			})
		})
		if linkBtn.Clicked() {
			openValidatedLink(card.URL)
		}
		if editable {
			if presentation.TextAction(c, "编辑", p.secondary).Clicked() {
				s.BeginEditLinkCard(card)
			}
			if presentation.TextAction(c, "移除", presentation.ColorDanger).Clicked() {
				s.RemoveLinkCard(card.ID)
			}
		}
	})
}

func (s *State) thumbnailBox(c *ui.Context, p diaryPalette, filename string, size float32) {
	bmp := s.attachmentBitmap(filename)
	box := ui.Box(c).Size(size, size).Radius(8).Background(p.chipFill).Border(1, p.rule).Center().Shrink(0)
	box.Children(func() {
		if bmp != nil {
			ui.Image(c, bmp).Size(size, size).Radius(8).Fit(ui.Cover)
			return
		}
		label := "加载中"
		if s.svc != nil && !s.svc.AttachmentExists(filename) {
			label = "待同步"
		} else if s.bitmapFailed[filename] {
			label = "无法预览"
		}
		ui.Text(c, label).FontSize(11).TextColor(p.faint).SingleLine()
	})
}

func (s *State) lightboxView(c *ui.Context, p diaryPalette) {
	bmp := s.attachmentBitmap(s.lightboxFilename)
	ui.Column(c).Absolute().Fill().
		Background(ui.RGBA(0, 0, 0, 0.78)).
		Padding(28).
		AlignItems(ui.Center).
		Justify(ui.Center).
		Gap(14).
		Children(func() {
			ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
				if presentation.DesignButton(c, "关闭 (Esc)", presentation.ButtonSecondary).Clicked() {
					s.CloseLightbox()
				}
			})
			ui.Box(c).Grow(1).FillWidth().Center().Children(func() {
				if bmp != nil {
					ui.Image(c, bmp).MaxWidth(840).MaxHeight(540).Fit(ui.Contain).Radius(8)
				} else {
					ui.Text(c, "图片尚未下载或无法解码").FontSize(14).TextColor(ui.Hex("#FFFFFF"))
				}
			})
		})
}

// --- 小构件与文案格式化 ---

func smallQuietBtn(c *ui.Context, label string, p diaryPalette) ui.Element {
	b := ui.ButtonBase(c).Label(label).
		Height(28).
		Padding(0, 10).
		Radius(6).
		Background(p.raised).
		Border(1, p.rule).
		Cursor(ui.CursorPointer)
	if b.Hovered() {
		b.Background(p.hover)
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(p.ink).SingleLine()
	})
	return b
}

func primaryFullBtn(c *ui.Context, label string, p diaryPalette) ui.Element {
	b := ui.ButtonBase(c).Label(label).
		FillWidth().
		Height(36).
		Radius(8).
		Background(p.accent).
		Center().
		Cursor(ui.CursorPointer)
	if b.Pressed() {
		b.Background(presentation.ColorBrandPressed)
	} else if b.Hovered() {
		b.Background(presentation.ColorBrandHover)
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(13).FontWeight(500).TextColor(p.onAccent).SingleLine()
	})
	return b
}

func calNavBtn(c *ui.Context, label string, enabled bool, p diaryPalette) ui.Element {
	b := ui.ButtonBase(c).Label(label).
		Height(24).
		Padding(0, 7).
		Radius(5)
	if enabled {
		b.Cursor(ui.CursorPointer)
		if b.Hovered() {
			b.Background(p.hover)
		}
	}
	col := p.secondary
	if !enabled {
		col = p.faint.Alpha(0.4)
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(col).SingleLine()
	})
	return b
}

func metaPill(c *ui.Context, p diaryPalette, label string) {
	ui.Box(c).Padding(2, 9).Radius(999).Background(p.chipFill).Border(1, p.rule).Children(func() {
		ui.Text(c, label).FontSize(11.5).TextColor(p.secondary).SingleLine()
	})
}

func entryAccessibleLabel(entry diary.Entry, title, summary string) string {
	if title != "" {
		return title
	}
	if summary != "" {
		return summary
	}
	return entry.DiaryDateKey.String()
}

func formatWeather(w diary.WeatherSnapshot) string {
	temp := int(math.Round(w.TemperatureCelsius))
	unit := w.Unit
	if unit == "" {
		unit = "°C"
	}
	if w.Place != "" {
		return fmt.Sprintf("%s %d%s · %s", w.Description, temp, unit, w.Place)
	}
	return fmt.Sprintf("%s %d%s", w.Description, temp, unit)
}

func entryMetaChips(e diary.Entry) []string {
	var out []string
	if e.Location != nil && strings.TrimSpace(e.Location.Place) != "" {
		out = append(out, e.Location.Place)
	}
	if e.Weather != nil {
		out = append(out, fmt.Sprintf("%s %d%s", e.Weather.Description, int(math.Round(e.Weather.TemperatureCelsius)), e.Weather.Unit))
	}
	if len(e.LinkCards) > 0 {
		out = append(out, fmt.Sprintf("%d 条链接", len(e.LinkCards)))
	}
	return out
}
