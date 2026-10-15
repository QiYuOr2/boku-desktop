package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"boku/internal/domain"
	"boku/internal/tools"
)

// maxCopiedTextBytes bounds what is written back to the clipboard: a copy
// from the interface is a newline-free line, and anything larger is a
// mistake rather than a paste.
const maxCopiedTextBytes = 1 << 20

// Clipboard is the clipboard tool: it reads what the platform currently
// holds, records it as history, and writes a chosen entry back.
//
// It is the only place that decides what the user's clipboard means, and it
// survives a window closing: the history is the service's, not a view's.
// Every method is safe to call from any goroutine.
type Clipboard struct {
	device   domain.ClipboardDevice
	store    domain.ClipboardStore
	tools    domain.ToolStore
	now      func() time.Time
	emit     func()
	interval time.Duration
	keep     int

	// watcher is the running poll, or nil while monitoring is off.
	watcher *Watcher
	cancel  context.CancelFunc
	done    chan struct{}

	// dirty says the history has changed since it was last written, and
	// writing says a background write is in progress. Together they keep at
	// most one write in flight.
	dirty   bool
	writing bool

	mu         sync.Mutex
	latest     *domain.ClipRecord
	history    []domain.ClipboardEntry
	historyErr error
	settings   domain.ToolSettings
	writeAt    time.Time
	writeValue string
	writeKind  domain.ClipKind
	// ignoreUntil is the end of a window in which the clipboard is not
	// recorded at all, whoever wrote it. It is what a synthetic copy — the
	// one the selection tool makes to read a text that no application
	// publishes — asks for (see IgnoreTheNextCopy).
	ignoreUntil time.Time
	// lastCopy is what the last click on a recorded copy wrote, and why it
	// failed when it did.
	lastCopy    string
	lastCopyErr error
	// readAt is when the clipboard was last read. A window that opens can
	// see that a reading has just happened and ask for a frame instead of
	// spending another read.
	readAt time.Time
}

// ClipboardOptions is what the service is assembled from. Only Device and
// Store are required; the rest has a working default.
type ClipboardOptions struct {
	Device domain.ClipboardDevice
	Store  domain.ClipboardStore
	// Tools stores the switches, including the poll interval and the record
	// range. A nil store keeps them in memory, which is what a test wants
	// and what an assembly without a settings file must still survive.
	Tools domain.ToolStore
	Clock Clock
	Emit  func()
	// Keep overrides the record range the settings hold.
	Keep int
	// PollIn overrides the interval the settings hold.
	PollIn time.Duration
}

// NewClipboard builds the service and reads the settings it was given.
func NewClipboard(opts ClipboardOptions) (*Clipboard, error) {
	if opts.Device == nil {
		return nil, fmt.Errorf("%w: the clipboard has no device", domain.ErrInvalidArgument)
	}
	if opts.Store == nil {
		return nil, fmt.Errorf("%w: the clipboard has no store", domain.ErrInvalidArgument)
	}
	clock := opts.Clock
	if clock == nil {
		clock = SystemClock{}
	}
	clipboard := &Clipboard{
		device: opts.Device,
		store:  opts.Store,
		tools:  opts.Tools,
		now:    clock.Now,
		emit:   opts.Emit,
		// An option overrides the record range the settings hold, which is
		// what a test wants; load fills it in from the settings otherwise,
		// and prunes the stored history to it as it reads it.
		keep: opts.Keep,
	}
	if err := clipboard.load(context.Background()); err != nil {
		return nil, err
	}
	// The interval the two switches were read with is the one the poll and
	// the loop-suppression window use; an option overrides it, which is
	// what a test wants.
	clipboard.interval = clipboard.settings.Interval()
	if opts.PollIn >= domain.ClipboardMinPollInterval {
		clipboard.interval = opts.PollIn
	}
	return clipboard, nil
}

