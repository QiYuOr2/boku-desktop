package app

import (
	"context"
	"strconv"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
)

// The clipboard tool has two faces. The quick window shows one line — what is
// on the clipboard right now — under the panel and above the footer, which is
// where a line about something else belongs. The history window lists the
// copies before it, newest first, and a click puts one back on the clipboard.
//
// The line is one line: the tool's own reading decides what it says, so a
// file is "[name.ext]", an image is "[图片]" and rich text is the text it
// shows. Nothing here shortens anything while drawing — that was decided when
// the reading was recorded, which is what keeps a frame cheap.
const (
	historyWidth = 420
	// historyHeight is the room the list wants: a windowful of copies, with
	// the head and the two quiet lines under it always in place.
	historyHeight = 640
)

// clipboardRegion is the row under the blocks: the mark, the content, and the
// way to the history. It is one row high, so that the quick window stays a
// glance rather than becoming a list.
//
// The design draws it as a field of its own rather than as another block: 12
// from each side of the window, 14 points high inside, on a stone face with a
// stone hairline — it is a value the desktop handed over, not a panel, and the
// two are told apart by their shape as well as by their place.
//
// With nothing read yet — monitoring is off, or nothing has been copied — the
// row is not drawn at all, not even the field.
//
// The mark and the way in do not shrink: a long value loses its own tail to
// the ellipsis, and what the row is and what it offers stay readable.
func (s *State) clipboardRegion(c *ui.Context) {
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
				Border(1, panelBorder).
				Background(panelField).
				AlignItems(ui.Center).
				Gap(float32(8)).
				Children(func() {
					// The mark wears the field's own ink: it is a label, and
					// the word it replaced was drawn in the same place.
					ui.Icon(c, s.clipboardMark).Size(float32(14), float32(14)).Shrink(0).TextColor(panelFaint)
					ui.Text(c, content.Display).FontSize(sizeSmall).TextColor(panelBody).SingleLine()
					ui.Spacer(c)
					if button := historyButton(c); button.Clicked() {
						s.openHistory()
					}
				})
		})
}

// historyButton is the way from the clipboard row to the history: a word,
// because the row it sits on is about a value rather than about actions.
func historyButton(c *ui.Context) ui.Element {
	return wordButton(c, "历史")
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
		return domain.ClipboardDefaultHistorySize
	}
	return s.clipboard.Settings().HistorySize()
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
func (s *State) openHistory() {
	if s.clipboard == nil {
		return
	}
	if s.historyWindow == nil {
		window := mygo.NewWindow(mygo.WindowOptions{
			Title:     "剪贴板历史",
			Width:     historyWidth,
			Height:    historyHeight,
			MinWidth:  340,
			MinHeight: 320,
			Content:   ui.View(s.historyView),
		})
		s.historyWindow = window
		window.OnClosed(func() {
			if s.historyWindow == window {
				s.historyWindow = nil
			}
		})
	}
	window := s.historyWindow
	if !s.clipboard.PolledRecently() {
		s.clipboard.Open(context.Background())
	}
	window.Show()
	window.Focus()
	// One frame for the list as it stands: the history that was already in
	// memory is worth showing before any clipboard read answers.
	window.Update(func() {})
}

