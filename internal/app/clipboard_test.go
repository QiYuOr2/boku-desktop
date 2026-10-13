package app

import (
	"context"
	"errors"
	"fmt"
	"image"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	"boku/internal/usecase"
)

// The clipboard has two faces in this layer: one line in the quick window
// and a list in the history window. Both are drawn from what the tool
// already holds — a frame reads and never copies — and both are absent,
// rather than empty, when the user has not asked for them.

// The region is off by default: an application nobody has configured reads
// no clipboard and shows none.
func TestClipboardRegionIsAbsentUntilItIsAskedFor(t *testing.T) {
	state := clipboardState(t, &testDevice{}, nil)
	tt := ui.NewTester(state.View, panelWidth, panelHeight)

	if tt.HasText("历史") {
		t.Errorf("the quick window shows a clipboard region by default: %q", tt.Texts())
	}
	if tt.HasText("（空）") {
		t.Error("the quick window shows an empty clipboard line by default")
	}
	// The window is otherwise what it was: the panel and the footer.
	for _, want := range []string{"DeepSeek 余额", "设置", "退出"} {
		if !tt.HasText(want) {
			t.Errorf("the quick window does not show %q", want)
		}
	}
}

func TestClipboardRegionShowsTheCurrentContent(t *testing.T) {
	cases := []struct {
		name    string
		reading domain.ClipContent
		want    string
	}{
		{
			name:    "text",
			reading: domain.ClipContent{Kind: domain.ClipKindText, Text: "hello  world"},
			want:    "hello world",
		},
		{
			name:    "a url",
			reading: domain.ClipContent{Kind: domain.ClipKindText, Text: "https://example.com/a"},
			want:    "https://example.com/a",
		},
		{
			name:    "one file",
			reading: domain.ClipContent{Kind: domain.ClipKindFile, Files: []string{`C:\dir\report.pdf`}},
			want:    "[report.pdf]",
		},
		{
			name:    "several files",
			reading: domain.ClipContent{Kind: domain.ClipKindFile, Files: []string{"a.txt", "b.txt"}},
			want:    "[a.txt 等 2 个文件]",
		},
		{
			name:    "an image",
			reading: domain.ClipContent{Kind: domain.ClipKindImage, HasImage: true},
			want:    "[图片]",
		},
		{
			name:    "rich text",
			reading: domain.ClipContent{Kind: domain.ClipKindText, HTML: "<p>a <b>bold</b> word</p>"},
			want:    "a bold word",
		},
	}
	for _, c := range cases {
		device := &testDevice{}
		device.set(c.reading)
		state := clipboardState(t, device, &testToolStore{
			settings: domain.ToolSettings{ClipboardRegionVisible: true, ClipboardMonitoring: true},
		})

		// Reading once is what the poll would have done before the frame.
		state.clipboard.Read(context.Background())
		tt := ui.NewTester(state.View, panelWidth, panelHeight)
		// The region is labelled by its mark rather than by a word, so
		// what says it is there is the way in beside the content.
		if !tt.HasText("历史") {
			t.Errorf("%s: the region is not drawn: %q", c.name, tt.Texts())
		}
		if !tt.HasText(c.want) {
			t.Errorf("%s: the region does not show %q; it shows %q", c.name, c.want, tt.Texts())
		}
	}
}

// The clipboard mark stands where the word "剪贴板" used to: the label of the
// line is the icon, so it has to be drawn, in a quiet color, at the left of
// the content it labels.
func TestClipboardRegionIsLabelledByItsMark(t *testing.T) {
	device := &testDevice{}
	device.set(domain.ClipContent{Kind: domain.ClipKindText, Text: "copied"})
	state := clipboardState(t, device, &testToolStore{
		settings: domain.ToolSettings{ClipboardRegionVisible: true, ClipboardMonitoring: true},
	})
	state.clipboard.Read(context.Background())

	muted := ui.LightTheme().TextMuted
	withoutIcon := ui.Render(state.View, panelWidth, panelHeight, 2)
	before := countNear(withoutIcon, muted)

	// The same region with no mark: whatever the mark draws is the
	// difference between the two.
	state.clipboardMark = nil
	without := ui.Render(state.View, panelWidth, panelHeight, 2)
	after := countNear(without, muted)

	if after >= before {
		t.Fatalf("the clipboard mark draws nothing: %d quiet pixels with it, %d without", before, after)
	}
	if drawn := before - after; drawn < 40 {
		t.Errorf("the clipboard mark covers %d pixels: too few to be the icon", drawn)
	}
}

