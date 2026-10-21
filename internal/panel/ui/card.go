package ui

import (
	"boku/internal/presentation"
	"errors"

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
func (s *State) QuickView(c *ui.Context, dark bool) {
	s.dark = dark
	panels := s.panelService.List()
	if len(panels) == 0 {
		presentation.QuickBlock(c, func() {
			ui.Text(c, "还没有面板。").FontSize(presentation.SizeBody).TextColor(presentation.ColorTextMuted)
		})
		return
	}
	for index, listed := range panels {
		if index > 0 {
			// Between two blocks, and only there: the line says where one
			// panel ends, and it keeps the same edge the blocks keep.
			presentation.QuickDivider(c)
		}
		s.panelBlock(c, s.panelService.View(listed.ID))
	}
}

func (s *State) ProviderMark(id string) presentation.PanelMark { return s.providerMark(id) }

// Block renders one panel chosen by the Shell's layout.
func (s *State) Block(c *ui.Context, panelID string, dark bool) {
	s.dark = dark
	c.Key(panelID)
	s.panelBlock(c, s.panelService.View(panelID))
}

// panelBlock draws one panel: the design's room around the block, its head,
// and what it knows.
func (s *State) panelBlock(c *ui.Context, view panel.View) {
	presentation.QuickBlock(c, func() {
		ui.Column(c).FillWidth().Gap(float32(8)).Children(func() {
			s.panelHead(c, view)
			if IsQuota(view) {
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
				presentation.QuickTile(c, s.providerMark(view.Panel.ProviderID))
				ui.Text(c, view.Panel.Title).
					FontSize(presentation.SizeQuickName).
					FontWeight(600).
					TextColor(presentation.PanelInk).
					SingleLine()
			})
			ui.Spacer(c)
			freshness(c, view)
			if button := presentation.RefreshButton(c, s.refreshMark, view.Loading, view.Err != nil && view.HasSnapshot); button.Clicked() && !view.Loading {
				s.RefreshPanelAsync(view.Panel.ID, true)
			}
		})
		if !IsQuota(view) {
			return
		}
		line, used := Account(view), ""
		if share, ok := Used(view); ok {
			used = "已用 " + strconv.FormatInt(share, 10) + "%"
		}
		if line == "" && used == "" {
			return
		}
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
			if line != "" {
				presentation.CardNote(c, line)
			}
			ui.Spacer(c)
			if used != "" {
				// The same number the bar is drawn from, at the end of the
				// line it belongs to: a bar alone does not say how much of
				// it is gone.
				ui.Text(c, used).
					FontSize(presentation.SizeQuickFine).
					TextColor(presentation.PanelFaint).
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
// A provider without an asset uses the application's neutral gauge placeholder.
func (s *State) providerMark(providerID string) presentation.PanelMark {
	switch providerID {
	case "deepseek":
		bar := deepseekBlue
		if s.dark {
			bar = deepseekBar
		}
		return presentation.PanelMark{SVG: s.deepseekMark, Tile: deepseekBlue, Ink: presentation.ColorOnMark, Bar: bar}
	case "codex":
		// The monochrome tile reverses with the theme. The bar is drawn on
		// the page and keeps its own contrasting ink.
		bar := codexBlack
		if s.dark {
			bar = presentation.ColorTextSoft
		}
		return presentation.PanelMark{SVG: s.codexMark, Tile: presentation.CodexTile, Ink: presentation.CodexInk, Bar: bar, Scale: 4.0 / 3.0}
	default:
		return presentation.PanelMark{SVG: s.gaugeMark, Tile: presentation.ColorMuted, Ink: presentation.ColorTextMuted, Bar: presentation.ColorTextMuted}
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
			presentation.Notice(c, presentation.NotePlain, "还没有登录：这个额度来自本机已经登录好的命令行工具。")
			s.loginAction(c, view)
		})
	case view.Err != nil && !view.HasSnapshot:
		ui.Column(c).FillWidth().Gap(float32(10)).Children(func() {
			presentation.Notice(c, presentation.NoteDanger, presentation.FriendlyError(view.Err))
			actionRow(c, "重试", func() { s.RefreshPanelAsync(view.Panel.ID, true) })
		})
	case !view.HasSnapshot:
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
			ui.Spinner(c)
			ui.Text(c, "正在获取…").FontSize(presentation.SizeBody).TextColor(presentation.ColorTextMuted)
		})
	default:
		used, known := Used(view)
		label := "已用 —"
		if known {
			label = "已用 " + strconv.FormatInt(used, 10) + "%"
		}
		// The bar takes the block's own colors: the design draws a bar in
		// the tint of the block it belongs to, which is how a quota reads as
		// part of that block rather than as a decoration of the window.
		mark := s.providerMark(view.Panel.ProviderID)
		presentation.ProgressBar(c, used, mark.Bar, label, "已用额度")
	}

	// The bar above stays on screen when a refresh fails: an old answer is
	// worth more than none, as long as it is labelled as old.
	if view.Err != nil && view.HasSnapshot {
		presentation.Notice(c, presentation.NoteDanger, presentation.FriendlyError(view.Err))
	}
	if message := view.Snapshot.Message; message != "" {
		presentation.Notice(c, presentation.NotePlain, message)
	}
	if s.loginNotice != "" {
		presentation.Notice(c, presentation.NotePlain, s.loginNotice)
	}
}