// load reads the settings and the history from the store.
//
// A settings file that cannot be read is fatal: it is the user's own
// configuration, and starting with a different one silently would be worse
// than not starting. A history file that cannot be read is not: it is a
// convenience, and it is kept as the last error so a test can see it. A
// corrupt history must never cost the user their switches.
//
// The history is pruned as it is read, so a record range the user shortened
// while the application was not running is honored before anything is shown.
func (c *Clipboard) load(ctx context.Context) error {
	if c.tools != nil {
		settings, err := c.tools.Load(ctx)
		if err != nil {
			return fmt.Errorf("read the clipboard settings: %w", err)
		}
		c.settings = settings.Normalize()
	} else {
		c.settings = domain.ToolSettings{}.Normalize()
	}
	if c.keep <= 0 {
		c.keep = c.settings.HistorySize()
	}
	entries, err := c.store.LoadEntries(ctx)
	if err != nil {
		c.historyErr = err
		entries = nil
	}
	c.history = c.prune(entries)
	return nil
}

// Settings returns the current switches.
func (c *Clipboard) Settings() domain.ToolSettings {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.settings
}

// SetEmit sets what is told that the clipboard content or the history
// changed, so the interface can redraw. The assembly wires it to the
// windows; it may be nil, which is what a test wants.
func (c *Clipboard) SetEmit(emit func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.emit = emit
}

// SetRegionVisible shows or hides the clipboard line in the quick window.
// Hiding it stops nothing: what the user turned off is the drawing.
func (c *Clipboard) SetRegionVisible(ctx context.Context, visible bool) error {
	c.mu.Lock()
	c.settings.ClipboardRegionVisible = visible
	settings := c.settings
	c.mu.Unlock()
	return c.saveSettings(ctx, settings)
}

// SetMonitoring starts or stops reading the clipboard. Stopping ends the
// poll and waits for it, so that "off" means no further read rather than a
// read in flight.
func (c *Clipboard) SetMonitoring(ctx context.Context, enabled bool) error {
	c.mu.Lock()
	if c.settings.ClipboardMonitoring == enabled {
		c.mu.Unlock()
		return nil
	}
	c.settings.ClipboardMonitoring = enabled
	settings := c.settings
	c.mu.Unlock()
	if err := c.saveSettings(ctx, settings); err != nil {
		return err
	}
	if enabled {
		c.begin(ctx)
		return nil
	}
	c.stop()
	return nil
}

// saveSettings writes the switches, and remembers them even when the file
// could not be written: the switch the user just flipped has taken effect.
func (c *Clipboard) saveSettings(ctx context.Context, settings domain.ToolSettings) error {
	if c.tools == nil {
		return nil
	}
	return c.tools.Save(ctx, settings)
}

// SetHistorySize chooses how many of the most recent copies are kept, and
// therefore how many the history page lists.
//
// The history is pruned to the new bound here rather than at the next
// reading: a user who asked for the last ten copies should not keep the
// longer list on disk until something else is copied. The entries that go are
// gone — the range is what the file holds, not what the page shows — so the
// settings form says so before the choice is made.
func (c *Clipboard) SetHistorySize(ctx context.Context, size int) error {
	if !domain.ValidClipboardHistorySize(size) {
		size = domain.ClipboardDefaultHistorySize
	}
	c.mu.Lock()
	if c.settings.ClipboardHistorySize == size {
		c.mu.Unlock()
		return nil
	}
	c.settings.ClipboardHistorySize = size
	settings := c.settings
	c.keep = size
	c.history = c.prune(c.history)
	emit := c.emit
	c.mu.Unlock()

	err := c.saveSettings(ctx, settings)
	c.persist()
	if emit != nil {
		emit()
	}
	return err
}

// Start begins monitoring when the settings say so. main.go calls it once
// the application is ready.
func (c *Clipboard) Start(ctx context.Context) {
	if !c.Settings().ClipboardMonitoring {
		return
	}
	c.begin(ctx)
}

// Stop ends monitoring and waits for the poll loop, and writes the history
// once more. The application calls it as it quits.
func (c *Clipboard) Stop() {
	c.stop()
	c.flush()
}

