package clipboard

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"boku/internal/domain"
	"boku/internal/kit"
)

// The doubles the clipboard feature is tested over, shared by more than one
// test file in this package: the device, the history store, the settings
// store, and a clock the test drives.

// fakeDevice is a domain.ClipboardDevice whose content the test decides.
type fakeDevice struct {
	// now stamps what the device hands back, so that a test's own clock
	// decides when a copy happened rather than the wall clock.
	now func() time.Time

	// readDelay and writeDelay stand in for a platform clipboard that is
	// being held by something else: the call takes that long to come back.
	// They are set before the device is used, never while it is.
	readDelay  time.Duration
	writeDelay time.Duration

	mu      sync.Mutex
	reading domain.ClipContent
	err     error
	reads   int
	writes  []string
	files   [][]string
	images  [][]byte
	writeEr error
}

// failWrite makes the next write fail, the way a platform that refuses the
// format does.
func (d *fakeDevice) failWrite(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writeEr = err
}

func (d *fakeDevice) set(reading domain.ClipContent) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.now != nil {
		reading.CapturedAt = d.now()
	}
	d.reading = reading
}

func (d *fakeDevice) fail(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.err = err
}

func (d *fakeDevice) Read(context.Context) (domain.ClipContent, error) {
	if delay := d.readDelay; delay > 0 {
		time.Sleep(delay)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.reads++
	if d.err != nil {
		return domain.ClipContent{}, d.err
	}
	return d.reading, nil
}

func (d *fakeDevice) WriteText(_ context.Context, text string) error {
	if delay := d.writeDelay; delay > 0 {
		time.Sleep(delay)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.writeEr != nil {
		return d.writeEr
	}
	d.writes = append(d.writes, text)
	d.reading = domain.ClipContent{
		Kind: domain.ClipKindText,
		Text: text,
	}
	if d.now != nil {
		d.reading.CapturedAt = d.now()
	}
	return nil
}

func (d *fakeDevice) WriteFiles(_ context.Context, paths []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.writeEr != nil {
		return d.writeEr
	}
	d.files = append(d.files, append([]string(nil), paths...))
	d.reading = domain.ClipContent{Kind: domain.ClipKindFile, Files: append([]string(nil), paths...)}
	if d.now != nil {
		d.reading.CapturedAt = d.now()
	}
	return nil
}

// WriteImage puts a picture on the clipboard, which is what a click on an
// image in the history does.
func (d *fakeDevice) WriteImage(_ context.Context, png []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.writeEr != nil {
		return d.writeEr
	}
	d.images = append(d.images, append([]byte(nil), png...))
	return nil
}

func (d *fakeDevice) readCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.reads
}

func (d *fakeDevice) written() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.writes...)
}

func (d *fakeDevice) writtenFiles() [][]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([][]string, 0, len(d.files))
	for _, paths := range d.files {
		out = append(out, append([]string(nil), paths...))
	}
	return out
}

func (d *fakeDevice) writtenImages() [][]byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([][]byte, 0, len(d.images))
	for _, png := range d.images {
		out = append(out, append([]byte(nil), png...))
	}
	return out
}

// fakeStore is the history, in memory.
type fakeStore struct {
	mu      sync.Mutex
	entries []domain.ClipboardEntry
	saves   int
	loadErr error
	saveErr error
	// images is the image store beside the history: the bytes of a copied
	// picture, under the reference its entry keeps.
	images map[string][]byte
}

func (s *fakeStore) LoadEntries(context.Context) ([]domain.ClipboardEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	return append([]domain.ClipboardEntry(nil), s.entries...), nil
}

// SaveImage is the image store: the same reference saved twice is the same
// picture, which is what the real store does too.
func (s *fakeStore) SaveImage(_ context.Context, ref string, png []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return s.saveErr
	}
	if s.images == nil {
		s.images = map[string][]byte{}
	}
	if _, ok := s.images[ref]; ok {
		return nil
	}
	s.images[ref] = append([]byte(nil), png...)
	return nil
}

