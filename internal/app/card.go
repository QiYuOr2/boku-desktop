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
// The panel is one block among the window's blocks. A divider closes the
// block, the way the footer's divider closes the content, so a second panel
// is a second block under the first with its own divider between them.
func (s *State) card(c *ui.Context) {
	t := c.Theme()
	listed := s.panelService.List()
	if len(listed) == 0 {
		ui.Text(c, "还没有面板。").
			FontSize(t.Rem(0.9)).
			TextColor(t.TextMuted)
		return
	}
	view := s.panelService.View(listed[0].ID)

	ui.Column(c).FillWidth().Gap(t.Space(2)).Children(func() {
		block(c, "", func() {
			s.cardTitle(c, view)
			s.cardNumbers(c, view)
		})
		ui.Divider(c)
	})
}

// cardTitle is the panel's first line: what it is called on the left, and on
// the right how old its numbers are together with the way to fetch them
// again — the age of a number and the action that changes it belong next to
// each other, at the head of the block.
func (s *State) cardTitle(c *ui.Context, view usecase.View) {
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
		ui.Text(c, view.Panel.Title).FontSize(t.Rem(0.95)).Bold().SingleLine()
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
	t := c.Theme()
	label := "刷新"
	if view.Err != nil && view.HasSnapshot {
		// One action, two situations: the reason the last attempt failed is
		// written under the numbers, and this is what trying again is
		// called when there is something to try again about.
		label = "重试"
	}
	button := ui.ButtonBase(c).Label(label).Tooltip(label).Cursor(ui.CursorPointer).
		Radius(t.Radius).
		Padding(t.Space(0.5))
	button.Children(func() {
		mark := ui.Icon(c, s.refreshMark).Size(t.Space(4), t.Space(4)).Shrink(0)
		if button.Hovered() || button.Focused() {
			mark.TextColor(t.Text)
		} else {
			mark.TextColor(t.TextMuted)
		}
		if view.Loading {
			mark.Rotate(mark.Loop(spinKey, spinPeriod, ui.Linear) * 360)
		}
	})
	return button
}

// cardNumbers draws what the card knows, and nothing it does not. An empty
// state — no credential, no snapshot, a failure — says what to do about it
// instead of drawing numbers; a snapshot is drawn as its headline, and the
// smaller numbers stay folded away until the user asks for them.
func (s *State) cardNumbers(c *ui.Context, view usecase.View) {
	t := c.Theme()
	switch {
	case view.NeedsCredentials():
		ui.Column(c).FillWidth().Gap(t.Space(2)).Children(func() {
			ui.Text(c, "还没有配置凭据。").
				FontSize(t.Rem(0.85))
			ui.Text(c, "配置后即可读取面板数据。").
				FontSize(t.Rem(0.78)).
				TextColor(t.TextMuted)
			actionRow(c, "去配置", s.openSettings)
		})
	case view.Err != nil && !view.HasSnapshot:
		ui.Column(c).FillWidth().Gap(t.Space(2)).Children(func() {
			ui.Text(c, friendlyError(view.Err)).
				FontSize(t.Rem(0.85)).
				TextColor(t.Danger).
				Wrap()
			actionRow(c, "重试", func() { s.refreshAsync(true) })
		})
	case !view.HasSnapshot:
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
			ui.Spinner(c)
			ui.Text(c, "正在获取…").FontSize(t.Rem(0.85)).TextColor(t.TextMuted)
		})
	default:
		balance(c, view, &s.detailsOpen)
	}

	// The numbers above stay on screen when a refresh fails: an old answer
	// is worth more than none, as long as it is labelled as old. The window
	// says why it failed and offers to try again on the title line.
	if view.Err != nil && view.HasSnapshot {
		ui.Text(c, friendlyError(view.Err)).
			FontSize(t.Rem(0.78)).
			TextColor(t.Danger).
			Wrap()
	}
	if message := view.Snapshot.Message; message != "" {
		ui.Text(c, message).
			FontSize(t.Rem(0.78)).
			TextColor(t.TextMuted).
			Wrap()
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
	t := c.Theme()
	if len(view.Metrics) == 0 {
		return
	}
	headline := view.Metrics[0]
	remaining := view.Metrics[1:]
	if len(remaining) == 0 {
		balanceHeadline(c, view, headline, nil)
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

	ui.Column(c).FillWidth().Gap(t.Space(2)).Children(func() {
		balanceHeadline(c, view, headline, detailsOpen)
		if !*detailsOpen {
			return
		}
		// The rows line up with the headline they belong to, not with the
		// disclosure at the end of its line.
		metricRows(c, rows)
	})
}

// disclosure is a control that opens and closes the rows under a headline: a
// small chevron and the label, which the pointer, Enter and Space toggle. It
// is drawn on the line of whatever it discloses rather than above the content
// it opens, which ui.Collapsible cannot do — its trigger and its panel are
// one column.
func disclosure(c *ui.Context, label string, open *bool) {
	t := c.Theme()
	button := ui.ButtonBase(c).Focusable().Cursor(ui.CursorPointer).
		Role(ui.RoleDisclosure).Expanded(*open).
		Radius(t.Radius).Padding(t.Space(0.5), t.Space(1)).
		AlignItems(ui.Center).Gap(t.Space(1))
	button.Children(func() {
		disclosureArrow(c, turn(*open))
		ui.Text(c, label).FontSize(t.Rem(0.78)).TextColor(t.TextMuted).SingleLine()
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
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
		ui.Text(c, headline.Label).FontSize(t.Rem(0.78)).TextColor(t.TextMuted)
		ui.Spacer(c)
		if detailsOpen != nil {
			disclosure(c, "明细", detailsOpen)
		}
	})
	number := formatMetric(view.Snapshot, headline)
	if view.UsageURL == "" {
		ui.Text(c, number).FontSize(t.Rem(1.8)).Bold()
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
			text := ui.Text(c, number).FontSize(t.Rem(1.8)).Bold()
			if button.Hovered() || button.Focused() {
				text.TextColor(t.Accent)
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
	t := c.Theme()
	ui.Box(c).Size(t.Space(4), t.Space(4)).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
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
		p.StrokePath(&path, 1.5, t.TextMuted)
	})
}

// actionRow puts one action at the left of its own row, where a column's
// stretching does not turn it into a full-width button.
func actionRow(c *ui.Context, label string, action func()) {
	ui.Row(c).FillWidth().Children(func() {
		if button := textButton(c, label); button.Clicked() && action != nil {
			action()
		}
		ui.Spacer(c)
	})
}

// freshness says how old the numbers are, or that they are being fetched.
func freshness(c *ui.Context, view usecase.View) {
	t := c.Theme()
	switch {
	case view.HasSnapshot && view.Stale:
		ui.Text(c, "旧数据 · "+clock(view.FetchedAt)).
			FontSize(t.Rem(0.75)).
			TextColor(t.Warning).
			SingleLine()
	case view.HasSnapshot:
		ui.Text(c, "已更新 "+clock(view.FetchedAt)).
			FontSize(t.Rem(0.75)).
			TextColor(t.TextMuted).
			SingleLine()
	}
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