// countNear counts the pixels drawn in a color, allowing for the antialiasing
// that turns an edge into a blend with what is behind it.
func countNear(img *image.RGBA, want ui.Color) int {
	count := 0
	for i := 0; i+3 < len(img.Pix); i += 4 {
		if closeTo(img.Pix[i], want.R, 24) && closeTo(img.Pix[i+1], want.G, 24) && closeTo(img.Pix[i+2], want.B, 24) {
			count++
		}
	}
	return count
}

func closeTo(got, want uint8, tolerance int) bool {
	diff := int(got) - int(want)
	if diff < 0 {
		diff = -diff
	}
	return diff <= tolerance
}

// A reading that noticed something — a picture whose bytes the platform
// never handed over — says so on its row, in the copy's own words.
func TestARowShowsWhatTheReadingNoticed(t *testing.T) {
	device := &testDevice{}
	state := clipboardState(t, device, &testToolStore{
		settings: domain.ToolSettings{ClipboardRegionVisible: true, ClipboardMonitoring: true},
	})
	note := "图片没能读出：unsupported clipboard format"
	device.set(domain.ClipContent{
		Kind:        domain.ClipKindImage,
		HasImage:    true,
		Observation: note,
	})
	state.clipboard.Read(context.Background())
	device.set(domain.ClipContent{Kind: domain.ClipKindText, Text: "an ordinary copy"})
	state.clipboard.Read(context.Background())

	tt := ui.NewTester(state.historyView, historyWidth, historyHeight)
	if !tt.HasText(note) {
		t.Fatalf("the row does not show what the reading noticed: %q", tt.Texts())
	}
	// It is on the picture's row: the copy below it carries no note.
	noted := findText(t, tt, note)
	ordinary := findText(t, tt, "an ordinary copy")
	if noted.Y <= ordinary.Y {
		t.Errorf("the note at y=%.0f is not under the picture it belongs to", noted.Y)
	}
	if near(tt, "an ordinary copy", "没能读出") {
		t.Error("an ordinary copy carries a note")
	}
}

// near reports whether a text containing want is on the same row as the
// text labelled.
func near(tt *ui.Tester, labelled, want string) bool {
	row, ok := tt.Find(labelled)
	if !ok {
		return false
	}
	for _, text := range tt.Texts() {
		if !strings.Contains(text, want) {
			continue
		}
		rect, ok := tt.Find(text)
		if !ok {
			continue
		}
		if rect.Y < row.Y+row.H && rect.Y+rect.H > row.Y {
			return true
		}
	}
	return false
}

// Each row carries one label: what the copy is, and for an image how large
// it was. Two labels read as a repetition ("文件 文件"), which is what this
// pins.
func TestEntryLabelIsOneWordAtMost(t *testing.T) {
	cases := []struct {
		name  string
		entry domain.ClipboardEntry
		want  string
	}{
		{"text", domain.ClipboardEntry{Kind: domain.ClipKindText}, ""},
		{"a link", domain.ClipboardEntry{Kind: domain.ClipKindURL}, "链接"},
		{"json", domain.ClipboardEntry{Kind: domain.ClipKindJSON}, "JSON"},
		{"code", domain.ClipboardEntry{Kind: domain.ClipKindCode}, "代码"},
		{"files", domain.ClipboardEntry{Kind: domain.ClipKindFile, Files: []string{"a", "b"}}, "文件"},
		{
			"an image with a size",
			domain.ClipboardEntry{Kind: domain.ClipKindImage, ImageBytes: 245 * 1024},
			"图片 245 KB",
		},
		{"an image with none", domain.ClipboardEntry{Kind: domain.ClipKindImage}, "图片"},
		{"a small image", domain.ClipboardEntry{Kind: domain.ClipKindImage, ImageBytes: 1}, "图片 1 B"},
	}
	for _, c := range cases {
		if got := entryLabel(c.entry); got != c.want {
			t.Errorf("%s: entryLabel = %q, want %q", c.name, got, c.want)
		}
	}
}