// begin starts the poll loop if it is not already running.
func (c *Clipboard) begin(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.Lock()
	if c.cancel != nil {
		c.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	watcher := &Watcher{
		Device:   c.device,
		Interval: c.interval,
		Now:      c.now,
		Emit:     c.record,
	}
	done := make(chan struct{})
	c.cancel, c.done, c.watcher = cancel, done, watcher
	c.mu.Unlock()

	// A poll that runs for hours is worth one line saying it started: it is
	// the only record of what the application was doing at the time.
	slog.Info("clipboard monitoring started", "interval", watcher.Interval.String())

	go func() {
		defer close(done)
		if err := watcher.Run(runCtx); err != nil {
			// The loop only gives up on an error it cannot continue past —
			// a device that does not exist — and nothing else will say so.
			slog.Warn("the clipboard poll stopped", "error", err)
		}
	}()
}

// stop cancels the poll loop and waits for it to return.
func (c *Clipboard) stop() {
	c.mu.Lock()
	cancel, done := c.cancel, c.done
	c.cancel, c.done, c.watcher = nil, nil, nil
	c.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	if done != nil {
		<-done
	}
	slog.Info("clipboard monitoring stopped")
}

// Show reports whether the interface has a reason to read the clipboard: the
// region is drawn and the content it would draw is being collected.
func (c *Clipboard) Show() bool {
	settings := c.Settings()
	return settings.ClipboardRegionVisible && settings.ClipboardMonitoring
}

// Latest is the most recent reading, and whether there is one.
func (c *Clipboard) Latest() (domain.ClipRecord, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.latest == nil {
		return domain.ClipRecord{}, false
	}
	return *c.latest, true
}

// Open asks for one reading of the clipboard, whatever the monitoring switch
// says, and returns at once. It is what opening the history window does: the
// user asked to see the clipboard, and that is a read the switch was never
// about.
//
// The reading happens on a goroutine rather than under the caller, because
// the caller is drawing a frame. A clipboard read on Windows can wait on
// whatever else is holding the clipboard, and every millisecond of that wait
// is a millisecond the window spends drawn but empty. The frames the reading
// leads to are asked for by the emit hook, when there is something to show.
func (c *Clipboard) Open(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	go c.Read(ctx)
}

// Read reads the clipboard once and waits for it, which is what a caller off
// the drawing thread — and a test — wants. It is what Open does in the
// background.
func (c *Clipboard) Read(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	c.poll(ctx)
}

// PolledRecently reports whether the clipboard was read within the last
// interval. A window that has just opened can ask before spending a read on
// content it already has.
func (c *Clipboard) PolledRecently() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.readAt.IsZero() && c.now().Sub(c.readAt) < c.interval
}

// poll reads the device once, suppresses a reading that is the application's
// own write coming back, and records what is left.
func (c *Clipboard) poll(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	reading, err := c.device.Read(ctx)
	if err != nil {
		return
	}
	c.noteRead()
	if reading.CapturedAt.IsZero() {
		reading.CapturedAt = c.now()
	}
	if !reading.Valid() {
		return
	}
	if c.suppressed(reading) {
		return
	}
	c.record(reading)
}

// noteRead remembers when the clipboard was last read, which is what keeps an
// opened window from reading it again immediately.
func (c *Clipboard) noteRead() {
	c.mu.Lock()
	c.readAt = c.now()
	c.mu.Unlock()
}

// Suppress keeps the next reading from being recorded as a new copy. It is
// what a write that is about to happen on its own goroutine calls: the write
// and the read that sees it are not ordered, so the window is opened before
// the write rather than after it.
func (c *Clipboard) Suppress() {
	c.mu.Lock()
	c.writeAt = c.now()
	c.mu.Unlock()
}

// copyIgnoreWindow is how long a synthetic copy's value is kept out of the
// history. It covers the copy, the read that takes the text out of it and the
// write that puts the clipboard back, with room for a poll that was already on
// its way.
const copyIgnoreWindow = 2 * time.Second

// IgnoreTheNextCopy keeps what the clipboard reports for the next moment out of
// the history, whoever wrote it.
//
// It is what the selection tool calls just before it asks an application to
// copy: that copy is not something the user asked to record — it is a question
// put to the application in front — and the tool puts the clipboard back as it
// found it straight afterwards. Unlike Suppress this does not compare the
// reading with a value this application wrote, because it did not write it.
func (c *Clipboard) IgnoreTheNextCopy() {
	c.mu.Lock()
	c.ignoreUntil = c.now().Add(copyIgnoreWindow)
	c.mu.Unlock()
}

