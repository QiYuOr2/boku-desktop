package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"boku/internal/domain"
)

// The clipboard tool is tested against a device and a store the test owns:
// no platform clipboard, no file, no window. What is checked here is what
// the tool promises — one entry per copy, a loop that stops when it is told
// to, and a history that does not grow without a bound.

func TestNewClipboardRequiresItsDependencies(t *testing.T) {
	if _, err := NewClipboard(ClipboardOptions{Store: &fakeStore{}}); err == nil {
		t.Error("NewClipboard accepted a service without a device")
	}
	if _, err := NewClipboard(ClipboardOptions{Device: &fakeDevice{}}); err == nil {
		t.Error("NewClipboard accepted a service without a store")
	}
}

// Both switches are off until the user turns them on: the clipboard is the
// most sensitive thing the application can read.
func TestClipboardDefaultsToOff(t *testing.T) {
	clipboard, _, _, _ := newTestClipboard(t, ClipboardOptions{})
	settings := clipboard.Settings()
	if settings.ClipboardRegionVisible || settings.ClipboardMonitoring {
		t.Errorf("the clipboard starts enabled: %+v", settings)
	}
	if clipboard.Show() {
		t.Error("Show reports a region nothing asked to draw")
	}
}

func TestRecordShapesEveryKind(t *testing.T) {
	cases := []struct {
		name        string
		reading     domain.ClipContent
		wantKind    domain.ClipKind
		wantDisplay string
		wantContent string
	}{
		{
			name:        "plain text",
			reading:     domain.ClipContent{Kind: domain.ClipKindText, Text: "hello  world\n"},
			wantKind:    domain.ClipKindText,
			wantDisplay: "hello world",
			wantContent: "hello  world\n",
		},
		{
			name:        "a url",
			reading:     domain.ClipContent{Kind: domain.ClipKindText, Text: "https://example.com/a"},
			wantKind:    domain.ClipKindURL,
			wantDisplay: "https://example.com/a",
			wantContent: "https://example.com/a",
		},
		{
			name:        "json",
			reading:     domain.ClipContent{Kind: domain.ClipKindText, Text: `{"a":1}`},
			wantKind:    domain.ClipKindJSON,
			wantDisplay: `{"a":1}`,
			wantContent: `{"a":1}`,
		},
		{
			name:        "one file",
			reading:     domain.ClipContent{Kind: domain.ClipKindFile, Files: []string{`C:\dir\report.pdf`}},
			wantKind:    domain.ClipKindFile,
			wantDisplay: "[report.pdf]",
			wantContent: `C:\dir\report.pdf`,
		},
		{
			name:        "three files",
			reading:     domain.ClipContent{Kind: domain.ClipKindFile, Files: []string{"a.txt", "b.txt", "c.txt"}},
			wantKind:    domain.ClipKindFile,
			wantDisplay: "[a.txt 等 3 个文件]",
			wantContent: "a.txt\nb.txt\nc.txt",
		},
		{
			name:        "an image without a name",
			reading:     domain.ClipContent{Kind: domain.ClipKindImage, HasImage: true},
			wantKind:    domain.ClipKindImage,
			wantDisplay: "[图片]",
			wantContent: "[图片]",
		},
		{
			name:        "an image with a name",
			reading:     domain.ClipContent{Kind: domain.ClipKindImage, HasImage: true, Files: []string{"/tmp/shot.png"}},
			wantKind:    domain.ClipKindImage,
			wantDisplay: "[shot.png]",
			wantContent: "[shot.png]",
		},
		{
			name:        "rich text without a plain flavour",
			reading:     domain.ClipContent{Kind: domain.ClipKindText, HTML: "<p>a <b>bold</b> word</p>"},
			wantKind:    domain.ClipKindText,
			wantDisplay: "a bold word",
			wantContent: "a bold word",
		},
		{
			name:        "rich text with an empty plain flavour",
			reading:     domain.ClipContent{Kind: domain.ClipKindText, Text: "  ", HTML: "<div>from markup</div>"},
			wantKind:    domain.ClipKindText,
			wantDisplay: "from markup",
			wantContent: "from markup",
		},
		{
			name:        "rich text with nothing readable",
			reading:     domain.ClipContent{Kind: domain.ClipKindText, HTML: "<div></div>"},
			wantKind:    domain.ClipKindText,
			wantDisplay: "",
			wantContent: "",
		},
	}
	for _, c := range cases {
		record := Record(c.reading)
		if record.Kind != c.wantKind {
			t.Errorf("%s: kind = %q, want %q", c.name, record.Kind, c.wantKind)
		}
		if record.Display != c.wantDisplay {
			t.Errorf("%s: display = %q, want %q", c.name, record.Display, c.wantDisplay)
		}
		if record.Content != c.wantContent {
			t.Errorf("%s: content = %q, want %q", c.name, record.Content, c.wantContent)
		}
		if record.Hash == "" {
			t.Errorf("%s: a record without a hash cannot be deduplicated", c.name)
		}
	}
}

