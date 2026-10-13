package app

import (
	"errors"
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	"boku/internal/tools"
	"boku/internal/usecase"
)

// card draws the panels the quick window shows. There is one today; it takes
// the first panel the service lists, so a second one only needs this loop
// widened.
//
// Every state it can be in is drawn here: a card that failed says why, and
// never shows 0 in place of an answer it did not get. That rule is the whole
// point of the card.
//
// The panel is a section of the window, the same shape the settings window
// gives each of its own: a title line, then what the panel knows.
func (s *State) card(c *ui.Context) {
	listed := s.panelService.List()
	if len(listed) == 0 {
		ui.Text(c, "还没有面板。").
			FontSize(sizeBody).
			TextColor(colorTextMuted)
		return
	}
	view := s.panelService.View(listed[0].ID)

	ui.Column(c).FillWidth().Gap(float32(8)).Children(func() {
		s.cardTitle(c, view)
		s.cardNumbers(c, view)
	})
}

// cardTitle is the panel's first line: what it is called on the left, and on
// the right how old its numbers are together with the way to fetch them
// again — the age of a number and the action that changes it belong next to
// each other, at the head of the section.
func (s *State) cardTitle(c *ui.Context, view usecase.View) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
		ui.Text(c, view.Panel.Title).
			FontSize(sizeTitle).
			FontWeight(600).
			TextColor(colorTextSoft).
			SingleLine()
		ui.Spacer(c)
		freshness(c, view)
		// One action: fetching a panel that failed and fetching a panel
		// that is merely old are the same request.
		if button := s.refreshButton(c, view); button.Clicked() && !view.Loading {
			s.refreshAsync(true)
		}
	})
}

// spinPeriod is how long the refresh mark takes to turn once, and spinKey
// tells its animation apart from any other on the element.
const (
	spinPeriod = 900 * time.Millisecond
	spinKey    = "refresh"
)

// refreshButton is the panel's one action, drawn as the mark for fetching
// again rather than as a word: the line it sits on is already carrying the
// age of the numbers, and a mark says "fetch this again" without taking a
// corner of the line for it. What it is, for a pointer that rests on it and
// for assistive technology, is its label.
//
// While a fetch is running the mark turns. That is the whole of what the
// click answers with: the numbers stay as they are until an answer arrives,
// and a still mark beside them would leave the user wondering whether the
// click landed.
func (s *State) refreshButton(c *ui.Context, view usecase.View) ui.Element {
	label := "刷新"
	if view.Err != nil && view.HasSnapshot {
		// One action, two situations: the reason the last attempt failed is
		// written under the numbers, and this is what trying again is
		// called when there is something to try again about.
		label = "重试"
	}
	// The mark is built here and filled in below, inside the button's own
	// children: the refresh action draws a mark that moves, which the
	// design system's icon button — a still mark — does not. Everything
	// else about it, down to the face under the pointer, is the design's.
	var mark ui.Element
	button := ui.ButtonBase(c).Label(label).Tooltip(label).
		Radius(radiusField).
		Padding(2).
		Cursor(ui.CursorPointer)
	switch {
	case button.Pressed():
		button = button.Background(colorActive)
	case button.Hovered() || button.Focused():
		button = button.Background(colorHover)
	}
	button.Children(func() {
		mark = ui.Icon(c, s.refreshMark).Size(float32(16), float32(16)).Shrink(0)
		if button.Hovered() || button.Focused() {
			mark.TextColor(colorText)
		} else {
			mark.TextColor(colorTextMuted)
		}
	})
	// Turning the mark is what the click answers while a fetch runs: a still
	// mark beside unchanged numbers would leave the user wondering whether
	// the click landed.
	if view.Loading && mark.Valid() {
		mark.Rotate(mark.Loop(spinKey, spinPeriod, ui.Linear) * 360)
	}
	return button
}

// cardNumbers draws what the card knows, and nothing it does not. An empty
// state — no credential, no snapshot, a failure — says what to do about it
// instead of drawing numbers; a snapshot is drawn as its headline, and the
// smaller numbers stay folded away until the user asks for them.
func (s *State) cardNumbers(c *ui.Context, view usecase.View) {
	switch {
	case view.NeedsCredentials():
		ui.Column(c).FillWidth().Gap(float32(10)).Children(func() {
			notice(c, notePlain, "还没有配置凭据。配置后即可读取面板数据。")
			actionRow(c, "去配置", s.openSettings)
		})
	case view.Err != nil && !view.HasSnapshot:
		ui.Column(c).FillWidth().Gap(float32(10)).Children(func() {
			notice(c, noteDanger, friendlyError(view.Err))
			actionRow(c, "重试", func() { s.refreshAsync(true) })
		})
	case !view.HasSnapshot:
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
			ui.Spinner(c)
			ui.Text(c, "正在获取…").FontSize(sizeBody).TextColor(colorTextMuted)
		})
	default:
		balance(c, view, &s.detailsOpen)
	}

	// The numbers above stay on screen when a refresh fails: an old answer
	// is worth more than none, as long as it is labelled as old. The window
	// says why it failed and offers to try again on the title line.
	if view.Err != nil && view.HasSnapshot {
		notice(c, noteDanger, friendlyError(view.Err))
	}
	if message := view.Snapshot.Message; message != "" {
		notice(c, notePlain, message)
	}
}