// record turns a reading into the current content and, when it is new,
// into a history entry. It is the watcher's callback and runs on the
// watcher's goroutine.
func (c *Clipboard) record(reading domain.ClipContent) {
	record := Record(reading)
	if record.Content == "" && record.Display == "" {
		// Rich text with nothing readable in it: there is nothing to show
		// and nothing to match, so it is not an entry.
		return
	}
	if len(reading.Image) > 0 && record.ImageRef != "" && !c.keepImage(reading.Image, record.ImageRef) {
		// The picture could not be kept. The entry still goes in — the user
		// copied something — and a click on it says the picture is gone
		// rather than pasting the line "[图片]".
		record.ImageRef = ""
	}
	c.mu.Lock()
	c.latest = &record
	changed := c.append(record)
	emit := c.emit
	c.mu.Unlock()
	if changed {
		c.persist()
	}
	if emit != nil {
		emit()
	}
}

// keepImage puts the bytes of a copied picture where a click on its entry can
// find them again.
//
// This is the only moment they can be had: by the time anyone clicks, the
// clipboard holds something else. It runs on the watcher's goroutine, which is
// why it is one file written once per copy rather than part of the history
// write — the history is written in the background, and this has to be there
// before the entry that names it.
func (c *Clipboard) keepImage(png []byte, ref string) bool {
	return c.store.SaveImage(context.Background(), ref, png) == nil
}

// append adds a reading to the history, unless it is already there. The
// caller holds the lock. It reports whether the history changed.
//
// append adds a reading to the history, unless it is already there. The
// caller holds the lock. It reports whether the history changed, which is
// what decides whether it has to be written again.
//
// The same content copied again moves its entry back to the top instead of
// adding a second one: the history is what was copied, not how often. A
// reading of what is already on the clipboard — the poll asking again, or a
// window being opened — is not a change, so it does not rewrite the file.
func (c *Clipboard) append(record domain.ClipRecord) bool {
	if record.Hash == "" || record.Content == "" {
		return false
	}
	for index := range c.history {
		if c.history[index].ContentHash != record.Hash {
			continue
		}
		// Only a move that the list would show counts: within the same
		// second, the entry is already where it belongs.
		if record.CapturedAt.Sub(c.history[index].CopiedAt) < time.Second {
			return false
		}
		c.history[index].CopiedAt = record.CapturedAt
		c.history = c.prune(c.history)
		return true
	}
	id, err := domain.NewID()
	if err != nil {
		// An entry nobody can identify is worse than no entry: the
		// clipboard is a convenience, and it must not take the
		// application down with it.
		return false
	}
	c.history = c.prune(append(c.history, domain.ClipboardEntry{
		ID:          id,
		Content:     record.Content,
		ContentHash: record.Hash,
		Kind:        record.Kind,
		CopiedAt:    record.CapturedAt,
		CreatedAt:   c.now(),
		Display:     record.Display,
		Files:       append([]string(nil), record.Files...),
		ImageBytes:  record.ImageBytes,
		ImageRef:    record.ImageRef,
		Note:        record.Note,
	}))
	return true
}

// prune orders the history newest first and drops what is over the limit,
// keeping pinned entries. The caller holds the lock.
func (c *Clipboard) prune(entries []domain.ClipboardEntry) []domain.ClipboardEntry {
	kept := make([]domain.ClipboardEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.Content) == "" || entry.ContentHash == "" {
			continue // an entry that cannot be shown or matched
		}
		kept = append(kept, entry)
	}
	sort.SliceStable(kept, func(i, j int) bool { return newerEntry(kept[i], kept[j]) })
	if len(kept) <= c.keep {
		return kept
	}
	// An entry past the limit that is not pinned goes; a pinned one keeps
	// its place, which is what pinning is for. The list is already newest
	// first, so what stays is what is at its front.
	room := make([]domain.ClipboardEntry, 0, len(kept))
	unpinned := 0
	for _, entry := range kept {
		if entry.Pinned {
			room = append(room, entry)
			continue
		}
		if unpinned < c.keep {
			room = append(room, entry)
			unpinned++
		}
	}
	// The limit bounds the unpinned entries and nothing else: a pinned one
	// is the reason the history may hold more than the limit.
	if unpinned > c.keep {
		room = c.evictOldest(room, unpinned-c.keep)
	}
	c.forgetImages(entries, room)
	return room
}

