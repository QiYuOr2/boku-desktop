// Package ui draws the playtime block in the tray flyout.
package ui

import (
	"fmt"
	"time"

	"boku/internal/playtime"
	"boku/internal/presentation"

	"github.com/egoist/mygo/ui"
)

const (
	monthCell float32 = 20
	monthGap  float32 = 2
)

// Panel is the flyout block: today, this week, or one month.
//
// The choice and the month being shown are the only state. They stay here,
// not on the recorder, because they are how the block is read.
type Panel struct {
	span   int
	month  time.Time
	covers map[string]*ui.Bitmap
	names  map[string]string
	skip   map[string]bool
}

// Draw is one frame of the block. A recorder that does not record on this
// system draws nothing.
func (p *Panel) Draw(c *ui.Context, recorder *playtime.Recorder) {
	if p == nil || recorder == nil || !recorder.Available() {
		return
	}
	now := time.Now()
	sessions := recorder.Sessions()
	presentation.QuickBlock(c, func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "游戏时长").FontSize(presentation.SizeQuickName).FontWeight(600).TextColor(presentation.PanelInk).SingleLine()
				ui.Spacer(c)
				spanSwitch(c, &p.span)
			})
			switch p.span {
			case 1:
				p.week(c, sessions, now)
			case 2:
				p.drawMonth(c, sessions, now)
			default:
				p.today(c, sessions, now)
			}
		})
	})
}

// Prepare turns cover files that are already on disk into bitmaps. It runs
// on the main thread, before a frame, and never from the view: drawing can
// happen several times for one click.
func (p *Panel) Prepare(recorder *playtime.Recorder) {
	if p == nil || recorder == nil {
		return
	}
	if p.covers == nil {
		p.covers = map[string]*ui.Bitmap{}
		p.names = map[string]string{}
		p.skip = map[string]bool{}
	}
	seen := map[string]bool{}
	for _, session := range recorder.Sessions() {
		if session.AppID == "" || seen[session.AppID] {
			continue
		}
		seen[session.AppID] = true
		if p.names[session.AppID] == "" {
			if name := recorder.StoreName(session.AppID); name != "" {
				p.names[session.AppID] = name
			}
		}
		if p.covers[session.AppID] != nil || p.skip[session.AppID] {
			continue
		}
		data := recorder.Cover(session.AppID)
		if len(data) == 0 {
			continue
		}
		bitmap, err := ui.DecodeBitmap(data)
		if err != nil {
			p.skip[session.AppID] = true
			continue
		}
		p.covers[session.AppID] = bitmap
	}
}

// ShownMonth is the month the calendar shows. A panel that has not been
// moved shows the month of now.
func (p *Panel) ShownMonth(now time.Time) time.Time {
	loc := now.Location()
	if p == nil || p.month.IsZero() {
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	}
	return time.Date(p.month.Year(), p.month.Month(), 1, 0, 0, 0, 0, loc)
}

// ShiftMonth moves the calendar by delta months and refuses a month after
// the one now is in.
func (p *Panel) ShiftMonth(now time.Time, delta int) {
	if p == nil || delta == 0 {
		return
	}
	shown := p.ShownMonth(now)
	next := shown.AddDate(0, delta, 0)
	current := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	if next.After(current) {
		return
	}
	p.month = next
}

func (p *Panel) today(c *ui.Context, sessions []playtime.Session, now time.Time) {
	day := now
	games := playtime.GamesOn(sessions, day, now)
	if len(games) == 0 {
		latest, ok := playtime.LatestPlayedDay(sessions, now)
		if !ok {
			figure(c, "今天", 0)
			quiet(c, "今天还没有记录。")
			return
		}
		day = latest
		games = playtime.GamesOn(sessions, day, now)
	}
	var total, tallest int64
	for _, game := range games {
		total += game.Seconds
		if game.Seconds > tallest {
			tallest = game.Seconds
		}
	}
	figure(c, playtime.DayHeading(day, now), total)
	if len(games) == 0 {
		quiet(c, "今天还没有记录。")
		return
	}
	ui.Column(c).FillWidth().Gap(10).Children(func() {
		for _, game := range games {
			name := p.gameName(game)
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Start).Children(func() {
				p.cover(c, game.AppID, 36, 48)
				ui.Column(c).Grow(1).Gap(4).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
						ui.Text(c, name).FontSize(presentation.SizeQuickFine).FontWeight(600).TextColor(presentation.PanelInk).SingleLine().Grow(1)
						ui.Text(c, playtime.FormatSeconds(game.Seconds)).FontSize(presentation.SizeQuickFine).TextColor(presentation.PanelFaint).SingleLine()
					})
					for _, run := range game.Runs {
						ink := presentation.PanelFaint
						if run.Open {
							ink = presentation.ColorBrand
						}
						ui.Text(c, runLine(run)).FontSize(presentation.SizeQuickTiny).TextColor(ink).SingleLine()
					}
					share := int64(0)
					if tallest > 0 {
						share = game.Seconds * 100 / tallest
					}
					presentation.ProgressBar(c, share, presentation.ColorBrand, "", name)
				})
			})
		}
	})
}

