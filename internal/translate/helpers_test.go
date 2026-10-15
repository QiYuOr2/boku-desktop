package translate

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"boku/internal/domain"
)

// The doubles the translation tool is assembled over: a clipboard device the
// test decides the content of, a settings store in memory, a secret store in
// memory, and a clock the test moves by hand.

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

// WriteImage puts a picture on the clipboard, which is what the selection
// tool putting back what it borrowed does.
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

func textReading(text string) domain.ClipContent {
	return domain.ClipContent{Kind: domain.ClipKindText, Text: text, Display: text}
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

// memoryStore is a domain.SecretStore in memory: the tool tests do not depend
// on the file store.
type memoryStore struct {
	mu     sync.Mutex
	values map[string]string
	meta   map[string]domain.SecretMeta
}

func newMemoryStore() *memoryStore {
	return &memoryStore{values: map[string]string{}, meta: map[string]domain.SecretMeta{}}
}

func (s *memoryStore) Get(_ context.Context, name string) (domain.Secret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[name]
	if !ok {
		return domain.Secret{}, fmt.Errorf("%w: %s", domain.ErrSecretNotFound, name)
	}
	return domain.Secret{Name: name, Value: []byte(value), Meta: s.meta[name]}, nil
}

func (s *memoryStore) Unlock(context.Context, string) error { return nil }

func (s *memoryStore) Lock() {}

func (s *memoryStore) Unlocked() bool { return true }

func (s *memoryStore) Put(_ context.Context, name string, value []byte, meta domain.SecretMeta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.meta[name]; ok && meta.CreatedAt.IsZero() {
		meta.CreatedAt = existing.CreatedAt
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = time.Now().UTC()
	}
	s.values[name] = string(value)
	s.meta[name] = meta
	return nil
}

func (s *memoryStore) Delete(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.values[name]; !exists {
		return fmt.Errorf("%w: %s", domain.ErrSecretNotFound, name)
	}
	delete(s.values, name)
	delete(s.meta, name)
	return nil
}

func (s *memoryStore) List(_ context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.values))
	for name := range s.values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (s *memoryStore) Meta(_ context.Context, name string) (domain.SecretMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, ok := s.meta[name]
	if !ok {
		return domain.SecretMeta{}, fmt.Errorf("%w: %s", domain.ErrSecretNotFound, name)
	}
	return meta, nil
}