// forgetImages deletes the pictures of entries that are no longer in the
// history: a picture whose entry is gone could never be put back, and an image
// store that only grows is a directory full of screenshots nobody will see
// again.
//
// It runs on a goroutine of its own. The caller holds the lock and is drawing
// a frame or polling the clipboard, and deleting a file is not worth either.
func (c *Clipboard) forgetImages(before, after []domain.ClipboardEntry) {
	kept := make(map[string]bool, len(after))
	for _, entry := range after {
		if entry.ImageRef != "" {
			kept[entry.ImageRef] = true
		}
	}
	var dropped []string
	for _, entry := range before {
		if entry.ImageRef != "" && !kept[entry.ImageRef] {
			dropped = append(dropped, entry.ImageRef)
		}
	}
	if len(dropped) == 0 {
		return
	}
	go func() {
		for _, ref := range dropped {
			if err := c.store.DeleteImage(context.Background(), ref); err != nil {
				// The entry is gone from the history either way: what stays
				// behind is a file nobody will look at again, so this is a
				// warning rather than a failure.
				slog.Warn("a picture of a dropped entry could not be deleted", "error", err)
			}
		}
	}()
}

// evictOldest drops n unpinned entries, from the oldest back, for a list
// that is newest first. The caller holds the lock.
func (c *Clipboard) evictOldest(entries []domain.ClipboardEntry, n int) []domain.ClipboardEntry {
	for n > 0 && len(entries) > 0 {
		index := -1
		for i := len(entries) - 1; i >= 0; i-- {
			if !entries[i].Pinned {
				index = i
				break
			}
		}
		if index < 0 {
			break
		}
		entries = append(entries[:index], entries[index+1:]...)
		n--
	}
	return entries
}

// History is the recorded history, newest first, at most limit entries. A
// limit of zero or less returns everything the store kept.
func (c *Clipboard) History(limit int) []domain.ClipboardEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.historyLocked(limit)
}

// historyLocked is History for callers that already hold the lock.
func (c *Clipboard) historyLocked(limit int) []domain.ClipboardEntry {
	entries := c.history
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return append([]domain.ClipboardEntry(nil), entries...)
}

// HistoryError is why the stored history could not be read, if it could not.
// The interface says so rather than showing an empty history, which would
// look like "nothing was ever copied".
func (c *Clipboard) HistoryError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.historyErr
}

// Copy writes a recorded copy back to the clipboard, as the thing it was: a
// file list goes back as a file list, an image as an image when its bytes
// are still in hand, and everything else as text. The entry it belongs to
// moves to the top of the history.
//
// The write is remembered for two poll intervals: the loop would otherwise
// read back what this application just wrote and record it as a new copy,
// which is the loop the documentation calls out.
func (c *Clipboard) Copy(ctx context.Context, entry domain.ClipboardEntry) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := c.write(ctx, entry); err != nil {
		return err
	}
	c.mu.Lock()
	c.writeValue = writeFingerprint(entry)
	c.writeKind = entry.Kind
	c.writeAt = c.now()
	c.touch(entry)
	c.mu.Unlock()
	c.persist()
	return nil
}

// CopyAsync puts a recorded copy back on the clipboard and returns at once,
// reporting to done what the write did.
//
// It is what a click on a history row calls. The write itself has to happen
// on the platform's thread, and the caller is that thread, drawing the frame
// the click was in: doing it here would mean the frame — and every frame
// after it until the write returned — is drawn before the clipboard has the
// copy. Nothing is left half-done by returning early: the entry is touched
// before the write, so a click is visible in the list whatever the platform
// answers.
func (c *Clipboard) CopyAsync(entry domain.ClipboardEntry, done func(error)) {
	c.mu.Lock()
	c.writeValue = writeFingerprint(entry)
	c.writeKind = entry.Kind
	c.writeAt = c.now()
	c.touch(entry)
	c.mu.Unlock()
	c.persist()

	go func() {
		err := c.write(context.Background(), entry)
		if done != nil {
			done(err)
		}
	}()
}