// balance draws the panel's numbers: the headline metric large, and the rest
// folded away under it — a panel that shows every number it has is a panel
// nobody reads at a glance.
//
// The disclosure sits on the headline's own line, at its right end: the label
// says what the big number is ("总余额"), the control beside the number says
// there is more behind it.
func balance(c *ui.Context, view usecase.View, detailsOpen *bool) {
	if len(view.Metrics) == 0 {
		return
	}
	headline := view.Metrics[0]
	remaining := view.Metrics[1:]
	if len(remaining) == 0 {
		balanceHeadline(c, view, headline, nil)
		hairline(c)
		return
	}

	// What is folded away is built first, so the rows can be drawn after the
	// line they are opened from.
	rows := make([]metricRow, 0, len(remaining))
	for _, metric := range remaining {
		rows = append(rows, metricRow{
			label: metric.Label,
			value: formatMetric(view.Snapshot, metric),
		})
	}

	ui.Column(c).FillWidth().Children(func() {
		balanceHeadline(c, view, headline, detailsOpen)
		if !*detailsOpen {
			return
		}
		// The rows line up with the headline they belong to, not with the
		// disclosure at the end of its line.
		metricRows(c, rows)
	})
	hairline(c)
}

// disclosure is a control that opens and closes the rows under a headline: a
// small chevron and the label, on a face of their own, which the pointer,
// Enter and Space toggle. It is drawn on the line of whatever it discloses
// rather than above the content it opens, which ui.Collapsible cannot do —
// its trigger and its panel are one column.
//
// It wears a face because it is the only thing on its line that can be
// clicked: the label beside it is what the big number is, and a control that
// looks like the text it sits next to is a control nobody finds.
func disclosure(c *ui.Context, label string, open *bool) {
	button := ui.ButtonBase(c).Focusable().Cursor(ui.CursorPointer).
		Role(ui.RoleDisclosure).Expanded(*open).
		Radius(999).Padding(float32(2), float32(8)).
		AlignItems(ui.Center).Gap(float32(4)).Translate(float32(8), 0)
	switch {
	case button.Pressed():
		button = button.Background(colorActive)
	case button.Hovered():
		button = button.Background(colorHover)
	}
	button.Children(func() {
		ui.Text(c, label).FontSize(sizeSmall).TextColor(colorTextMuted).SingleLine()
		disclosureArrow(c, turn(*open))
	})
	if button.Clicked() {
		*open = !*open
	}
}

// turn is how far the chevron of a disclosure is turned: pointing right while
// closed, down while open.
func turn(open bool) float32 {
	if open {
		return 90
	}
	return 0
}

// balanceHeadline is the panel's big number: what it is called, and at the
// end of that line the control that opens the rest, when there is a rest to
// open.
//
// The number itself is the way to the provider's own detail page, when the
// provider names one: a balance is worth seeing in full where it is kept,
// and the number is the thing the user is looking at when that thought
// arrives. It wears a pointer and the accent color under the pointer, so
// that it is not a hidden action.
func balanceHeadline(c *ui.Context, view usecase.View, headline domain.Metric, detailsOpen *bool) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
		ui.Text(c, headline.Label).FontSize(sizeSmall).TextColor(colorTextMuted).SingleLine()
		ui.Spacer(c)
		if detailsOpen != nil {
			disclosure(c, "明细", detailsOpen)
		}
	})
	number := formatMetric(view.Snapshot, headline)
	if view.UsageURL == "" {
		ui.Text(c, number).FontSize(28).FontWeight(600).SingleLine()
		return
	}
	// The number is its own row, so that the column does not stretch the
	// link across the window: the click target is the number, not the line.
	ui.Row(c).FillWidth().Children(func() {
		button := ui.ButtonBase(c).
			Label("查看" + headline.Label + "明细").
			Tooltip("在官网查看明细").
			Cursor(ui.CursorPointer)
		button.Children(func() {
			text := ui.Text(c, number).FontSize(28).FontWeight(600).SingleLine()
			if button.Hovered() || button.Focused() {
				text.TextColor(colorBrand)
			}
		})
		if button.Clicked() {
			// The system opens the link; nothing here waits for it, and a
			// browser that does not open is not this window's failure.
			c.OpenURL(view.UsageURL)
		}
	})
}

