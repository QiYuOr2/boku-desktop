package app

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	"boku/internal/kit"
	"boku/internal/panel"
)

// card draws the panels the quick window shows: one block each, with the line
// between two of them that says where one ends. A panel draws itself by what
// its headline is — a proportion is a bar, a figure is a number — so what a
// provider reports decides the shape, and nothing here names a vendor.
//
// Every state it can be in is drawn here: a panel that failed says why, and
// never shows 0 in place of an answer it did not get. That rule is the whole
// point of the card.
//
// The panel is a section of the window, the same shape the settings window
// gives each of its own: a title line, then what the panel knows.
func (s *State) card(c *ui.Context) {
	panels := s.panelService.List()
	if len(panels) == 0 {
		quickBlock(c, func() {
			ui.Text(c, "还没有面板。").FontSize(sizeBody).TextColor(colorTextMuted)
		})
		return
	}
	for index, listed := range panels {
		if index > 0 {
			// Between two blocks, and only there: the line says where one
			// panel ends, and it keeps the same edge the blocks keep.
			quickDivider(c)
		}
		s.panelBlock(c, s.panelService.View(listed.ID))
	}
}

// panelBlock draws one panel: the design's room around the block, its head,
// and what it knows.
func (s *State) panelBlock(c *ui.Context, view panel.View) {
	quickBlock(c, func() {
		ui.Column(c).FillWidth().Gap(float32(8)).Children(func() {
			s.panelHead(c, view)
			if quotaPanel(view) {
				s.quotaBody(c, view)
				return
			}
			s.cardNumbers(c, view)
		})
	})
}

// panelHead is a block's first line: the mark of what it is about, its name,
// and at the right the age of the numbers with the way to fetch them again.
//
// A quota block carries one more line under it — what the account is and when
// its window resets, and at the far end how much of the window is gone —
// because both are longer than the room left beside a 320-point name once the
// age and the refresh are on the same line; put there, they would all be
// ellipsised.
func (s *State) panelHead(c *ui.Context, view panel.View) {
	ui.Column(c).FillWidth().Gap(float32(2)).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(float32(8)).Children(func() {
				quickTile(c, s.providerMark(view.Panel.ProviderID))
				ui.Text(c, view.Panel.Title).
					FontSize(sizeQuickName).
					FontWeight(600).
					TextColor(panelInk).
					SingleLine()
			})
			ui.Spacer(c)
			freshness(c, view)
			if button := s.refreshButton(c, view.Loading, view.Err != nil && view.HasSnapshot); button.Clicked() && !view.Loading {
				s.refreshPanelAsync(view.Panel.ID, true)
			}
		})
		if !quotaPanel(view) {
			return
		}
		line, used := quotaAccount(view), ""
		if share, ok := quotaUsed(view); ok {
			used = "已用 " + strconv.FormatInt(share, 10) + "%"
		}
		if line == "" && used == "" {
			return
		}
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
			if line != "" {
				cardNote(c, line)
			}
			ui.Spacer(c)
			if used != "" {
				// The same number the bar is drawn from, at the end of the
				// line it belongs to: a bar alone does not say how much of
				// it is gone.
				ui.Text(c, used).
					FontSize(sizeQuickFine).
					TextColor(panelFaint).
					SingleLine()
			}
		})
	})
}

// The tile of a block is the provider's own color, and the drawing on it is
// white: the DeepSeek blue, and the black OpenAI signs its work with. A
// provider with no color of its own — or no drawing — gets the placeholder
// pair below instead, so that a block is never one nobody can tell apart.
var (
	deepseekBlue = ui.Hex("#4D6BFE")
	// deepseekBar is the same brand blue, raised for a bar drawn on a dark
	// page: the tile keeps the provider's own blue, but a bar and the small
	// number over it are read against the page rather than inside the tile.
	deepseekBar = ui.Hex("#7C93FF")
	codexBlack  = ui.Hex("#111111")
)

