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
// on the clipboard right now — under the panels and above the footer, which
// is where a line about something else belongs. The history window lists the
// copies before it, newest first, and a click puts one back on the clipboard.
//
// The line is one line: the tool's own reading decides what it says, so a
// file is "[name.ext]", an image is "[图片]" and rich text is the text it
// shows. Nothing here shortens anything while drawing — that was decided when
// the reading was recorded, which is what keeps a frame cheap.
const (
	historyWidth  = 420
	historyHeight = 420
)

// clipboardRegion is the line under the panels: the mark, the content, and
// the way to the history. It is one row high, so that the quick window stays
// a glance rather than becoming a list.
//
// It draws its own leading line, and only when there is something to show:
// with nothing read yet — monitoring is off, or nothing has been copied —
// the line under the content runs straight to the footer, which is what a
// window with no clipboard region looks like.
//
// The mark and the way in do not shrink: a long value loses its own tail to
// the ellipsis, and what the row is and what it offers stay readable.
func (s *State) clipboardRegion(c *ui.Context) {
	t := c.Theme()
	content, ok := s.clipboardLatest()

	if content.Display == "" || !ok {
		return
	}

	ui.Divider(c)
	ui.Row(c).FillWidth().Height(t.Space(8)).
		PaddingX(t.Space(4)).
		AlignItems(ui.Center).
		Gap(t.Space(2)).
		Children(func() {
			// The mark wears the label's color: it is a label, and the
			// word it replaced was drawn in the same place.
			ui.Icon(c, s.clipboardMark).TextColor(t.TextMuted)
			ui.Text(c, content.Display).FontSize(t.Rem(0.78)).SingleLine()
			ui.Spacer(c)
			if button := historyButton(c); button.Clicked() {
				s.openHistory()
			}
		})
}

// historyButton is the way from the clipboard line to the history: a chip,
// because the line it sits on is a value rather than a row of actions.
func historyButton(c *ui.Context) ui.Element {
	return chipButton(c, "历史")
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
			Title:     appName + " 剪贴板历史",
			Width:     historyWidth,
			Height:    historyHeight,
			MinWidth:  320,
			MinHeight: 240,
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

// historyView lists the recorded copies. It reads what the tool already
// holds, and copies nothing while drawing.
func (s *State) historyView(c *ui.Context) {
	t := c.Theme()
	c.Root().Background(t.Background)
	s.page(c, page{
		body: func(c *ui.Context) {
			if err := s.clipboardHistoryError(); err != nil {
				block(c, "剪贴板历史", func() {
					ui.Text(c, "读不到上次的历史："+friendlyError(err)).
						FontSize(t.Rem(0.82)).
						TextColor(t.Danger).
						Wrap()
					ui.Text(c, "新复制的仍然会记下来。").
						FontSize(t.Rem(0.75)).
						TextColor(t.TextMuted).
						Wrap()
				})
				ui.Divider(c)
				return
			}
			entries := s.clipboardHistory()
			block(c, "最近 "+strconv.Itoa(s.clipboardHistorySize())+" 条", func() {
				if len(entries) == 0 {
					ui.Text(c, "还没有记录。").
						FontSize(t.Rem(0.85)).
						TextColor(t.TextMuted)
					ui.Text(c, "复制一段内容，再打开这里。").
						FontSize(t.Rem(0.75)).
						TextColor(t.TextMuted)
					return
				}
				ui.Column(c).FillWidth().Gap(t.Space(1)).Children(func() {
					for _, entry := range entries {
						s.historyRow(c, entry)
					}
				})
			})
			ui.Divider(c)
			ui.Text(c, "点击一条即复制回剪贴板。").
				FontSize(t.Rem(0.72)).
				TextColor(t.TextMuted)
		},
	})
}

// historyRow is one recorded copy: what was copied, when, and of what kind.
// The whole row is the button: a list of ten copies is a list of ten things
// to click, not a list with a small target at the end of each line.
//
// The row shows what it is doing: a face under the pointer, a deeper one
// while the press is held, and the pressed face kept for a moment after the
// copy, so that a click is answered on the row itself rather than only on
// the clipboard.
func (s *State) historyRow(c *ui.Context, entry domain.ClipboardEntry) {
	t := c.Theme()
	row := ui.ButtonBase(c).FillWidth().
		Radius(t.Radius).
		Padding(t.Space(1.5), t.Space(2)).
		AlignItems(ui.Center).
		Gap(t.Space(2))
	justCopied := entry.ID == s.copiedEntry
	switch {
	case justCopied:
		row.Background(t.Accent.Alpha(0.18))
	case row.Pressed():
		row.Background(t.SurfacePressed)
	case row.Hovered() || row.Focused():
		row.Background(t.SurfaceHover)
	}
	row.Children(func() {
		if entry.Pinned {
			ui.Text(c, "★").FontSize(t.Rem(0.7)).TextColor(t.TextMuted).SingleLine().Shrink(0)
		}
		ui.Text(c, entry.Line()).FontSize(t.Rem(0.8)).SingleLine()
		ui.Spacer(c)
		// What the reading noticed about itself: the copy is still usable,
		// and the interface should not pretend it was complete. It is the
		// copy's own words, so it is drawn as it was stored.
		if entry.Note != "" {
			ui.Text(c, entry.Note).
				FontSize(t.Rem(0.66)).
				TextColor(t.Danger).
				SingleLine().
				Shrink(0)
		}
		// One label, not two: what the copy is, and for an image how much
		// of it the platform handed over.
		if label := entryLabel(entry); label != "" {
			ui.Text(c, label).
				FontSize(t.Rem(0.68)).
				TextColor(t.TextMuted).
				SingleLine().
				Shrink(0)
		}
		ui.Text(c, copiedClock(entry.CopiedAt)).
			FontSize(t.Rem(0.68)).
			TextColor(t.TextMuted).
			SingleLine().
			Shrink(0)
	})
	if row.Clicked() {
		s.copyEntry(entry)
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

// entryLabel is the one word a list shows beside a copy: what it is, and for
// an image how much of it there is — an image is the kind whose copying
// depends on what the platform handed over.
func entryLabel(entry domain.ClipboardEntry) string {
	switch {
	case entry.Kind == domain.ClipKindFile:
		return "文件"
	case entry.Kind == domain.ClipKindImage && entry.ImageBytes > 0:
		return "图片 " + byteSize(entry.ImageBytes)
	case entry.Kind == domain.ClipKindImage:
		return "图片"
	default:
		return kindLabel(entry.Kind)
	}
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