func TestRecordShortensLongText(t *testing.T) {
	record := Record(textReading(strings.Repeat("x", 5000)))
	if len([]rune(record.Display)) != 121 { // the limit plus the ellipsis
		t.Errorf("the display is %d runes, want 121", len([]rune(record.Display)))
	}
	if !strings.HasSuffix(record.Display, "…") {
		t.Error("a shortened display does not say it was shortened")
	}
	if len(record.Content) != 5000 {
		t.Error("the content itself was shortened as well as the display")
	}
}

// A reading that repeats what the history already holds is not a second
// entry, and what is recorded reaches the store behind the frame. (Reading
// a slow clipboard without blocking is what clipboard_blocking_test.go
// pins; this is the history's side of a read.)
func TestReadingTheSameValueTwiceKeepsOneEntry(t *testing.T) {
	clock := newTestClock()
	clipboard, device, store, _ := newTestClipboard(t, ClipboardOptions{Clock: clock})
	ctx := context.Background()
	read(clipboard, clock, device, "first")
	clipboard.Read(ctx)

	if got := len(clipboard.History(0)); got != 1 {
		t.Fatalf("the history has %d entries after reading one value twice, want 1", got)
	}
	if got := device.readCount(); got != 2 {
		t.Errorf("the device was read %d times, want 2", got)
	}
	// The history is written behind the change, so the store catches up a
	// moment later rather than holding the frame that made it.
	waitFor(t, "the history to be written", func() bool { return len(store.stored()) == 1 })
	latest, ok := clipboard.Latest()
	if !ok || latest.Content != "first" {
		t.Errorf("Latest = %+v, %v", latest, ok)
	}

	// A different value is a second entry, newest first.
	read(clipboard, clock, device, "second")
	history := clipboard.History(0)
	if len(history) != 2 {
		t.Fatalf("the history has %d entries, want 2", len(history))
	}
	if history[0].Content != "second" || history[1].Content != "first" {
		t.Errorf("the history is not newest first: %q, %q", history[0].Content, history[1].Content)
	}
}

func TestHistoryLimitAndOrder(t *testing.T) {
	clock := newTestClock()
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{Clock: clock, Keep: 3})
	for _, text := range []string{"one", "two", "three", "four"} {
		read(clipboard, clock, device, text)
	}
	history := clipboard.History(0)
	if len(history) != 3 {
		t.Fatalf("the history kept %d entries, want 3", len(history))
	}
	if history[0].Content != "four" || history[2].Content != "two" {
		t.Errorf("the oldest entry was not evicted: %q … %q", history[0].Content, history[2].Content)
	}
	// The page asks for fewer than the store keeps.
	if got := len(clipboard.History(2)); got != 2 {
		t.Errorf("History(2) returned %d entries", got)
	}
}

// The same content copied again moves its entry back to the top instead of
// adding a second one.
func TestSameContentCopiedAgainMovesToTheTop(t *testing.T) {
	clock := newTestClock()
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{Clock: clock})
	for _, text := range []string{"alpha", "beta", "alpha"} {
		read(clipboard, clock, device, text)
	}

	history := clipboard.History(0)
	if len(history) != 2 {
		t.Fatalf("the history has %d entries, want 2", len(history))
	}
	if history[0].Content != "alpha" {
		t.Errorf("the repeated copy is not at the top: %q first", history[0].Content)
	}
}