// providerMark is how a block's mark is drawn: the provider's own drawing, the
// tile it sits on, the color it is drawn in and the color of a bar in that
// block. The drawings are the application's own assets; the tiles are the
// providers' own colors, which is what the design sheet draws a block's mark
// on.
//
// A provider the repository has no drawing for — Codex today, whose owner's
// mark is not in it — is drawn with the application's placeholder, so that its
// block is still told apart by its color rather than by a logo that would be
// somebody else's.
func (s *State) providerMark(providerID string) panelMark {
	switch providerID {
	case "deepseek":
		bar := deepseekBlue
		if s.dark {
			bar = deepseekBar
		}
		return panelMark{svg: s.deepseekMark, tile: deepseekBlue, ink: colorOnMark, bar: bar}
	case "codex":
		// The tile stays the provider's black; the bar cannot. A bar is
		// drawn on the page rather than in the tile, and black on a dark
		// page is not a bar at all. In the dark appearance it is drawn in
		// the page's own light ink, which is the same mark the tile makes
		// when the tile is black.
		bar := codexBlack
		if s.dark {
			bar = colorTextSoft
		}
		return panelMark{svg: s.gaugeMark, tile: codexBlack, ink: colorOnMark, bar: bar}
	default:
		return panelMark{svg: s.gaugeMark, tile: colorMuted, ink: colorTextMuted, bar: colorTextMuted}
	}
}

// quotaPanel reports whether a panel's headline is a proportion rather than a
// figure: a percentage is read as a shape before it is read as a number, so
// such a panel is drawn as one. A second quota panel needs no code here.
func quotaPanel(view panel.View) bool {
	if len(view.Metrics) == 0 {
		return false
	}
	headline := view.Metrics[0]
	return headline.Unit == domain.UnitPercent && headline.ValueType == domain.ValueTypeNumber
}

// quota is what a proportion panel's snapshot says.
type quota struct {
	remaining int64
	hasLeft   bool
	resetsIn  int64
	hasReset  bool
	plan      string
}

// readQuota reads a quota snapshot by what its metrics are rather than by what
// they are called: a percentage is what is left, a duration is when it resets,
// and a text is the plan the account is on.
func readQuota(view panel.View) quota {
	var found quota
	for _, metric := range view.Metrics {
		observation, ok := view.Snapshot.Observation(metric.Key)
		if !ok {
			continue
		}
		switch {
		case metric.Unit == domain.UnitPercent && !found.hasLeft:
			if number, ok := observation.Value.Number(); ok {
				found.remaining, found.hasLeft = number, true
			}
		case metric.ValueType == domain.ValueTypeDuration && !found.hasReset:
			if seconds, ok := observation.Value.DurationSeconds(); ok {
				found.resetsIn, found.hasReset = seconds, true
			}
		case metric.ValueType == domain.ValueTypeText && found.plan == "":
			if text, ok := observation.Value.Text(); ok {
				found.plan = strings.TrimSpace(text)
			}
		}
	}
	return found
}

// quotaAccount is what the account is and when its window resets, as one line:
// "Plus · 下次重置 10-14 20:00". A plan the service did not name is left out
// rather than guessed at.
func quotaAccount(view panel.View) string {
	found := readQuota(view)
	parts := make([]string, 0, 2)
	if plan := planLabel(found.plan); plan != "" {
		parts = append(parts, plan)
	}
	if found.hasReset {
		if found.resetsIn <= 0 {
			parts = append(parts, "即将重置")
		} else {
			parts = append(parts, "下次重置 "+resetClock(view.Snapshot.ObservedAt, found.resetsIn))
		}
	}
	return strings.Join(parts, " · ")
}