// disclosureArrow draws a chevron: pointing right, turned by deg degrees
// clockwise.
func disclosureArrow(c *ui.Context, deg float32) {
	ui.Box(c).Size(float32(12), float32(12)).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy, d := r.X+r.W/2, r.Y+r.H/2, r.W/8
		s, co := math.Sincos(float64(deg) * math.Pi / 180)
		sin, cos := float32(s), float32(co)
		at := func(x, y float32) (float32, float32) {
			return cx + x*cos - y*sin, cy + x*sin + y*cos
		}
		x1, y1 := at(-d, -2*d)
		x2, y2 := at(d, 0)
		x3, y3 := at(-d, 2*d)
		var path ui.Path
		path.MoveTo(x1, y1)
		path.LineTo(x2, y2)
		path.LineTo(x3, y3)
		p.StrokePath(&path, 1.5, colorTextMuted)
	})
}

// actionRow puts one action at the left of its own row, where a column's
// stretching does not turn it into a full-width button. It is a secondary
// action — the form's main one is the other button beside it — so it keeps
// the face of one rather than being a word the user has to hunt for.
func actionRow(c *ui.Context, label string, run func()) {
	ui.Row(c).FillWidth().Gap(float32(8)).Children(func() {
		if button := designButton(c, label, buttonSecondary); button.Clicked() && run != nil {
			run()
		}
		ui.Spacer(c)
	})
}

// freshness says how old the numbers are, or that they are being fetched. A
// number from a failed attempt is marked as old in the color of a warning,
// because the number is still worth more than none.
func freshness(c *ui.Context, view usecase.View) {
	switch {
	case view.HasSnapshot && view.Stale:
		ui.Text(c, "旧数据 · "+clock(view.FetchedAt)).
			FontSize(sizeSmall).
			TextColor(colorWarning).
			SingleLine()
	case view.HasSnapshot:
		ui.Text(c, "已更新 "+clock(view.FetchedAt)).
			FontSize(sizeSmall).
			TextColor(colorTextMuted).
			SingleLine()
	}
}

// metricRows draws the small numbers under a section's headline: one line
// each, label on the left and value on the right, close together so that a
// section stays a section instead of turning into a page.
func metricRows(c *ui.Context, rows []metricRow) {
	ui.Column(c).FillWidth().Gap(float32(2)).Children(func() {
		for _, row := range rows {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, row.label).FontSize(sizeSmall).TextColor(colorTextMuted)
				ui.Spacer(c)
				ui.Text(c, row.value).FontSize(sizeSmall).SingleLine()
			})
		}
	})
}

// metricRow is one line of a section: what a number is, and the number.
type metricRow struct {
	label string
	value string
}

// formatMetric shows one observation. A metric the snapshot does not have
// becomes a dash, never a zero: a zero is an answer, a dash is the absence
// of one.
func formatMetric(snapshot domain.Snapshot, metric domain.Metric) string {
	observation, ok := snapshot.Observation(metric.Key)
	if !ok {
		return "—"
	}
	number, ok := observation.Value.Number()
	if !ok {
		return "—"
	}
	currency, err := snapshot.MetricCurrency(metric)
	if err != nil {
		return "—"
	}
	return tools.FormatMoney(number, currency, metric.Precision)
}

// friendlyError says what went wrong in the interface's own words. The
// errors themselves are built so that they never carry a credential.
func friendlyError(err error) string {
	var bad *domain.BadResponseError
	switch {
	case err == nil:
		return ""
	case errors.Is(err, domain.ErrCredentialMissing):
		return "还没有配置凭据。"
	case errors.Is(err, domain.ErrProviderUnauthorized):
		return "凭据被拒绝，请检查是否仍然有效。"
	case errors.Is(err, domain.ErrProviderRateLimited):
		return "请求太频繁，请稍后再试。"
	case errors.Is(err, domain.ErrProviderTimeout):
		return "请求超时，请检查网络。"
	case errors.Is(err, domain.ErrProviderUnavailable):
		return "服务暂时不可用，请稍后再试。"
	case errors.Is(err, domain.ErrProviderNotFound):
		return "找不到这个数据源。"
	case errors.As(err, &bad):
		if bad.Reason != "" {
			return "服务返回了无法识别的数据：" + bad.Reason
		}
		return "服务返回了无法识别的数据。"
	default:
		return err.Error()
	}
}

// clock is the time a snapshot was fetched, as a card shows it.
func clock(at time.Time) string {
	if at.IsZero() {
		return "—"
	}
	return at.Local().Format("15:04")
}