func TestPinnedEntriesSurviveTheLimit(t *testing.T) {
	device := &fakeDevice{}
	store := &fakeStore{}
	// A pinned entry, as the full clipboard tool will be able to make.
	pinned := domain.ClipboardEntry{
		ID:          "pinned-1",
		Content:     "keep me",
		ContentHash: "sha256:pinned",
		Kind:        domain.ClipKindText,
		Pinned:      true,
		CopiedAt:    time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		CreatedAt:   time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := store.SaveEntries(context.Background(), []domain.ClipboardEntry{pinned}); err != nil {
		t.Fatalf("SaveEntries failed: %v", err)
	}
	clipboard, err := NewClipboard(ClipboardOptions{
		Device: device,
		Store:  store,
		Tools:  &fakeTools{},
		Clock:  newTestClock(),
		Keep:   2,
	})
	if err != nil {
		t.Fatalf("NewClipboard failed: %v", err)
	}
	t.Cleanup(clipboard.Stop)

	if got := len(clipboard.History(0)); got != 1 {
		t.Fatalf("the pinned entry was not loaded: %d entries", got)
	}
	for _, text := range []string{"a", "b", "c"} {
		device.set(textReading(text))
		clipboard.Read(context.Background())
	}
	history := clipboard.History(0)
	// The limit bounds the unpinned entries: the pinned one sits on top of
	// them rather than inside the limit.
	if len(history) != 3 {
		t.Fatalf("the history has %d entries, want the limit plus the pinned one", len(history))
	}
	found := false
	for _, entry := range history {
		if entry.Content == "keep me" {
			found = true
		}
	}
	if !found {
		t.Errorf("the pinned entry was evicted: %v", history)
	}
}

func TestClipboardIgnoresWhatIsNotShowable(t *testing.T) {
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{})
	ctx := context.Background()

	// A reading with no kind is not a reading.
	device.set(domain.ClipContent{})
	clipboard.Read(ctx)
	if got := len(clipboard.History(0)); got != 0 {
		t.Errorf("an empty reading produced %d entries", got)
	}
	if _, ok := clipboard.Latest(); ok {
		t.Error("an empty reading became the latest content")
	}

	// A failed read is skipped rather than treated as an empty clipboard.
	device.fail(errors.New("the clipboard is busy"))
	clipboard.Read(ctx)
	if _, ok := clipboard.Latest(); ok {
		t.Error("a failed read became the latest content")
	}
	if got := len(clipboard.History(0)); got != 0 {
		t.Errorf("a failed read produced %d entries", got)
	}
}

// The application's own write must not come back as a new copy.
func TestCopySuppressesTheLoop(t *testing.T) {
	clock := newTestClock()
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{Clock: clock})
	ctx := context.Background()
	read(clipboard, clock, device, "from the user")

	clock.advance(time.Second)
	if err := clipboard.Copy(ctx, textEntry("from boku")); err != nil {
		t.Fatalf("Copy failed: %v", err)
	}
	wroteAt := clock.Now()
	if got := device.written(); len(got) != 1 || got[0] != "from boku" {
		t.Fatalf("the device was written %v", got)
	}

	// The device now holds what the application wrote: the next read is its
	// own write coming back.
	clock.advance(time.Second)
	clipboard.Read(ctx)
	history := clipboard.History(0)
	if len(history) != 2 {
		t.Fatalf("the history has %d entries, want 2: %v", len(history), history)
	}
	if history[0].Content != "from boku" {
		t.Errorf("the written value is not at the top: %q", history[0].Content)
	}
	for _, entry := range history {
		if entry.Content == "from boku" && !entry.CopiedAt.Equal(wroteAt) {
			t.Errorf("the written entry was re-recorded at %s, want %s", entry.CopiedAt, wroteAt)
		}
	}

	// After the suppression window, a genuinely new copy is recorded again.
	clock.advance(3 * domain.ClipboardDefaultPollInterval)
	read(clipboard, clock, device, "something else")
	if got := len(clipboard.History(0)); got != 3 {
		t.Errorf("a new value after the window produced %d entries, want 3", got)
	}
}