// write puts a recorded copy back on the clipboard as the right kind of
// thing.
//
// A file list goes back as files: that is the copy whose value is in being
// pasted into something that takes a file. A picture goes back as a picture,
// loaded from the image store — what a click on one used to do was put the line
// "[图片]" on the clipboard, which is a text nobody can paste anywhere.
// Everything else goes back as the text it was.
func (c *Clipboard) write(ctx context.Context, entry domain.ClipboardEntry) error {
	if entry.Kind == domain.ClipKindImage {
		if entry.ImageRef == "" {
			return fmt.Errorf("%w: this copy is a picture whose bytes were not kept",
				domain.ErrUnsupportedClipboardFormat)
		}
		png, err := c.store.LoadImage(ctx, entry.ImageRef)
		if err != nil {
			return err
		}
		return c.device.WriteImage(ctx, png)
	}
	if len(entry.Files) > 0 {
		return c.device.WriteFiles(ctx, entry.Files)
	}
	value := entry.Content
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%w: there is nothing to copy", domain.ErrInvalidArgument)
	}
	if len(value) > maxCopiedTextBytes {
		return fmt.Errorf("%w: the text is too long to copy", domain.ErrInvalidArgument)
	}
	return c.device.WriteText(ctx, value)
}

// entryHash is what identifies a recorded copy. An entry that has one keeps
// it: it was computed from what the user copied, and recomputing it from the
// one line the history shows would turn one copy into two.
func entryHash(entry domain.ClipboardEntry) string {
	if entry.ContentHash != "" {
		return entry.ContentHash
	}
	if len(entry.Files) > 0 {
		return tools.ClipContentHash("files:" + strings.Join(entry.Files, "\n"))
	}
	return tools.ClipContentHash(entry.Content)
}

// writeFingerprint is what the suppression window compares a reading
// against: the text of a text copy, and the one line that stands for a file
// list.
func writeFingerprint(entry domain.ClipboardEntry) string {
	if entry.Content != "" {
		return strings.TrimSpace(entry.Content)
	}
	return strings.TrimSpace(entry.Display)
}

// touch moves the entry holding a copy back to the top, recording it when
// the history never saw it. The caller holds the lock.
func (c *Clipboard) touch(entry domain.ClipboardEntry) {
	hash := entryHash(entry)
	for index := range c.history {
		if c.history[index].ContentHash != hash {
			continue
		}
		c.history[index].CopiedAt = c.now()
		c.history = c.prune(c.history)
		return
	}
	_ = c.append(domain.ClipRecord{
		Kind:       entry.Kind,
		Content:    entry.Content,
		Display:    entry.Display,
		Hash:       hash,
		Files:      entry.Files,
		ImageBytes: entry.ImageBytes,
		CapturedAt: c.now(),
	})
}

// suppressed reports whether a reading is this application's own write
// coming back.
func (c *Clipboard) suppressed(reading domain.ClipContent) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.suppresses(reading)
}

// suppresses is suppressed without the lock, for callers that hold it.
func (c *Clipboard) suppresses(reading domain.ClipContent) bool {
	if !c.ignoreUntil.IsZero() && c.now().Before(c.ignoreUntil) {
		// A window a synthetic copy opened: nothing read inside it is a copy
		// the user made.
		return true
	}
	if c.writeValue == "" {
		return false
	}
	if c.now().Sub(c.writeAt) > 2*c.interval {
		c.writeValue = ""
		return false
	}
	// A written file list comes back as the same paths; a written text as
	// the same text.
	if reading.Kind != c.writeKind {
		return false
	}
	text := reading.Text
	if text == "" {
		text = reading.Display
	}
	if c.writeKind == domain.ClipKindFile {
		text = strings.Join(reading.Files, "\n")
	}
	return strings.TrimSpace(text) == c.writeValue
}

// newerEntry orders two entries newest first, and breaks a tie by id.
//
// Ids are UUIDv7, so their order is the order they were minted in: two
// copies within the same second still have a stable order, which is what
// keeps the list from reshuffling between frames.
func newerEntry(a, b domain.ClipboardEntry) bool {
	if a.CopiedAt.Equal(b.CopiedAt) {
		return a.ID > b.ID
	}
	return a.CopiedAt.After(b.CopiedAt)
}