func (s *fakeStore) LoadImage(_ context.Context, ref string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	png, ok := s.images[ref]
	if !ok {
		return nil, fmt.Errorf("%w: no picture was kept", domain.ErrUnsupportedClipboardFormat)
	}
	return append([]byte(nil), png...), nil
}

func (s *fakeStore) DeleteImage(_ context.Context, ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.images, ref)
	return nil
}

// keptImages is what the image store holds, for a test to look at.
func (s *fakeStore) keptImages() map[string][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string][]byte, len(s.images))
	for ref, png := range s.images {
		out[ref] = append([]byte(nil), png...)
	}
	return out
}

func (s *fakeStore) SaveEntries(_ context.Context, entries []domain.ClipboardEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saves++
	if s.saveErr != nil {
		return s.saveErr
	}
	s.entries = append([]domain.ClipboardEntry(nil), entries...)
	return nil
}

func (s *fakeStore) stored() []domain.ClipboardEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.ClipboardEntry(nil), s.entries...)
}

// fakeTools is the settings store, in memory.
type fakeTools struct {
	mu       sync.Mutex
	settings domain.ToolSettings
	saves    int
	err      error
}

func (f *fakeTools) Load(context.Context) (domain.ToolSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.settings, nil
}

func (f *fakeTools) Save(_ context.Context, settings domain.ToolSettings) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves++
	if f.err != nil {
		return f.err
	}
	f.settings = settings
	return nil
}

// Update is the store's read-modify-write, in one critical section.
func (f *fakeTools) Update(_ context.Context, change func(*domain.ToolSettings) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	settings := f.settings
	if err := change(&settings); err != nil {
		return err
	}
	f.saves++
	if f.err != nil {
		return f.err
	}
	f.settings = settings.Normalize()
	return nil
}

// testClock is a clock the test moves by hand.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock {
	return &testClock{now: time.Date(2026, 2, 3, 9, 0, 0, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// newTestClipboard assembles the tool over the fakes.
func newTestClipboard(t *testing.T, opts ClipboardOptions) (*Clipboard, *fakeDevice, *fakeStore, *fakeTools) {
	t.Helper()
	device := &fakeDevice{}
	store := &fakeStore{}
	tools := &fakeTools{}
	opts.Device = device
	opts.Store = store
	if opts.Tools == nil {
		opts.Tools = tools
	}
	if opts.Clock == nil {
		opts.Clock = newTestClock()
	}
	// The device stamps what it hands back with the same clock the service
	// reads, so a test compares one timeline rather than two.
	if clock, ok := opts.Clock.(*testClock); ok {
		device.now = clock.Now
	}
	clipboard, err := NewClipboard(opts)
	if err != nil {
		t.Fatalf("NewClipboard failed: %v", err)
	}
	t.Cleanup(clipboard.Stop)
	return clipboard, device, store, tools
}

// read polls the clipboard the way the history window does, one tick of the
// test clock later: two copies are never the same instant, so the order the
// history keeps is the order they happened in.
//
// It uses Read rather than Open: Open is the interface's non-blocking
// request, and a test wants the reading to have happened by the next line.
func read(clipboard *Clipboard, clock *testClock, device *fakeDevice, text string) {
	clock.advance(time.Second)
	device.set(textReading(text))
	clipboard.Read(context.Background())
}

func textReading(text string) domain.ClipContent {
	return domain.ClipContent{Kind: domain.ClipKindText, Text: text, Display: text}
}

// textEntry is a recorded copy of some text, as the history holds one.
func textEntry(content string) domain.ClipboardEntry {
	return domain.ClipboardEntry{
		ID:          "entry-" + content,
		Content:     content,
		ContentHash: kit.ClipContentHash(content),
		Kind:        kit.ClassifyClip(content),
		Display:     kit.ClipDisplay(content),
		CopiedAt:    time.Now().UTC(),
		CreatedAt:   time.Now().UTC(),
	}
}

// waitFor polls a condition until it holds, and fails the test if it never
// does: waiting on a goroutine is the one place a test may sleep.
func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