func TestCopyRejectsWhatItCannotWrite(t *testing.T) {
	clipboard, _, _, _ := newTestClipboard(t, ClipboardOptions{})
	ctx := context.Background()
	if err := clipboard.Copy(ctx, textEntry("   ")); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("Copy of blank text = %v, want ErrInvalidArgument", err)
	}
	if err := clipboard.Copy(ctx, textEntry(strings.Repeat("x", maxCopiedTextBytes+1))); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("Copy of an over-long value = %v, want ErrInvalidArgument", err)
	}
}

// Clicking a recorded copy of files pastes the files, not the text of their
// paths: the copy is the file list, and putting it back is what a clipboard
// is for.
func TestCopyWritesFilesBackAsFiles(t *testing.T) {
	clock := newTestClock()
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{Clock: clock})
	ctx := context.Background()
	paths := []string{"/tmp/report.pdf", "/tmp/notes.txt"}
	read(clipboard, clock, device, "placeholder")
	device.set(domain.ClipContent{Kind: domain.ClipKindFile, Files: paths})
	clipboard.Read(ctx)

	var fileEntry domain.ClipboardEntry
	for _, entry := range clipboard.History(0) {
		if entry.Kind == domain.ClipKindFile {
			fileEntry = entry
		}
	}
	if fileEntry.ID == "" {
		t.Fatalf("the file list was not recorded: %v", clipboard.History(0))
	}
	if len(fileEntry.Files) != 2 {
		t.Fatalf("the recorded entry holds %v, want the paths", fileEntry.Files)
	}

	if err := clipboard.Copy(ctx, fileEntry); err != nil {
		t.Fatalf("Copy failed: %v", err)
	}
	if got := device.written(); len(got) != 0 {
		t.Errorf("the file list was written as text: %v", got)
	}
	files := device.writtenFiles()
	if len(files) != 1 || len(files[0]) != 2 || files[0][0] != paths[0] || files[0][1] != paths[1] {
		t.Errorf("the file list written back is %v, want %v", files, paths)
	}

	// And it does not come back as a new entry: the application wrote it.
	clock.advance(time.Second)
	clipboard.Read(ctx)
	if got := len(clipboard.History(0)); got != 2 {
		t.Errorf("the history has %d entries after the write, want 2", got)
	}
}

// An image is recorded with its size and copied back as its name: the
// application does not keep the pixels of a screenshot.
func TestCopyWritesAnImageBackAsItsName(t *testing.T) {
	clock := newTestClock()
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{Clock: clock})
	ctx := context.Background()
	device.set(domain.ClipContent{Kind: domain.ClipKindImage, HasImage: true, ImageBytes: 4096})
	clipboard.Read(ctx)

	history := clipboard.History(0)
	if len(history) != 1 {
		t.Fatalf("the image was not recorded: %v", history)
	}
	entry := history[0]
	if entry.Kind != domain.ClipKindImage {
		t.Fatalf("the entry is %q, want an image", entry.Kind)
	}
	if entry.ImageBytes != 4096 {
		t.Errorf("the recorded size is %d, want 4096", entry.ImageBytes)
	}
	if entry.Content != "[图片]" {
		t.Errorf("the recorded content is %q, want the name", entry.Content)
	}

	if err := clipboard.Copy(ctx, entry); err != nil {
		t.Fatalf("Copy failed: %v", err)
	}
	if got := device.written(); len(got) != 1 || got[0] != "[图片]" {
		t.Errorf("the image copy wrote %v, want its name as text", got)
	}

	// The clipboard now holds text, and the history says so: a copy of an
	// image is recorded once as the picture it was, and once as the text it
	// was turned into.
	clock.advance(time.Second)
	clipboard.Read(ctx)
	history = clipboard.History(0)
	if len(history) != 2 {
		t.Fatalf("the history has %d entries after the write, want 2", len(history))
	}
	if history[0].Kind != domain.ClipKindText || history[0].Content != "[图片]" {
		t.Errorf("what the clipboard now holds is not recorded: %+v", history[0])
	}
}