// A file row is labelled once, not twice.
func TestHistoryRowLabelsAFileOnce(t *testing.T) {
	device := &testDevice{}
	state := clipboardState(t, device, &testToolStore{
		settings: domain.ToolSettings{ClipboardRegionVisible: true, ClipboardMonitoring: true},
	})
	ctx := context.Background()
	device.set(domain.ClipContent{Kind: domain.ClipKindFile, Files: []string{"/tmp/a.txt"}})
	state.clipboard.Read(ctx)

	tt := ui.NewTester(state.historyView, historyWidth, historyHeight)
	texts := tt.Texts()
	seen := 0
	for _, text := range texts {
		if text == "文件" {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("the file row shows 文件 %d times: %q", seen, texts)
	}
}

// With the region on and nothing read yet, no line is drawn at all: a line
// with nothing after it would read as a region that failed to load.
func TestClipboardRegionDrawsNothingBeforeThereIsACopy(t *testing.T) {
	state := clipboardState(t, &testDevice{}, &testToolStore{
		settings: domain.ToolSettings{ClipboardRegionVisible: true, ClipboardMonitoring: true},
	})
	tt := ui.NewTester(state.View, panelWidth, panelHeight)
	if tt.HasText("历史") {
		t.Errorf("the region is drawn with nothing to show: %q", tt.Texts())
	}
	if tt.HasText("（空）") {
		t.Errorf("an empty region draws a placeholder: %q", tt.Texts())
	}
	// The panel and the footer are there.
	for _, want := range []string{"DeepSeek 余额", "Boku 0.1.0"} {
		if !tt.HasText(want) {
			t.Errorf("the window lost %q: %q", want, tt.Texts())
		}
	}
}

// The line is one line between the content and the footer: it sits under the
// panel and above the version.
func TestClipboardRegionSitsBetweenTheContentAndTheFooter(t *testing.T) {
	device := &testDevice{}
	device.set(domain.ClipContent{Kind: domain.ClipKindText, Text: "copied text"})
	state := clipboardState(t, device, &testToolStore{
		settings: domain.ToolSettings{ClipboardRegionVisible: true, ClipboardMonitoring: true},
	})
	state.clipboard.Read(context.Background())
	tt := ui.NewTester(state.View, panelWidth, panelHeight)

	content, ok := tt.Find("copied text")
	if !ok {
		t.Fatalf("the clipboard line was not found; the window shows %q", tt.Texts())
	}
	title, ok := tt.Find("DeepSeek 余额")
	if !ok {
		t.Fatalf("the panel title was not found; the window shows %q", tt.Texts())
	}
	version, ok := tt.Find("Boku 0.1.0")
	if !ok {
		t.Fatalf("the footer was not found; the window shows %q", tt.Texts())
	}
	if content.Y <= title.Y {
		t.Errorf("the clipboard line at y=%.0f is not below the panel at y=%.0f", content.Y, title.Y)
	}
	if content.Y >= version.Y {
		t.Errorf("the clipboard line at y=%.0f is not above the footer at y=%.0f", content.Y, version.Y)
	}
	// One row: the whole line stays inside the height of a footer row.
	if content.H > 20 {
		t.Errorf("the clipboard line is %.0f points tall: it should be one line", content.H)
	}
	// The mark and the way in are on either side of the value: the label
	// is at the window's margin, the action at the other end.
	history, ok := tt.Find("历史")
	if !ok {
		t.Fatalf("the way to the history was not found: %q", tt.Texts())
	}
	if content.X <= 0 || content.X >= history.X {
		t.Errorf("the content at x=%.0f is not between the window's margin and the history at x=%.0f",
			content.X, history.X)
	}
	// A long value does not push anything out of the window.
	long := strings.Repeat("x", 500)
	device.set(domain.ClipContent{Kind: domain.ClipKindText, Text: long})
	state.clipboard.Read(context.Background())
	tt = ui.NewTester(state.View, panelWidth, panelHeight)
	if !tt.HasText("Boku 0.1.0") {
		t.Error("a long clipboard value pushed the footer out of the window")
	}
}

// The history window lists what was copied, newest first, and clicking an
// entry copies it back.
func TestHistoryWindowListsAndCopiesBack(t *testing.T) {
	device := &testDevice{}
	state := clipboardState(t, device, &testToolStore{
		settings: domain.ToolSettings{
			ClipboardRegionVisible: true,
			ClipboardMonitoring:    true,
			ClipboardHistorySize:   domain.ClipboardMinHistorySize,
		},
	})
	ctx := context.Background()
	for _, text := range []string{"first copy", "second copy", "third copy"} {
		device.set(domain.ClipContent{Kind: domain.ClipKindText, Text: text})
		state.clipboard.Read(ctx)
	}

	tt := ui.NewTester(state.historyView, historyWidth, historyHeight)
	// The heading is the record range the user chose, which is also all the
	// page can list.
	for _, want := range []string{"最近 10 条", "first copy", "second copy", "third copy", "点击一条即复制回剪贴板。"} {
		if !tt.HasText(want) {
			t.Errorf("the history does not show %q; it shows %q", want, tt.Texts())
		}
	}
	// Newest first.
	newest, _ := tt.Find("third copy")
	oldest, _ := tt.Find("first copy")
	if newest.Y >= oldest.Y {
		t.Errorf("the newest copy at y=%.0f is not above the oldest at y=%.0f", newest.Y, oldest.Y)
	}

	if err := tt.Click("second copy"); err != nil {
		t.Fatalf("clicking an entry failed: %v", err)
	}
	writes := device.written()
	if len(writes) != 1 || writes[0] != "second copy" {
		t.Errorf("the clipboard was written %v, want the entry that was clicked", writes)
	}
	// Copying an entry puts the copy back on the clipboard and reuses the
	// entry rather than adding a second one. That the reused entry moves to
	// the top is the use case's promise, and it is tested there against a
	// clock the test controls.
	if got := len(state.clipboard.History(0)); got != 3 {
		t.Errorf("copying an entry left %d entries, want 3", got)
	}
	tt.Frame()
	for _, want := range []string{"first copy", "second copy", "third copy"} {
		if !tt.HasText(want) {
			t.Errorf("the list lost %q after a copy: %q", want, tt.Texts())
		}
	}
}

func TestHistoryWindowSaysWhenThereIsNothingYet(t *testing.T) {
	state := clipboardState(t, &testDevice{}, &testToolStore{})
	tt := ui.NewTester(state.historyView, historyWidth, historyHeight)
	if !tt.HasText("还没有记录。") {
		t.Errorf("an empty history does not say so: %q", tt.Texts())
	}
}

// The page lists what the record range keeps — the range is the bound on the
// history, not a separate page size — and it names that range.
func TestHistoryPageFollowsTheRecordRange(t *testing.T) {
	device := &testDevice{}
	state := clipboardState(t, device, &testToolStore{
		settings: domain.ToolSettings{ClipboardRegionVisible: true, ClipboardMonitoring: true},
	})
	ctx := context.Background()
	total := domain.ClipboardMinHistorySize + 2
	for i := 0; i < total; i++ {
		device.set(domain.ClipContent{Kind: domain.ClipKindText, Text: fmt.Sprintf("copy %d", i)})
		state.clipboard.Read(ctx)
	}
	if got := len(state.clipboardHistory()); got != total {
		t.Fatalf("the default range lists %d entries, want %d", got, total)
	}
	tt := ui.NewTester(state.historyView, historyWidth, historyHeight)
	if !tt.HasText(fmt.Sprintf("最近 %d 条", domain.ClipboardDefaultHistorySize)) {
		t.Errorf("the page does not name its range: %q", tt.Texts())
	}

	if err := state.clipboard.SetHistorySize(ctx, domain.ClipboardMinHistorySize); err != nil {
		t.Fatalf("SetHistorySize failed: %v", err)
	}
	tt = ui.NewTester(state.historyView, historyWidth, historyHeight)
	if !tt.HasText("最近 10 条") {
		t.Errorf("the page still shows the old range: %q", tt.Texts())
	}
	if got := len(state.clipboardHistory()); got != domain.ClipboardMinHistorySize {
		t.Errorf("the page lists %d entries, want %d", got, domain.ClipboardMinHistorySize)
	}
}

// The clipboard settings offer the record range, and choosing one applies it
// at once: the entries that no longer fit are dropped, and the form says so
// before the choice is made.
func TestSettingsChooseTheRecordRange(t *testing.T) {
	device := &testDevice{}
	state := clipboardState(t, device, &testToolStore{
		settings: domain.ToolSettings{ClipboardRegionVisible: true, ClipboardMonitoring: true},
	})
	ctx := context.Background()
	for i := 0; i < domain.ClipboardDefaultHistorySize+2; i++ {
		device.set(domain.ClipContent{Kind: domain.ClipKindText, Text: fmt.Sprintf("copy %d", i)})
		state.clipboard.Read(ctx)
	}
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)
	for _, want := range []string{
		"记录范围", "10 条", "30 条", "50 条",
		"只保留最近 30 条；缩短范围会立刻丢弃更早的记录。",
	} {
		if !tt.HasText(want) {
			t.Errorf("the clipboard settings do not show %q: %q", want, tt.Texts())
		}
	}

	if err := tt.Click("10 条"); err != nil {
		t.Fatalf("click the shortest range: %v", err)
	}
	tt.Frame()
	if got := state.clipboard.Settings().HistorySize(); got != domain.ClipboardMinHistorySize {
		t.Fatalf("the record range is %d after the click, want %d", got, domain.ClipboardMinHistorySize)
	}
	if got := len(state.clipboard.History(0)); got != domain.ClipboardMinHistorySize {
		t.Errorf("the history holds %d entries, want %d", got, domain.ClipboardMinHistorySize)
	}
	if !tt.HasText("只保留最近 10 条；缩短范围会立刻丢弃更早的记录。") {
		t.Errorf("the settings do not show the new range: %q", tt.Texts())
	}
	if text := state.clipboardStatusLine(); !strings.Contains(text, "丢弃") {
		t.Errorf("the settings do not say what shortening the range cost: %q", text)
	}
}