// historyView lists the recorded copies: the line that says what the list is
// and what a click does, and the list itself.
//
// It reads what the tool already holds, and copies nothing while drawing.
func (s *State) historyView(c *ui.Context) {
	useDesign(c)
	windowFrame(c).Children(func() {
		// What the list is, and what a click does, on its own line above it:
		// the list scrolls under this, so it stays where it is. The window's
		// own title bar names the window, so this does not say it again.
		ui.Row(c).FillWidth().Shrink(0).
			Padding(pageEdge/2, pageEdge).
			AlignItems(ui.Center).
			Children(func() {
				ui.Text(c, "最近 "+strconv.Itoa(s.clipboardHistorySize())+" 条").
					FontSize(sizeSection).FontWeight(600).TextColor(colorTextBody).SingleLine()
				ui.Spacer(c)
				ui.Text(c, "点击即复制").FontSize(sizeSmall).TextColor(colorTextMuted).SingleLine()
			})
		hairline(c)

		if err := s.clipboardHistoryError(); err != nil {
			content(c, true, func() {
				inset(c, func() {
					notice(c, noteDanger, "读不到上次的历史："+friendlyError(err))
					notice(c, notePlain, "新复制的仍然会记下来。")
				})
			})
			return
		}

		entries := s.clipboardHistory()
		if len(entries) == 0 {
			content(c, true, func() {
				inset(c, func() {
					ui.Text(c, "还没有记录。").
						FontSize(sizeBody).
						TextColor(colorTextMuted)
					ui.Text(c, "复制一段内容，再打开这里。").
						FontSize(sizeSmall).
						TextColor(colorTextMuted)
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
						rowDivider(c)
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
	switch {
	case justCopied:
		row.Background(ink.Alpha(0.10))
	case row.Pressed():
		row.Background(colorActive)
	case row.Hovered() || row.Focused():
		row.Background(colorRowAlt)
	}
	row.Children(func() {
		kindTile(c, mark, tile, ink)
		ui.Text(c, entry.Line()).
			FontSize(sizeBody).
			TextColor(inkText(entry)).
			Font(fontOf(entry)).
			SingleLine().
			Grow(1).
			MinWidth(0)
		ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(float32(6)).Children(func() {
			if entry.Note != "" {
				// What the reading noticed about itself: the copy is still
				// usable, and the interface should not pretend it was
				// complete. It is the copy's own words, drawn as stored.
				notice(c, noteDanger, entry.Note)
			}
			if label != "" {
				kindBadge(c, tile, ink, label)
			}
			ui.Text(c, copiedClock(entry.CopiedAt)).
				FontSize(sizeSmall).
				TextColor(colorTextMuted).
				SingleLine()
		})
	})
	if row.Clicked() {
		s.copyEntry(entry)
	}
}

// inkText is the color a copy's own text is drawn in: a link is drawn in the
// color of a link, and everything else in the list's body color.
func inkText(entry domain.ClipboardEntry) ui.Color {
	if entry.Kind == domain.ClipKindURL {
		return colorBlueInk
	}
	return colorTextBody
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
		return colorVioletTile, colorVioletInk, s.historyImage
	case domain.ClipKindURL:
		return colorBlueTile, colorBlueInk, s.historyLink
	case domain.ClipKindCode:
		return colorStoneTile, colorStoneInk, s.historyCode
	case domain.ClipKindJSON:
		return colorEmeraldTile, colorEmeraldInk, s.historyText
	case domain.ClipKindFile:
		return colorAmberTile, colorAmberInk, s.historyText
	default:
		return colorSkyTile, colorSkyInk, s.historyText
	}
}

// historyLabel is the one word a list shows beside a copy: what it is, and for
// an image how much of it there is — an image is the kind whose copying
// depends on what the platform handed over.
func historyLabel(entry domain.ClipboardEntry) string {
	if entry.Kind == domain.ClipKindImage && entry.ImageBytes > 0 {
		return "图片 " + byteSize(entry.ImageBytes)
	}
	return kindLabel(entry.Kind)
}

// kindLabel names an entry's kind, where knowing that a copy was a link
// rather than prose is worth a word. Plain text gets none: it is what most
// copies are.
func kindLabel(kind domain.ClipKind) string {
	switch kind {
	case domain.ClipKindURL:
		return "链接"
	case domain.ClipKindJSON:
		return "JSON"
	case domain.ClipKindCode:
		return "代码"
	case domain.ClipKindFile:
		return "文件"
	case domain.ClipKindImage:
		return "图片"
	default:
		return ""
	}
}

// copyEntry puts a recorded copy back on the clipboard: a file list as
// files, anything else as the text it was.
//
// The row is marked and the write is sent, both without waiting. A click is
// answered by the next frame, and the write happens on the clipboard's own
// goroutine: the frame the click was in is not held up by a platform call,
// and a write the platform refuses is not something the user can act on from
// this window — the mark goes back either way, so no row claims a copy that
// did not happen.
func (s *State) copyEntry(entry domain.ClipboardEntry) {
	if s.clipboard == nil {
		return
	}
	window := s.historyWindow
	s.markCopied(entry)
	if window != nil {
		window.Update(func() {})
	}
	s.clipboard.CopyAsync(entry, func(err error) {
		if err == nil || window == nil {
			return
		}
		window.Update(func() {
			if s.copiedEntry == entry.ID {
				s.clearCopied()
			}
		})
	})
	s.unmarkCopiedLater(700 * time.Millisecond)
}

// markCopied remembers which row was just clicked, so that it can say so for
// a moment. It runs on the main thread, where the frame that reads it runs.
func (s *State) markCopied(entry domain.ClipboardEntry) {
	s.copiedEntry = entry.ID
	s.copiedAt = time.Now()
}

// clearCopied forgets the mark.
func (s *State) clearCopied() {
	s.copiedEntry = ""
	s.copiedAt = time.Time{}
}

// unmarkCopiedLater takes the mark back once it has been on screen long
// enough to be seen, and asks the frame that draws it away for one more
// frame. The state it changes is touched on the main thread, inside Update:
// the timer only asks.
func (s *State) unmarkCopiedLater(after time.Duration) {
	window := s.historyWindow
	if window == nil {
		// No window is showing the list, so there is no mark to take back.
		return
	}
	deadline := s.copiedAt.Add(after)
	time.AfterFunc(after, func() {
		window.Update(func() {
			// A second click moved the mark on: this timer was for the one
			// before it, and the newer mark keeps its moment.
			if !s.copiedAt.After(deadline) {
				s.clearCopied()
			}
		})
	})
}

// byteSize writes a size the way a person reads it.
func byteSize(n int) string {
	switch {
	case n >= 1<<20:
		return strconv.Itoa(n/(1<<20)) + " MB"
	case n >= 1<<10:
		return strconv.Itoa(n/(1<<10)) + " KB"
	default:
		return strconv.Itoa(n) + " B"
	}
}

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