func (p *Panel) week(c *ui.Context, sessions []playtime.Session, now time.Time) {
	days := playtime.WeekOf(sessions, now)
	var total, tallest int64
	for _, day := range days {
		total += day.Seconds
		if day.Seconds > tallest {
			tallest = day.Seconds
		}
	}
	var longest playtime.GameDay
	if len(days) == 7 {
		figure(c, days[0].Day.Format("1月2日")+" – "+days[6].Day.Format("1月2日"), total)
		games := playtime.GamesDuring(sessions, days[0].Day, days[0].Day.AddDate(0, 0, 7), now)
		if len(games) > 0 {
			longest = games[0]
		}
	}
	ui.Row(c).FillWidth().Gap(12).AlignItems(ui.End).Children(func() {
		ui.Row(c).Grow(1).AlignItems(ui.End).Children(func() {
			labels := []string{"日", "一", "二", "三", "四", "五", "六"}
			today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
			for i, day := range days {
				ui.Column(c).Grow(1).Gap(4).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Height(12).Center().Children(func() {
						if day.Seconds > 0 {
							ui.Text(c, shortSeconds(day.Seconds)).FontSize(presentation.SizeQuickTiny).TextColor(presentation.PanelFaint).SingleLine()
						}
					})
					fill := presentation.PanelLine
					height := float32(2)
					if day.Seconds > 0 {
						fill = presentation.PlayHeat2
						height = barHeight(day.Seconds, tallest)
						if day.Day.Equal(today) {
							fill = presentation.ColorBrand
						}
					}
					ui.Column(c).Height(48).Justify(ui.End).Children(func() {
						ui.Box(c).Width(8).Height(height).Radius(2).Background(fill)
					})
					ink := presentation.PanelFaint
					if day.Day.Equal(today) {
						ink = presentation.ColorBrand
					}
					ui.Text(c, labels[i]).FontSize(presentation.SizeQuickTiny).TextColor(ink).SingleLine()
				})
			}
		})
		if longest.AppID != "" {
			p.longest(c, longest)
		}
	})
}

func (p *Panel) longest(c *ui.Context, game playtime.GameDay) {
	name := p.gameName(game)
	ui.Column(c).Width(76).Gap(4).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "最久").FontSize(presentation.SizeQuickTiny).TextColor(presentation.PanelFaint).SingleLine()
		p.cover(c, game.AppID, 56, 72)
		ui.Text(c, name).FontSize(presentation.SizeQuickTiny).FontWeight(600).TextColor(presentation.PanelInk).SingleLine()
		ui.Text(c, playtime.FormatSeconds(game.Seconds)).FontSize(presentation.SizeQuickTiny).TextColor(presentation.PanelFaint).SingleLine()
	})
}

func (p *Panel) drawMonth(c *ui.Context, sessions []playtime.Session, now time.Time) {
	shown := p.ShownMonth(now)
	current := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	cells := playtime.MonthOf(sessions, shown, now)
	var total int64
	for _, cell := range cells {
		total += cell.Seconds
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	games := playtime.GamesDuring(sessions, shown, shown.AddDate(0, 1, 0), now)
	if len(games) > 3 {
		games = games[:3]
	}
	// Squares stay a fixed size. The total sits at the top of the right column,
	// and the games under it shrink so the column is no taller than the calendar.
	baseline := playtime.PlayedBaseline(cells)
	weeks := (len(cells) + 6) / 7
	coverW, coverH, rowGap := leaderCover(weeks, len(games))
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Start).Children(func() {
		ui.Column(c).Shrink(0).Gap(6).Children(func() {
			ui.Row(c).Gap(monthGap).Children(func() {
				for _, label := range []string{"日", "一", "二", "三", "四", "五", "六"} {
					ui.Box(c).Size(monthCell, 12).Center().Children(func() {
						ui.Text(c, label).FontSize(presentation.SizeQuickTiny).TextColor(presentation.PanelFaint).SingleLine()
					})
				}
			})
			for start := 0; start < len(cells); start += 7 {
				end := min(start+7, len(cells))
				ui.Row(c).Gap(monthGap).Children(func() {
					for _, cell := range cells[start:end] {
						dayCell(c, cell, today, baseline)
					}
					for range 7 - (end - start) {
						ui.Box(c).Size(monthCell, monthCell)
					}
				})
			}
		})
		if total == 0 {
			monthEmpty(c, weeks)
		} else {
			p.monthLeaders(c, total, games, coverW, coverH, rowGap)
		}
	})
	ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
		heatLegend(c)
		ui.Spacer(c)
		ui.Row(c).Shrink(0).AlignItems(ui.Center).Radius(6).Background(presentation.PanelField).Children(func() {
			monthStep(c, "‹", true, func() { p.ShiftMonth(now, -1) })
			ui.Text(c, shown.Format("2006年1月")).FontSize(presentation.SizeQuickFine).FontWeight(600).TextColor(presentation.PanelInk).SingleLine()
			monthStep(c, "›", shown.Before(current), func() { p.ShiftMonth(now, 1) })
		})
	})
}