// quotaUsed is the share of the window already spent, and whether there is an
// answer at all.
//
// The panel's own metric is what is *left* — that is what the provider
// reports and what §7 of the provider contract fixes for both ends — and the window
// shows the complement: what a person wants to know about a quota they are
// spending is how much of it is gone. Nothing here is invented: a share the
// answer does not have is not a share, so the second result says which it is.
func quotaUsed(view panel.View) (int64, bool) {
	found := readQuota(view)
	if !found.hasLeft {
		return 0, false
	}
	return 100 - found.remaining, true
}

// resetClock is when a window resets, as the panel reads it: a date and a
// time, counted from the moment the numbers were taken. The view has no clock
// of its own, so a window that would have reset while nobody was looking
// still reads as the moment it was going to.
func resetClock(observedAt time.Time, resetsIn int64) string {
	if observedAt.IsZero() || resetsIn <= 0 {
		return "—"
	}
	return observedAt.Add(time.Duration(resetsIn) * time.Second).Local().Format("01-02 15:04")
}

// planLabel names an account's tier the way a person reads it: a service that
// spells it "plus" is shown as "Plus". A value the application does not know
// is shown exactly as it came — never hidden, never guessed at.
func planLabel(plan string) string {
	trimmed := strings.TrimSpace(plan)
	switch strings.ToLower(trimmed) {
	case "free", "plus", "pro", "business", "enterprise", "team", "edu":
		return strings.ToUpper(trimmed[:1]) + strings.ToLower(trimmed[1:])
	default:
		return trimmed
	}
}

// quotaBody draws a quota: the bar and the number it stands for, or — when
// there is nothing to draw — what to do about it.
//
// The bar fills with what is *used*, not with what is left: the panel's metric
// is the remaining share (§7 of the provider contract), and a bar that grows as the
// window is spent is the one a person reads without thinking. A failure, an
// empty answer and a window nothing is known about draw no fill at all, which
// is what keeps "0% used" an answer rather than a missing one.
func (s *State) quotaBody(c *ui.Context, view panel.View) {
	switch {
	case view.NeedsCredentials():
		ui.Column(c).FillWidth().Gap(float32(10)).Children(func() {
			notice(c, notePlain, "还没有登录：这个额度来自本机已经登录好的命令行工具。")
			s.loginAction(c, view)
		})
	case view.Err != nil && !view.HasSnapshot:
		ui.Column(c).FillWidth().Gap(float32(10)).Children(func() {
			notice(c, noteDanger, friendlyError(view.Err))
			actionRow(c, "重试", func() { s.refreshPanelAsync(view.Panel.ID, true) })
		})
	case !view.HasSnapshot:
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
			ui.Spinner(c)
			ui.Text(c, "正在获取…").FontSize(sizeBody).TextColor(colorTextMuted)
		})
	default:
		used, known := quotaUsed(view)
		label := "已用 —"
		if known {
			label = "已用 " + strconv.FormatInt(used, 10) + "%"
		}
		// The bar takes the block's own colors: the design draws a bar in
		// the tint of the block it belongs to, which is how a quota reads as
		// part of that block rather than as a decoration of the window.
		mark := s.providerMark(view.Panel.ProviderID)
		progressBar(c, used, mark.bar, label, "已用额度")
	}

	// The bar above stays on screen when a refresh fails: an old answer is
	// worth more than none, as long as it is labelled as old.
	if view.Err != nil && view.HasSnapshot {
		notice(c, noteDanger, friendlyError(view.Err))
	}
	if message := view.Snapshot.Message; message != "" {
		notice(c, notePlain, message)
	}
	if s.loginNotice != "" {
		notice(c, notePlain, s.loginNotice)
	}
}

// loginAction is the way to the login a quota panel is waiting for: the tool's
// own login, started in a terminal, or its documentation when it cannot be
// started here.
func (s *State) loginAction(c *ui.Context, view panel.View) {
	name := s.providerName(view.Panel.ProviderID)
	ui.Row(c).FillWidth().Gap(float32(8)).Children(func() {
		if button := designButton(c, "登录 "+name, buttonPrimary); button.Clicked() {
			s.startCredentialLogin(view.Panel.ProviderID, name)
		}
		if button := designButton(c, "查看说明", buttonSecondary); button.Clicked() {
			if descriptor, err := s.panelService.Descriptor(view.Panel.ProviderID); err == nil && descriptor.DocsURL != "" {
				c.OpenURL(descriptor.DocsURL)
			}
		}
		ui.Spacer(c)
	})
}