// A picture the platform refused is recorded with a note saying so, and the
// note survives the trip through the history.
func TestAnUnreadablePictureIsRecordedWithItsReason(t *testing.T) {
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{})
	device.set(domain.ClipContent{
		Kind:        domain.ClipKindImage,
		HasImage:    true,
		Observation: "图片没能读出：unsupported clipboard format",
	})
	clipboard.Read(context.Background())

	history := clipboard.History(0)
	if len(history) != 1 {
		t.Fatalf("the picture was not recorded: %v", history)
	}
	if history[0].Note == "" {
		t.Error("the reason the picture could not be read was lost")
	}
	if history[0].ImageBytes != 0 {
		t.Errorf("a picture that was not read has a size: %d", history[0].ImageBytes)
	}
}

// The interval the switches were saved with is the one the suppression
// window is built from: a service that polled every two seconds must not
// start treating its own write as new after one.
func TestCopiedValueIsSuppressedForTheConfiguredInterval(t *testing.T) {
	clock := newTestClock()
	tools := &fakeTools{settings: domain.ToolSettings{ClipboardPollInterval: 2 * time.Second}}
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{Clock: clock, Tools: tools})
	if got := clipboard.Settings().Interval(); got != 2*time.Second {
		t.Fatalf("the service reads an interval of %s, want the saved one", got)
	}
	if got := clipboard.interval; got != 2*time.Second {
		t.Errorf("the service polls every %s, want the saved interval", got)
	}

	read(clipboard, clock, device, "from the user")
	if err := clipboard.Copy(context.Background(), textEntry("from boku")); err != nil {
		t.Fatalf("Copy failed: %v", err)
	}
	// Three seconds on, which is inside 2 × 2s but outside 2 × 700ms.
	clock.advance(3 * time.Second)
	clipboard.Read(context.Background())
	history := clipboard.History(0)
	if len(history) != 2 {
		t.Fatalf("the history has %d entries, want 2", len(history))
	}
	if history[0].Content != "from boku" {
		t.Errorf("the written value is not at the top: %q first", history[0].Content)
	}
}

func TestCopyReportsAWriteFailure(t *testing.T) {
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{})
	failure := errors.New("the clipboard is owned by another application")
	device.writeEr = failure
	if err := clipboard.Copy(context.Background(), textEntry("text")); !errors.Is(err, failure) {
		t.Errorf("Copy = %v, want the device's failure", err)
	}
}

// Monitoring is a switch on the poll loop, and off means no read at all.
func TestMonitoringOnlyReadsWhileItIsOn(t *testing.T) {
	clipboard, device, _, tools := newTestClipboard(t, ClipboardOptions{})
	ctx := context.Background()

	if err := clipboard.SetRegionVisible(ctx, true); err != nil {
		t.Fatalf("SetRegionVisible failed: %v", err)
	}
	if clipboard.Show() {
		t.Error("the region is shown while monitoring is off")
	}
	if err := clipboard.SetMonitoring(ctx, true); err != nil {
		t.Fatalf("SetMonitoring(true) failed: %v", err)
	}
	if !clipboard.Show() {
		t.Error("monitoring is on and the region is not shown")
	}
	waitFor(t, "the first poll", func() bool { return device.readCount() >= 1 })

	if err := clipboard.SetMonitoring(ctx, false); err != nil {
		t.Fatalf("SetMonitoring(false) failed: %v", err)
	}
	if clipboard.Show() {
		t.Error("the region is still shown after monitoring was turned off")
	}
	after := device.readCount()
	time.Sleep(4 * domain.ClipboardMinPollInterval)
	if got := device.readCount(); got != after {
		t.Errorf("the clipboard was read %d more times after monitoring was turned off", got-after)
	}

	settings, _ := tools.Load(ctx)
	if settings.ClipboardMonitoring {
		t.Error("the switch was not saved as off")
	}
	if tools.saves == 0 {
		t.Error("flipping the switch wrote nothing")
	}
}