// loginAction is the way to the login a quota panel is waiting for: the tool's
// own login, started in a terminal, or its documentation when it cannot be
// started here.
func (s *State) loginAction(c *ui.Context, view panel.View) {
	name := s.providerName(view.Panel.ProviderID)
	ui.Row(c).FillWidth().Gap(float32(8)).Children(func() {
		if button := presentation.DesignButton(c, "登录 "+name, presentation.ButtonPrimary); button.Clicked() {
			s.startCredentialLogin(view.Panel.ProviderID, name)
		}
		if button := presentation.DesignButton(c, "查看说明", presentation.ButtonSecondary); button.Clicked() {
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
// loginFailure says why a login could not be started, in words that name the
// service the button was about.
func loginFailure(err error, name string) string {
	switch {
	case errors.Is(err, domain.ErrLoginCommandMissing):
		return "没有找到 " + name + " 的命令行工具：先安装它，再回来点这里。"
	case errors.Is(err, domain.ErrLoginUnsupported):
		return "这个平台没有可以打开登录的终端：请在终端里手动完成 " + name + " 的登录。"
	default:
		return presentation.FriendlyError(err)
	}
}

// cardNumbers draws what the card knows, and nothing it does not. An empty
// state — no credential, no snapshot, a failure — says what to do about it
// instead of drawing numbers; a snapshot is drawn as its headline, and the
// smaller numbers stay folded away until the user asks for them.
func (s *State) cardNumbers(c *ui.Context, view panel.View) {
	switch {
	case view.NeedsCredentials():
		ui.Column(c).FillWidth().Gap(float32(10)).Children(func() {
			presentation.Notice(c, presentation.NotePlain, "还没有配置凭据。配置后即可读取面板数据。")
			actionRow(c, "去配置", s.openSettings)
		})
	case view.Err != nil && !view.HasSnapshot:
		ui.Column(c).FillWidth().Gap(float32(10)).Children(func() {
			presentation.Notice(c, presentation.NoteDanger, presentation.FriendlyError(view.Err))
			actionRow(c, "重试", func() { s.RefreshPanelAsync(view.Panel.ID, true) })
		})
	case !view.HasSnapshot:
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
			ui.Spinner(c)
			ui.Text(c, "正在获取…").FontSize(presentation.SizeBody).TextColor(presentation.ColorTextMuted)
		})
	default:
		balance(c, view, &s.detailsOpen)
	}

	// The numbers above stay on screen when a refresh fails: an old answer
	// is worth more than none, as long as it is labelled as old. The window
	// says why it failed and offers to try again on the title line.
	if view.Err != nil && view.HasSnapshot {
		presentation.Notice(c, presentation.NoteDanger, presentation.FriendlyError(view.Err))
	}
	if message := view.Snapshot.Message; message != "" {
		presentation.Notice(c, presentation.NotePlain, message)
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
				FontSize(presentation.SizeQuickFine).
				TextColor(presentation.PanelFaint).
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
			FontSize(presentation.SizeQuickNumber).
			FontWeight(700).
			TextColor(presentation.PanelNumber).
			SingleLine()
		return
	}
	button := ui.ButtonBase(c).
		Label("查看" + headline.Label + "明细").
		Tooltip("在官网查看明细").
		Cursor(ui.CursorPointer)
	button.Children(func() {
		text := ui.Text(c, number).
			FontSize(presentation.SizeQuickNumber).
			FontWeight(700).
			SingleLine()
		if button.Hovered() || button.Focused() {
			text.TextColor(presentation.ColorBrand)
		} else {
			text.TextColor(presentation.PanelNumber)
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
		button = button.Background(presentation.PanelPress)
	case button.Hovered() || button.Focused():
		button = button.Background(presentation.PanelField)
	}
	button.Children(func() {
		ink := presentation.PanelFaint
		if button.Hovered() || button.Focused() {
			ink = presentation.ColorBrand
		}
		ui.Text(c, label).FontSize(presentation.SizeQuickFine).TextColor(ink).SingleLine()
		presentation.DisclosureArrow(c, turn(*open), ink)
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
		button = button.Background(presentation.ColorActive)
	case button.Hovered():
		button = button.Background(presentation.ColorHover)
	}
	button.Children(func() {
		ui.Text(c, label).FontSize(presentation.SizeSmall).TextColor(presentation.ColorTextMuted).SingleLine()
		presentation.DisclosureArrow(c, turn(open), presentation.ColorTextMuted)
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
// actionRow puts one action at the left of its own row, where a column's
// stretching does not turn it into a full-width button. It is a secondary
// action — the form's main one is the other button beside it — so it keeps
// the face of one rather than being a word the user has to hunt for.
func actionRow(c *ui.Context, label string, run func()) {
	ui.Row(c).FillWidth().Gap(float32(8)).Children(func() {
		if button := presentation.DesignButton(c, label, presentation.ButtonSecondary); button.Clicked() && run != nil {
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
		ui.Text(c, "旧数据 · "+Clock(view.FetchedAt)).
			FontSize(presentation.SizeQuickFine).
			TextColor(presentation.ColorWarning).
			SingleLine()
	case view.HasSnapshot:
		ui.Text(c, Clock(view.FetchedAt)).
			FontSize(presentation.SizeQuickFine).
			TextColor(presentation.PanelFaint).
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
				ui.Text(c, row.label).FontSize(presentation.SizeQuickFine).TextColor(presentation.PanelFaint)
				ui.Spacer(c)
				ui.Text(c, row.value).FontSize(presentation.SizeQuickFine).TextColor(presentation.PanelInk).SingleLine()
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

// clock is the time a snapshot was fetched, as a card shows it.
func Clock(at time.Time) string {
	if at.IsZero() {
		return "—"
	}
	return at.Local().Format("15:04")
}