func monthStep(c *ui.Context, label string, enabled bool, fn func()) {
	ink := presentation.PanelFaint
	button := ui.ButtonBase(c).Label(label).Radius(4)
	if enabled {
		ink = presentation.PanelInk
		button = button.Cursor(ui.CursorPointer)
		if button.Clicked() {
			fn()
		}
		switch {
		case button.Pressed():
			button = button.Background(presentation.PanelPress)
		case button.Hovered() || button.Focused():
			button = button.Background(presentation.PanelHover)
		}
	}
	button.Children(func() {
		ui.Box(c).Size(24, 24).Center().Children(func() {
			ui.Text(c, label).FontSize(presentation.SizeQuickName).FontWeight(600).TextColor(ink).SingleLine()
		})
	})
}

func (p *Panel) gameName(game playtime.GameDay) string {
	if p != nil && p.names != nil {
		if name := p.names[game.AppID]; name != "" {
			return name
		}
	}
	if game.Name != "" {
		return game.Name
	}
	if game.AppID == "" {
		return "游戏"
	}
	return "App " + game.AppID
}

func monthEmpty(c *ui.Context, weeks int) {
	ui.Column(c).Grow(1).Height(monthCalendarHeight(weeks)).Children(func() {
		ui.Box(c).Grow(4)
		ui.Row(c).FillWidth().Justify(ui.Center).Children(func() {
			ui.Text(c, "本月没有游戏时长统计").FontSize(presentation.SizeQuickFine).TextColor(presentation.PanelFaint).SingleLine()
		})
		ui.Box(c).Grow(5)
	})
}

func (p *Panel) monthLeaders(c *ui.Context, total int64, games []playtime.GameDay, coverW, coverH, rowGap float32) {
	ui.Column(c).Grow(1).Gap(rowGap).Children(func() {
		figure(c, "合计", total)
		for _, game := range games {
			name := p.gameName(game)
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				p.cover(c, game.AppID, coverW, coverH)
				ui.Column(c).Grow(1).Gap(1).Children(func() {
					title := ui.Text(c, name).FillWidth().FontSize(presentation.SizeQuickTiny).FontWeight(600).TextColor(presentation.PanelInk)
					if len(games) > 1 {
						title.SingleLine()
					}
					ui.Text(c, playtime.FormatSeconds(game.Seconds)).FontSize(presentation.SizeQuickTiny).TextColor(presentation.PanelFaint).SingleLine()
				})
			})
		}
	})
}

// leaderCover sizes the top-game rows so they fit under the month total and
// beside the calendar. Fewer games keep a larger cover; three short months shrink.
func leaderCover(weeks, games int) (width, height, gap float32) {
	const (
		figureH float32 = 40
		split   float32 = 8
		maxH    float32 = 28
		minH    float32 = 16
	)
	if games <= 0 {
		return 0, 0, 8
	}
	gap = 6
	if games > 2 {
		gap = 4
	}
	budget := monthCalendarHeight(weeks) - figureH - split
	row := maxH
	if games == 1 {
		row = 40
	}
	if games > 1 {
		fitted := (budget - gap*float32(games-1)) / float32(games)
		if fitted < row {
			row = fitted
		}
	} else if budget < row {
		row = budget
	}
	if row < minH {
		row = minH
	}
	if row > maxH && games > 1 {
		row = maxH
	}
	return row * 28 / 36, row, gap
}

func monthCalendarHeight(weeks int) float32 {
	if weeks < 1 {
		weeks = 1
	}
	const (
		weekday float32 = 12
		gap     float32 = 6
	)
	return weekday + float32(weeks)*monthCell + float32(weeks)*gap
}