// Turning monitoring off waits for the loop: the poll goroutine is gone by
// the time the switch reports it.
func TestMonitoringStopEndsTheLoop(t *testing.T) {
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{})
	ctx := context.Background()
	if err := clipboard.SetMonitoring(ctx, true); err != nil {
		t.Fatalf("SetMonitoring(true) failed: %v", err)
	}
	waitFor(t, "the first poll", func() bool { return device.readCount() >= 1 })

	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := clipboard.SetMonitoring(ctx, false); err != nil {
			t.Errorf("SetMonitoring(false) failed: %v", err)
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("turning monitoring off did not return")
	}

	// Starting again works: the loop is not left in a half-stopped state.
	if err := clipboard.SetMonitoring(ctx, true); err != nil {
		t.Fatalf("SetMonitoring(true) after a stop failed: %v", err)
	}
	before := device.readCount()
	waitFor(t, "a poll after restarting", func() bool { return device.readCount() > before })
}

func TestSetRegionVisibleIsIndependentOfMonitoring(t *testing.T) {
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{})
	ctx := context.Background()

	if err := clipboard.SetRegionVisible(ctx, true); err != nil {
		t.Fatalf("SetRegionVisible failed: %v", err)
	}
	if clipboard.Settings().ClipboardMonitoring {
		t.Error("showing the region turned monitoring on")
	}
	time.Sleep(2 * domain.ClipboardMinPollInterval)
	if got := device.readCount(); got != 0 {
		t.Errorf("the clipboard was read %d times while monitoring is off", got)
	}
	if !clipboard.Settings().ClipboardRegionVisible {
		t.Error("the visibility switch was not saved")
	}
}

// A settings file that cannot be written must not take the switch with it.
func TestSwitchesSurviveASaveFailure(t *testing.T) {
	tools := &fakeTools{err: errors.New("the settings file is read-only")}
	clipboard, _, _, _ := newTestClipboard(t, ClipboardOptions{Tools: tools})
	if err := clipboard.SetMonitoring(context.Background(), true); err == nil {
		t.Error("SetMonitoring reported no error for a failed save")
	}
	if !clipboard.Settings().ClipboardMonitoring {
		t.Error("the switch the user flipped was dropped")
	}
}

// A history file that cannot be read must not keep the tool from working.
func TestCorruptHistoryDoesNotStopTheTool(t *testing.T) {
	store := &fakeStore{loadErr: errors.New("the history file is not json")}
	device := &fakeDevice{}
	clipboard, err := NewClipboard(ClipboardOptions{Device: device, Store: store, Tools: &fakeTools{}})
	if err != nil {
		t.Fatalf("NewClipboard failed over an unreadable history: %v", err)
	}
	t.Cleanup(clipboard.Stop)
	if clipboard.HistoryError() == nil {
		t.Error("the unreadable history was not reported")
	}
	device.set(textReading("still works"))
	clipboard.Read(context.Background())
	if got := len(clipboard.History(0)); got != 1 {
		t.Errorf("the tool recorded %d entries after an unreadable history, want 1", got)
	}
}

// A settings file that cannot be read is the user's configuration being
// unavailable, which is worth failing over rather than guessing at.
func TestUnreadableSettingsStopTheTool(t *testing.T) {
	store := &fakeStore{}
	device := &fakeDevice{}
	if _, err := NewClipboard(ClipboardOptions{
		Device: device,
		Store:  store,
		Tools:  &failingTools{},
	}); err == nil {
		t.Error("NewClipboard started over an unreadable settings file")
	}
}

// failingTools refuses to load, as a settings file with the wrong owner or
// unreadable bytes would.
type failingTools struct{ fakeTools }

func (f *failingTools) Load(context.Context) (domain.ToolSettings, error) {
	return domain.ToolSettings{}, errors.New("the settings file is unreadable")
}