// An entry's kind is worth a word: a link among prose is worth recognizing.
func TestKindLabels(t *testing.T) {
	cases := map[domain.ClipKind]string{
		domain.ClipKindText:  "",
		domain.ClipKindURL:   "链接",
		domain.ClipKindJSON:  "JSON",
		domain.ClipKindCode:  "代码",
		domain.ClipKindFile:  "文件",
		domain.ClipKindImage: "图片",
	}
	for kind, want := range cases {
		if got := kindLabel(kind); got != want {
			t.Errorf("kindLabel(%q) = %q, want %q", kind, got, want)
		}
	}
}

// A state assembled without a clipboard tool draws neither face and does not
// fall over: that is how a test of a panel alone runs.
func TestStateWithoutAClipboardIsSafe(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.clipboard = nil
	loaded(t, state)
	tt := ui.NewTester(state.View, panelWidth, panelHeight)
	if !tt.HasText("退出") {
		t.Errorf("the quick window lost its footer without a clipboard: %q", tt.Texts())
	}
	if _, ok := state.clipboardLatest(); ok {
		t.Error("a state without a clipboard reported content")
	}
	if entries := state.clipboardHistory(); entries != nil {
		t.Errorf("a state without a clipboard reported history: %v", entries)
	}
	if err := state.clipboardHistoryError(); err != nil {
		t.Errorf("a state without a clipboard reported an error: %v", err)
	}
	state.openHistory() // must not panic
}

