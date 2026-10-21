package ui

import (
	"boku/internal/clipboard"
	"boku/internal/domain"
	"boku/internal/presentation"
	"github.com/egoist/mygo/ui"
	"strconv"
	"time"
)

// clipboardRegion is the row under the blocks: the mark, the content, and the
// way to the history. It is one row high, so that the quick window stays a
// glance rather than becoming a list.
//
// The design draws it as a field of its own rather than as another block: 12
// from each side of the window, 14 points high inside, on a stone face with a
// stone s.design.Hairline — it is a value the desktop handed over, not a panel, and the
// two are told apart by their shape as well as by their place.
//
// With nothing read yet — monitoring is off, or nothing has been copied — the
// row is not drawn at all, not even the field.
//
// The mark and the way in do not shrink: a long value loses its own tail to
// the ellipsis, and what the row is and what it offers stay readable.
func (s *State) QuickView(c *ui.Context, design presentation.ClipboardDesign) {
	s.design = design
	content, ok := s.clipboardLatest()

	if content.Display == "" || !ok {
		return
	}

	ui.Column(c).FillWidth().
		Padding(float32(4), float32(12), float32(10), float32(12)).
		Children(func() {
			ui.Row(c).FillWidth().
				Padding(float32(6), float32(10)).
				Radius(float32(8)).
				Border(1, s.design.PanelBorder).
				Background(s.design.PanelField).
				AlignItems(ui.Center).
				Gap(float32(8)).
				Children(func() {
					// The mark wears the field's own ink: it is a label, and
					// the word it replaced was drawn in the same place.
					ui.Icon(c, s.design.Mark).Size(float32(14), float32(14)).Shrink(0).TextColor(s.design.PanelFaint)
					ui.Text(c, content.Display).FontSize(s.design.SizeSmall).TextColor(s.design.PanelBody).SingleLine()
					ui.Spacer(c)
					if button := s.quickHistoryButton(c); button.Clicked() {
						s.openHistory()
					}
				})
		})
}

// historyButton is the way from the clipboard row to the history: a word,
// because the row it sits on is about a value rather than about actions.
func (s *State) quickHistoryButton(c *ui.Context) ui.Element {
	return s.design.WordButton(c, "历史")
}

// clipboardLatest is what the tool last saw, and whether it saw anything.
func (s *State) clipboardLatest() (domain.ClipRecord, bool) {
	if s.clipboard == nil {
		return domain.ClipRecord{}, false
	}
	return s.clipboard.Latest()
}

// clipboardHistorySize is the record range the user chose: the history keeps
// that many entries, so it is also all the page has to list.
func (s *State) clipboardHistorySize() int {
	if s.clipboard == nil {
		return clipboard.DefaultHistorySize
	}
	return s.clipboard.Settings().KeepsHistory()
}

// clipboardHistory is the recorded copies, newest first, at most the record
// range the user chose.
func (s *State) clipboardHistory() []domain.ClipboardEntry {
	if s.clipboard == nil {
		return nil
	}
	return s.clipboard.History(s.clipboardHistorySize())
}

// clipboardHistoryError is why the stored history could not be read, if it
// could not.
func (s *State) clipboardHistoryError() error {
	if s.clipboard == nil {
		return nil
	}
	return s.clipboard.HistoryError()
}

// openHistory shows the history window, creating it the first time. It reads
// the clipboard once as it opens: the user asked to see the clipboard, and
// that is a read the monitoring switch was never about.
//
// Nothing here waits. The window is created and shown, and the reading it
// asks for happens on its own goroutine: a clipboard read can wait on
// whatever else is holding the clipboard, and that wait must not be spent
// with the window drawn but empty. When the reading is done it asks for a
// frame, so the list appears as soon as there is something to put in it.
// historyView lists the recorded copies: the line that says what the list is
// and what a click does, and the list itself.
//
// It reads what the tool already holds, and copies nothing while drawing.
func (s *State) HistoryView(c *ui.Context, design presentation.ClipboardDesign) {
	s.design = design
	s.design.WindowFrame(c).Children(func() {
		// What the list is, and what a click does, on its own line above it:
		// the list scrolls under this, so it stays where it is. The window's
		// own title bar names the window, so this does not say it again.
		ui.Row(c).FillWidth().Shrink(0).
			Padding(s.design.PageEdge/2, s.design.PageEdge).
			AlignItems(ui.Center).
			Children(func() {
				ui.Text(c, "最近 "+strconv.Itoa(s.clipboardHistorySize())+" 条").
					FontSize(s.design.SizeSection).FontWeight(600).TextColor(s.design.ColorTextBody).SingleLine()
				ui.Spacer(c)
				ui.Text(c, "点击即复制").FontSize(s.design.SizeSmall).TextColor(s.design.ColorTextMuted).SingleLine()
			})
		s.design.Hairline(c)

		if err := s.clipboardHistoryError(); err != nil {
			s.design.Content(c, true, func() {
				s.design.Inset(c, func() {
					s.design.Notice(c, true, "读不到上次的历史："+s.design.FriendlyError(err))
					s.design.Notice(c, false, "新复制的仍然会记下来。")
				})
			})
			return
		}

		entries := s.clipboardHistory()
		if len(entries) == 0 {
			s.design.Content(c, true, func() {
				s.design.Inset(c, func() {
					ui.Text(c, "还没有记录。").
						FontSize(s.design.SizeBody).
						TextColor(s.design.ColorTextMuted)
					ui.Text(c, "复制一段内容，再打开这里。").
						FontSize(s.design.SizeSmall).
						TextColor(s.design.ColorTextMuted)
				})
			})
			return
		}

		list := ui.Scroll(c).FillWidth().Grow(1)
		list.Children(func() {
			// The rows keep the page's margin and the room between two of
			// them, the same margin and the same line the sections of any
			// other window use: a list is a list wherever it is drawn.
			ui.Column(c).FillWidth().Children(func() {
				for index, entry := range entries {
					if index > 0 {
						s.design.RowDivider(c)
					}
					s.historyRow(c, entry)
				}
			})
		})
	})
}