// prune is where the history's bound lives, so it is tested on its own, with
// timestamps the test decides rather than whatever a read happened to stamp.
func TestPruneOrdersAndBoundsTheHistory(t *testing.T) {
	base := time.Date(2026, 2, 3, 9, 0, 0, 0, time.UTC)
	entry := func(name string, ago time.Duration, pinned bool) domain.ClipboardEntry {
		return domain.ClipboardEntry{
			ID:          name,
			Content:     name,
			ContentHash: "sha256:" + name,
			Kind:        domain.ClipKindText,
			Pinned:      pinned,
			CopiedAt:    base.Add(-ago),
			CreatedAt:   base.Add(-ago),
		}
	}
	names := func(entries []domain.ClipboardEntry) []string {
		out := make([]string, 0, len(entries))
		for _, e := range entries {
			out = append(out, e.Content)
		}
		return out
	}

	cases := []struct {
		name    string
		keep    int
		entries []domain.ClipboardEntry
		want    []string
	}{
		{
			name:    "newest first",
			keep:    5,
			entries: []domain.ClipboardEntry{entry("old", 2*time.Hour, false), entry("new", time.Hour, false)},
			want:    []string{"new", "old"},
		},
		{
			name:    "over the limit drops the oldest",
			keep:    3,
			entries: []domain.ClipboardEntry{entry("one", 3*time.Hour, false), entry("two", 2*time.Hour, false), entry("three", time.Hour, false), entry("four", 0, false)},
			want:    []string{"four", "three", "two"},
		},
		{
			name:    "a pinned entry past the limit stays",
			keep:    2,
			entries: []domain.ClipboardEntry{entry("pinned", 5*time.Hour, true), entry("two", 2*time.Hour, false), entry("three", time.Hour, false), entry("four", 0, false)},
			want:    []string{"four", "three", "pinned"},
		},
		{
			name: "entries that cannot be shown go",
			keep: 5,
			entries: []domain.ClipboardEntry{
				entry("keep", time.Hour, false),
				{ID: "blank", ContentHash: "sha256:blank", Kind: domain.ClipKindText, CopiedAt: base},
				{ID: "nohash", Content: "x", Kind: domain.ClipKindText, CopiedAt: base},
			},
			want: []string{"keep"},
		},
		{
			name:    "an empty history stays empty",
			keep:    3,
			entries: nil,
			want:    []string{},
		},
	}
	for _, c := range cases {
		clipboard := &Clipboard{keep: c.keep}
		got := names(clipboard.prune(c.entries))
		if len(got) != len(c.want) {
			t.Errorf("%s: pruned to %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: pruned to %v, want %v", c.name, got, c.want)
				break
			}
		}
	}

	// A pinned entry is never evicted, however many unpinned ones arrive.
	clipboard := &Clipboard{keep: 2}
	entries := []domain.ClipboardEntry{entry("pinned", 9*time.Hour, true)}
	for i := 0; i < 6; i++ {
		entries = append(entries, entry(string(rune('a'+i)), time.Duration(6-i)*time.Minute, false))
	}
	kept := clipboard.prune(entries)
	found := false
	for _, e := range kept {
		if e.Content == "pinned" {
			found = true
		}
	}
	if !found {
		t.Errorf("the pinned entry was evicted: %v", names(kept))
	}
	if len(kept) != 3 {
		t.Errorf("prune kept %d entries, want the limit plus the pinned one: %v", len(kept), names(kept))
	}
}

// Starting the tool again obeys the switches the last session saved.
func TestClipboardStartRespectsTheSavedSwitch(t *testing.T) {
	tools := &fakeTools{settings: domain.ToolSettings{
		ClipboardMonitoring:    true,
		ClipboardRegionVisible: true,
	}}
	clipboard, device, _, _ := newTestClipboard(t, ClipboardOptions{Tools: tools})
	clipboard.Start(context.Background())
	waitFor(t, "the first poll from Start", func() bool { return device.readCount() >= 1 })
	if !clipboard.Show() {
		t.Error("a saved switch did not bring the region back")
	}
}