// persist asks for the history to be written, and returns at once.
//
// Writing it where the change happened would put a file the size of the
// history — up to a few hundred entries — between the click and the frame
// that shows the click landed. A background writer takes the latest
// snapshot instead, and one already on its way makes the next call a no-op:
// a click during a write is folded into the write after it rather than
// queueing a second one.
func (c *Clipboard) persist() {
	c.mu.Lock()
	c.dirty = true
	if c.writing || c.store == nil {
		c.mu.Unlock()
		return
	}
	c.writing = true
	c.mu.Unlock()

	go func() {
		for {
			c.mu.Lock()
			if !c.dirty {
				c.writing = false
				c.mu.Unlock()
				return
			}
			entries := append([]domain.ClipboardEntry(nil), c.history...)
			c.dirty = false
			c.mu.Unlock()
			// The in-memory history is the working copy and this file is
			// the only one that survives the process: a write that failed
			// is the reason this run's copies are gone at the next launch,
			// and nothing else would say so.
			if err := c.store.SaveEntries(context.Background(), entries); err != nil {
				slog.Warn("the clipboard history could not be written", "error", err)
			}
		}
	}()
}

// settled reports whether no write is pending and none is in flight. It is
// how a test waits for the history to be on disk.
func (c *Clipboard) settled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.dirty && !c.writing
}

// flush writes the history now and waits for it, for the caller that is
// about to stop existing.
func (c *Clipboard) flush() {
	c.mu.Lock()
	entries := append([]domain.ClipboardEntry(nil), c.history...)
	c.dirty = false
	store := c.store
	c.mu.Unlock()
	if store == nil {
		return
	}
	// This is the last write of the run, so a failure here is the one the
	// user is most likely to notice and the least likely to be told about.
	if err := store.SaveEntries(context.Background(), entries); err != nil {
		slog.Warn("the clipboard history could not be written as the application quit", "error", err)
	}
}

// Record is the one reading the service keeps and draws: what the clipboard
// holds, as one line, a hash to match it by, and which kind it is.
//
// Content is the value a history entry puts back on the clipboard: the text
// itself for a copy, and for a file list or an image the paths and the name,
// which is what the platform can write. Display is the line both faces of
// the tool show, so that a list of ten copies stays a list.
func Record(reading domain.ClipContent) domain.ClipRecord {
	switch reading.Kind {
	case domain.ClipKindFile:
		display := tools.ClipFileDisplay(reading.Files)
		content := strings.Join(reading.Files, "\n")
		if content == "" {
			content = display
		}
		return domain.ClipRecord{
			Kind:       reading.Kind,
			Content:    tools.ClipText(content),
			Display:    display,
			Hash:       tools.ClipContentHash("files:" + strings.Join(reading.Files, "\n")),
			Files:      append([]string(nil), reading.Files...),
			ImageBytes: reading.ImageBytes,
			ImageRef:   reading.ImageRef,
			Note:       reading.Observation,
			CapturedAt: reading.CapturedAt,
		}
	case domain.ClipKindImage:
		name := ""
		if len(reading.Files) > 0 {
			name = reading.Files[0]
		}
		display := "[" + tools.ClipImageName(name) + "]"
		// The hash is the picture itself when its bytes came with the
		// reading, so that two screenshots are two entries; without them
		// there is only the name the platform offered, and every nameless
		// picture is then the same picture — which is what used to happen.
		hash := tools.ClipContentHash("image:" + tools.ClipFileName(name))
		if reading.ImageRef != "" {
			hash = tools.ClipContentHash("image:" + reading.ImageRef)
		}
		return domain.ClipRecord{
			Kind:       reading.Kind,
			Content:    display,
			Display:    display,
			Hash:       hash,
			Files:      append([]string(nil), reading.Files...),
			ImageBytes: reading.ImageBytes,
			ImageRef:   reading.ImageRef,
			Note:       reading.Observation,
			CapturedAt: reading.CapturedAt,
		}
	default:
		text := reading.Text
		if strings.TrimSpace(text) == "" && reading.HTML != "" {
			// No plain-text flavour came with the rich one: the text is
			// what the markup shows.
			text = tools.ExtractHTMLText(reading.HTML)
		}
		kept := tools.ClipText(text)
		display := tools.ClipDisplay(kept)
		if display == "" {
			// Rich text with nothing readable in it is still a copy, and
			// the interface has to say something rather than nothing.
			display = tools.ClipDisplay(tools.ExtractHTMLText(reading.HTML))
		}
		return domain.ClipRecord{
			Kind:       tools.ClassifyClip(kept),
			Content:    kept,
			Display:    display,
			Hash:       tools.ClipContentHash(kept),
			CapturedAt: reading.CapturedAt,
		}
	}
}

// errorsIsInvalidArgument reports whether an error is the domain's way of
// saying the caller asked for something impossible.