// providerName is what a provider is called, for the words the window puts
// around an action: the name belongs to the provider rather than to this file.
func (s *State) providerName(providerID string) string {
	if descriptor, err := s.panelService.Descriptor(providerID); err == nil && strings.TrimSpace(descriptor.DisplayName) != "" {
		return descriptor.DisplayName
	}
	return "这个数据源"
}

// startCredentialLogin starts the login a credential comes from, and says what
// happened: a tool that is not installed, or a platform with no terminal, is
// reported where the button is rather than swallowed.
//
// The work runs off the main thread. Finding a CLI on macOS means asking the
// user's login shell, which can take a moment; a click handler must return at
// once and never wait on another process.
func (s *State) startCredentialLogin(providerID, name string) {
	s.loginNotice = ""
	s.Refresh()
	panels := s.panelService
	window := s.panel
	go func() {
		err := panels.StartCredentialLogin(providerID)
		if window == nil {
			return
		}
		window.Update(func() {
			if err != nil {
				s.loginNotice = loginFailure(err, name)
			} else {
				s.loginNotice = "已在终端里打开登录；完成后点刷新。"
			}
			s.Refresh()
		})
	}()
}

// loginFailure says why a login could not be started, in words that name the
// service the button was about.
func loginFailure(err error, name string) string {
	switch {
	case errors.Is(err, domain.ErrLoginCommandMissing):
		return "没有找到 " + name + " 的命令行工具：先安装它，再回来点这里。"
	case errors.Is(err, domain.ErrLoginUnsupported):
		return "这个平台没有可以打开登录的终端：请在终端里手动完成 " + name + " 的登录。"
	default:
		return friendlyError(err)
	}
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
func (s *State) refreshButton(c *ui.Context, loading, retry bool) ui.Element {
	label := "刷新"
	if retry {
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
		Radius(float32(4)).
		Padding(float32(2)).
		Cursor(ui.CursorPointer)
	switch {
	case button.Pressed():
		button = button.Background(panelPress)
	case button.Hovered() || button.Focused():
		button = button.Background(panelHover)
	}
	button.Children(func() {
		mark = ui.Icon(c, s.refreshMark).Size(float32(quickMarkSize), float32(quickMarkSize)).Shrink(0)
		if button.Hovered() || button.Focused() {
			mark.TextColor(panelInk)
		} else {
			mark.TextColor(panelFaint)
		}
	})
	// Turning the mark is what the click answers while a fetch runs: a still
	// mark beside unchanged numbers would leave the user wondering whether
	// the click landed.
	if loading && mark.Valid() {
		mark.Rotate(mark.Loop(spinKey, spinPeriod, ui.Linear) * 360)
	}
	return button
}

// cardNumbers draws what the card knows, and nothing it does not. An empty
// state — no credential, no snapshot, a failure — says what to do about it
// instead of drawing numbers; a snapshot is drawn as its headline, and the
// smaller numbers stay folded away until the user asks for them.
func (s *State) cardNumbers(c *ui.Context, view panel.View) {
	switch {
	case view.NeedsCredentials():
		ui.Column(c).FillWidth().Gap(float32(10)).Children(func() {
			notice(c, notePlain, "还没有配置凭据。配置后即可读取面板数据。")
			actionRow(c, "去配置", s.openSettings)
		})
	case view.Err != nil && !view.HasSnapshot:
		ui.Column(c).FillWidth().Gap(float32(10)).Children(func() {
			notice(c, noteDanger, friendlyError(view.Err))
			actionRow(c, "重试", func() { s.refreshPanelAsync(view.Panel.ID, true) })
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

// balance draws the panel's numbers: the headline metric, and the rest folded
// away under it — a panel that shows every number it has is a panel nobody
// reads at a glance.
//
// The design puts the label over the number and the disclosure at the far end
// of that same row: the label says what the big number is ("总余额"), and the
// control beside it says there is more behind it.
func balance(c *ui.Context, view panel.View, detailsOpen *bool) {
	if len(view.Metrics) == 0 {
		return
	}
	headline := view.Metrics[0]
	rows := make([]metricRow, 0, len(view.Metrics)-1)
	for _, metric := range view.Metrics[1:] {
		rows = append(rows, metricRow{
			label: metric.Label,
			value: formatMetric(view.Snapshot, metric),
		})
	}
	if len(rows) == 0 {
		// Nothing to fold away: a disclosure with nothing behind it is a
		// control that lies.
		detailsOpen = nil
	}

	ui.Column(c).FillWidth().Children(func() {
		balanceHeadline(c, view, headline, detailsOpen)
		if detailsOpen == nil || !*detailsOpen {
			return
		}
		metricRows(c, rows)
	})
}

// balanceHeadline is the panel's big number with what it is called over it,
// and at the end of that row the control that opens the rest, when there is a
// rest to open.
func balanceHeadline(c *ui.Context, view panel.View, headline domain.Metric, detailsOpen *bool) {
	ui.Row(c).FillWidth().AlignItems(ui.End).Children(func() {
		ui.Column(c).Gap(float32(2)).Children(func() {
			ui.Text(c, headline.Label).
				FontSize(sizeQuickFine).
				TextColor(panelFaint).
				SingleLine()
			balanceNumber(c, view, headline)
		})
		ui.Spacer(c)
		if detailsOpen != nil {
			disclosure(c, "明细", detailsOpen)
		}
	})
}

// balanceNumber is the number itself, and the way to the provider's own page
// for it when the provider names one: a balance is worth seeing in full where
// it is kept, and the number is the thing the user is looking at when that
// thought arrives. It wears a pointer and the brand green under the pointer,
// so that it is not a hidden action.
func balanceNumber(c *ui.Context, view panel.View, headline domain.Metric) {
	number := formatMetric(view.Snapshot, headline)
	if view.UsageURL == "" {
		ui.Text(c, number).
			FontSize(sizeQuickNumber).
			FontWeight(700).
			TextColor(panelNumber).
			SingleLine()
		return
	}
	button := ui.ButtonBase(c).
		Label("查看" + headline.Label + "明细").
		Tooltip("在官网查看明细").
		Cursor(ui.CursorPointer)
	button.Children(func() {
		text := ui.Text(c, number).
			FontSize(sizeQuickNumber).
			FontWeight(700).
			SingleLine()
		if button.Hovered() || button.Focused() {
			text.TextColor(colorBrand)
		} else {
			text.TextColor(panelNumber)
		}
	})
	if button.Clicked() {
		// The system opens the link; nothing here waits for it, and a
		// browser that does not open is not this window's failure.
		c.OpenURL(view.UsageURL)
	}
}

// disclosure is a control that opens and closes the rows under a headline: the
// label and a small chevron, which the pointer, Enter and Space toggle. It is
// drawn on the row of whatever it discloses rather than above the content it
// opens, which ui.Collapsible cannot do — its trigger and its panel are one
// column.
//
// The design draws it as a quiet word at the far end of the row: it is the
// only thing there that can be clicked, and it takes the brand green under the
// pointer so that it is found without shouting beside the number it belongs
// to.
func disclosure(c *ui.Context, label string, open *bool) {
	button := ui.ButtonBase(c).Focusable().Cursor(ui.CursorPointer).
		Role(ui.RoleDisclosure).Expanded(*open).
		Radius(float32(4)).
		Padding(float32(2), float32(6)).
		AlignItems(ui.Center).Gap(float32(2))
	switch {
	case button.Pressed():
		button = button.Background(panelPress)
	case button.Hovered() || button.Focused():
		button = button.Background(panelField)
	}
	button.Children(func() {
		ink := panelFaint
		if button.Hovered() || button.Focused() {
			ink = colorBrand
		}
		ui.Text(c, label).FontSize(sizeQuickFine).TextColor(ink).SingleLine()
		disclosureArrow(c, turn(*open), ink)
	})
	if button.Clicked() {
		*open = !*open
	}
}

// cardDisclosure is disclosure for the translation card, which reports the
// click instead of flipping a flag: the card is as tall as what is in it, so
// whoever opens it has to ask for the frames that measure it again.
//
// It is the same control — a chevron and a label on a face of their own — and
// it is not drawn at the end of a line, so it does not wear the offset the
// panels' headlines give theirs.
func cardDisclosure(c *ui.Context, label string, open bool) bool {
	button := ui.ButtonBase(c).Focusable().Cursor(ui.CursorPointer).
		Role(ui.RoleDisclosure).Expanded(open).
		Radius(999).Padding(float32(2), float32(8)).
		AlignItems(ui.Center).Gap(float32(4))
	switch {
	case button.Pressed():
		button = button.Background(colorActive)
	case button.Hovered():
		button = button.Background(colorHover)
	}
	button.Children(func() {
		ui.Text(c, label).FontSize(sizeSmall).TextColor(colorTextMuted).SingleLine()
		disclosureArrow(c, turn(open), colorTextMuted)
	})
	return button.Clicked()
}

// turn is how far the chevron of a disclosure is turned: pointing right while
// closed, down while open.
func turn(open bool) float32 {
	if open {
		return 90
	}
	return 0
}

// disclosureArrow draws a chevron: pointing right, turned by deg degrees
// clockwise, in the ink of the line it sits on.
func disclosureArrow(c *ui.Context, deg float32, ink ui.Color) {
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
		p.StrokePath(&path, 1.5, ink)
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

// freshness says how old the numbers are. The design writes the clock alone
// beside the refresh action; a number from a failed attempt is marked as old
// in the color of a warning, because the number is still worth more than none.
func freshness(c *ui.Context, view panel.View) {
	switch {
	case view.HasSnapshot && view.Stale:
		ui.Text(c, "旧数据 · "+clock(view.FetchedAt)).
			FontSize(sizeQuickFine).
			TextColor(colorWarning).
			SingleLine()
	case view.HasSnapshot:
		ui.Text(c, clock(view.FetchedAt)).
			FontSize(sizeQuickFine).
			TextColor(panelFaint).
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
				ui.Text(c, row.label).FontSize(sizeQuickFine).TextColor(panelFaint)
				ui.Spacer(c)
				ui.Text(c, row.value).FontSize(sizeQuickFine).TextColor(panelInk).SingleLine()
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
	return kit.FormatMoney(number, currency, metric.Precision)
}

// friendlyError says what went wrong in the interface's own words. The
// errors themselves are built so that they never carry a credential.
func friendlyError(err error) string {
	var bad *domain.BadResponseError
	var imageFailure *domain.ImageTranslationError
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
	case errors.Is(err, domain.ErrScreenRecordingDenied):
		return "需要屏幕录制权限才能读取截图。"
	case errors.As(err, &imageFailure):
		switch imageFailure.Code {
		case domain.ImageFailureNoReadableText:
			return "图片中没有可识别的文字。"
		case domain.ImageFailureUnsupported:
			return "模型无法处理这张图片。"
		case domain.ImageFailureRefused:
			return "模型拒绝处理这张图片。"
		}
		return "模型无法完成这张图片的翻译。"
	case errors.As(err, &bad):
		if bad.Reason != "" {
			return "服务返回了无法识别的数据：" + bad.Reason
		}
		return "服务返回了无法识别的数据。"
	case errors.Is(err, domain.ErrProviderBadResponse):
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