// historyRow is one recorded copy: the tile that says what it is, the copy
// itself, how long ago it was taken, and — for a copy whose kind is worth a
// word — the kind's own badge in the kind's own color.
//
// The whole row is the button: a list of ten copies is a list of ten things
// to click, not a list with a small target at the end of each line.
//
// The row shows what it is doing: a face under the pointer, a deeper one
// while the press is held, and the pressed face kept for a moment after the
// copy, so that a click is answered on the row itself rather than only on
// the clipboard.
func (s *State) historyRow(c *ui.Context, entry domain.ClipboardEntry) {
	tile, ink, mark := s.historyKind(entry)
	label := historyLabel(entry)
	row := ui.ButtonBase(c).FillWidth().
		Padding(float32(10), float32(12)).
		AlignItems(ui.Center).
		Gap(float32(12))
	justCopied := entry.ID == s.copiedEntry
	// The kind's own ink washed over the row. Ten percent reads as a tint on
	// a white page; on a dark one it needs a little more to be seen at all,
	// and this is the one state the row keeps after the click rather than one
	// the pointer explains.
	wash := float32(0.10)
	if s.design.Dark {
		wash = 0.20
	}
	switch {
	case justCopied:
		row.Background(ink.Alpha(wash))
	case row.Pressed():
		row.Background(s.design.ColorActive)
	case row.Hovered() || row.Focused():
		row.Background(s.design.ColorRowAlt)
	}
	row.Children(func() {
		s.design.KindTile(c, mark, tile, ink)
		ui.Text(c, entry.Line()).
			FontSize(s.design.SizeBody).
			TextColor(s.inkText(entry)).
			Font(fontOf(entry)).
			SingleLine().
			Grow(1).
			MinWidth(0)
		ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(float32(6)).Children(func() {
			if entry.Note != "" {
				// What the reading noticed about itself: the copy is still
				// usable, and the interface should not pretend it was
				// complete. It is the copy's own words, drawn as stored.
				s.design.Notice(c, true, entry.Note)
			}
			if label != "" {
				s.design.KindBadge(c, tile, ink, label)
			}
			ui.Text(c, copiedClock(entry.CopiedAt)).
				FontSize(s.design.SizeSmall).
				TextColor(s.design.ColorTextMuted).
				SingleLine()
		})
	})
	if row.Clicked() {
		s.copyEntry(entry)
	}
}

// inkText is the color a copy's own text is drawn in: a link is drawn in the
// color of a link, and everything else in the list's body color.
func (s *State) inkText(entry domain.ClipboardEntry) ui.Color {
	if entry.Kind == domain.ClipKindURL {
		return s.design.ColorBlueInk
	}
	return s.design.ColorTextBody
}

// fontOf is the family a copy's own text is drawn in: a copy that is code or a
// color value is read character by character, and a monospace face is what
// makes that possible.
func fontOf(entry domain.ClipboardEntry) string {
	if entry.Kind == domain.ClipKindCode {
		return "monospace"
	}
	return ""
}

// historyKind is the tile and the ink a copy is drawn with — and, for a copy
// that is an image, the same tile with the picture mark — so a list of ten
// copies is read by shape and color before it is read word by word.
func (s *State) historyKind(entry domain.ClipboardEntry) (tile, ink ui.Color, mark *ui.SVG) {
	switch entry.Kind {
	case domain.ClipKindImage:
		return s.design.ColorVioletTile, s.design.ColorVioletInk, s.design.HistoryImage
	case domain.ClipKindURL:
		return s.design.ColorBlueTile, s.design.ColorBlueInk, s.design.HistoryLink
	case domain.ClipKindCode:
		return s.design.ColorStoneTile, s.design.ColorStoneInk, s.design.HistoryCode
	case domain.ClipKindJSON:
		return s.design.ColorEmeraldTile, s.design.ColorEmeraldInk, s.design.HistoryText
	case domain.ClipKindFile:
		return s.design.ColorAmberTile, s.design.ColorAmberInk, s.design.HistoryText
	default:
		return s.design.ColorSkyTile, s.design.ColorSkyInk, s.design.HistoryText
	}
}

// historyLabel is the one word a list shows beside a copy: what it is, and for
// an image how much of it there is — an image is the kind whose copying
// depends on what the platform handed over.
func historyLabel(entry domain.ClipboardEntry) string { return Label(entry) }

// copiedClock is when a copy happened, as a list shows it: the time for
// today, the date for anything older.
func copiedClock(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	local := at.Local()
	now := time.Now()
	if local.Year() == now.Year() && local.YearDay() == now.YearDay() {
		return local.Format("15:04")
	}
	return local.Format("01-02 15:04")
}