func heatLegend(c *ui.Context) {
	ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "少").FontSize(presentation.SizeQuickTiny).TextColor(presentation.PanelFaint).SingleLine()
		for level := range 5 {
			ui.Box(c).Size(10, 10).Radius(2).Background(presentation.HeatColor(level))
		}
		ui.Text(c, "多").FontSize(presentation.SizeQuickTiny).TextColor(presentation.PanelFaint).SingleLine()
	})
}

func dayCell(c *ui.Context, cell playtime.CalendarDay, today time.Time, baseline int64) {
	if cell.Blank {
		ui.Box(c).Size(monthCell, monthCell)
		return
	}
	level := playtime.Heat(cell.Seconds, baseline)
	ink := presentation.PanelFaint
	if level >= 3 {
		ink = presentation.ColorOnBrand
	} else if cell.Seconds > 0 {
		ink = presentation.PanelInk
	}
	if cell.Day.Equal(today) && level < 3 {
		ink = presentation.ColorBrand
	}
	mark := ui.Box(c).Size(monthCell, monthCell).Radius(4).Background(presentation.HeatColor(level)).Center()
	if cell.Day.Equal(today) {
		mark.Border(1, presentation.ColorBrand)
	}
	mark.Children(func() {
		ui.Text(c, fmt.Sprintf("%d", cell.Day.Day())).FontSize(presentation.SizeQuickTiny).TextColor(ink).SingleLine()
	})
}

func (p *Panel) cover(c *ui.Context, appID string, width, height float32) {
	box := ui.Box(c).Size(width, height).Shrink(0).Radius(4).Background(presentation.PanelField)
	box.Children(func() {
		bitmap := p.covers[appID]
		if bitmap == nil {
			return
		}
		ui.Image(c, bitmap).Fill().Fit(ui.Cover).Radius(4)
	})
}

func spanSwitch(c *ui.Context, selected *int) {
	labels := []string{"日", "周", "月"}
	parts := ui.SegmentedBase(c, selected, len(labels))
	parts.Track.Shrink(0).Gap(2)
	parts.Track.Children(func() {
		for index, label := range labels {
			segment := parts.Segment(index).Padding(float32(2), float32(8)).Radius(4)
			ink := presentation.PanelFaint
			weight := 500
			if index == *selected {
				ink = presentation.PanelInk
				weight = 600
				segment.Background(presentation.PanelField)
			}
			switch {
			case segment.Pressed():
				segment.Background(presentation.PanelPress)
			case segment.Hovered() || segment.Focused():
				segment.Background(presentation.PanelHover)
			}
			segment.Children(func() {
				ui.Text(c, label).FontSize(presentation.SizeQuickFine).FontWeight(weight).TextColor(ink).SingleLine()
			})
		}
	})
}

func figure(c *ui.Context, label string, seconds int64) {
	ui.Column(c).Gap(2).Children(func() {
		ui.Text(c, label).FontSize(presentation.SizeQuickFine).TextColor(presentation.PanelFaint).SingleLine()
		ui.Text(c, playtime.FormatSeconds(seconds)).FontSize(presentation.SizeQuickNumber).FontWeight(700).TextColor(presentation.PanelNumber).SingleLine()
	})
}

func quiet(c *ui.Context, text string) {
	ui.Text(c, text).FontSize(presentation.SizeQuickFine).TextColor(presentation.PanelFaint)
}

func runLine(run playtime.Run) string {
	start := run.StartedAt.Format("15:04")
	if run.Open {
		return start + " 起 · " + playtime.FormatSeconds(run.Seconds)
	}
	end := run.EndedAt.Format("15:04")
	if run.UntilMidnight {
		end = "24:00"
	}
	return start + "–" + end + " · " + playtime.FormatSeconds(run.Seconds)
}

func barHeight(seconds, tallest int64) float32 {
	const full = 48
	if seconds <= 0 || tallest <= 0 {
		return 2
	}
	height := full * float32(seconds) / float32(tallest)
	if height < 4 {
		height = 4
	}
	return height
}

func shortSeconds(seconds int64) string {
	if seconds <= 0 {
		return ""
	}
	if seconds < 60 {
		return playtime.FormatSeconds(seconds)
	}
	if seconds < 3600 {
		return fmt.Sprintf("%d分", seconds/60)
	}
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	if minutes == 0 {
		return fmt.Sprintf("%d时", hours)
	}
	return fmt.Sprintf("%d:%02d", hours, minutes)
}