// A history that could not be read is said out loud rather than shown as an
// empty list, which would look like "nothing was ever copied".
func TestHistoryWindowReportsAnUnreadableHistory(t *testing.T) {
	state := clipboardState(t, &testDevice{}, &testToolStore{})
	clipboard, err := usecase.NewClipboard(usecase.ClipboardOptions{
		Device: &testDevice{},
		Store:  failingEntryStore{},
		Tools:  &testToolStore{},
	})
	if err != nil {
		t.Fatalf("NewClipboard failed: %v", err)
	}
	t.Cleanup(clipboard.Stop)
	state.clipboard = clipboard

	tt := ui.NewTester(state.historyView, historyWidth, historyHeight)
	if !tt.HasText("读不到上次的历史：history is unreadable") {
		t.Errorf("the window does not report the unreadable history: %q", tt.Texts())
	}
}

// The way to the history sits at the end of the clipboard line, where a
// glance lands last, rather than in the footer: a footer of actions is about
// the window, and the history is about the line above it.
func TestHistoryIsReachableFromTheQuickWindow(t *testing.T) {
	device := &testDevice{}
	state := clipboardState(t, device, &testToolStore{
		settings: domain.ToolSettings{ClipboardRegionVisible: true, ClipboardMonitoring: true},
	})
	device.set(domain.ClipContent{Kind: domain.ClipKindText, Text: "copied"})
	state.clipboard.Read(t.Context())

	tt := ui.NewTester(state.View, panelWidth, panelHeight)
	button, ok := tt.Find("历史")
	if !ok {
		t.Fatalf("the quick window has no way to the history: %q", tt.Texts())
	}
	if _, ok := tt.Find("Boku 0.1.0"); !ok {
		t.Fatalf("the footer was not found: %q", tt.Texts())
	}
	if button.W > 60 {
		t.Errorf("the history action is %.0f points wide: the clipboard line should keep it small", button.W)
	}
	line, ok := tt.Find("copied")
	if !ok {
		t.Fatalf("the clipboard line was not found: %q", tt.Texts())
	}
	if diff := button.Y - line.Y; diff > 4 || diff < -4 {
		t.Errorf("the history action at y=%.1f and the clipboard line at y=%.1f are not on one row", button.Y, line.Y)
	}
}

// failingEntryStore is a domain.ClipboardStore whose history cannot be read.
type failingEntryStore struct{}

func (failingEntryStore) LoadEntries(context.Context) ([]domain.ClipboardEntry, error) {
	return nil, errors.New("history is unreadable")
}

func (failingEntryStore) SaveEntries(context.Context, []domain.ClipboardEntry) error { return nil }
