package ui

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/egoist/mygo"

	"boku/internal/credential"
	"boku/internal/domain"
	"boku/internal/settings"
	"boku/internal/translate"
	"boku/internal/translate/free"
)

type testTranslateFixture struct {
	service *translate.Translate
	device  *testDevice
	reply   *testTranslator
}

func testStateWith(t testing.TB) (*State, *testTranslateFixture, *testShortcuts) {
	t.Helper()
	dir := t.TempDir()
	store, err := credential.Open(filepath.Join(dir, credential.FileName))
	if err != nil {
		t.Fatal(err)
	}
	section := translate.NewSection()
	tools, err := settings.New(filepath.Join(dir, "settings.json"), section)
	if err != nil {
		t.Fatal(err)
	}
	reply := &testTranslator{Translator: free.NewMicrosoft(nil)}
	registry := translate.NewRegistry()
	if err := registry.Register(reply); err != nil {
		t.Fatal(err)
	}
	device := &testDevice{}
	service, err := translate.NewTranslate(translate.TranslateOptions{
		Translators: registry, Secrets: store, Section: section, Settings: tools, Clipboard: device,
	})
	if err != nil {
		t.Fatal(err)
	}
	shortcuts := &testShortcuts{}
	state := New(Config{Service: service, Shortcuts: shortcuts})
	return state, &testTranslateFixture{service: service, device: device, reply: reply}, shortcuts
}

type testTranslator struct {
	domain.Translator
	gate <-chan struct{}
}

func (t *testTranslator) Translate(ctx context.Context, _ domain.Credentials, req domain.TranslationRequest) (domain.TranslationResult, error) {
	if t.gate != nil {
		select {
		case <-t.gate:
		case <-ctx.Done():
			return domain.TranslationResult{}, ctx.Err()
		}
	}
	return domain.TranslationResult{
		SourceText: req.Text, TranslatedText: "译文", SourceLang: req.SourceLang,
		TargetLang: req.TargetLang, ProviderID: t.ID(),
	}, nil
}

type testDevice struct{ reading domain.ClipContent }

func (d *testDevice) set(reading domain.ClipContent)                   { d.reading = reading }
func (d *testDevice) Read(context.Context) (domain.ClipContent, error) { return d.reading, nil }
func (*testDevice) WriteText(context.Context, string) error            { return nil }
func (*testDevice) WriteFiles(context.Context, []string) error         { return nil }
func (*testDevice) WriteImage(context.Context, []byte) error           { return nil }

type testShortcuts struct {
	mu            sync.Mutex
	accelerator   string
	fn            func()
	registrations int
	releases      int
	refusals      int
	err           error
	working       int
}

func (s *testShortcuts) Register(accelerator string, fn func()) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registrations++
	switch {
	case s.working > 0:
		s.working--
	case s.err != nil:
		s.refusals++
		return s.err
	}
	s.accelerator, s.fn = accelerator, fn
	return nil
}

func (s *testShortcuts) refuseAfter(taken int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.working = taken
	s.err = errors.New("the combination is already registered")
}

func (s *testShortcuts) refused() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refusals
}

func (s *testShortcuts) UnregisterAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releases++
	s.accelerator, s.fn = "", nil
}

func (s *testShortcuts) bound() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accelerator
}

func (s *testShortcuts) callback() func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fn
}

func (s *testShortcuts) counts() (registrations, releases int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.registrations, s.releases
}

func (s *testShortcuts) failWith(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func translateState(t testing.TB) (*State, *testTranslateFixture, *testCard) {
	t.Helper()
	state, fixture, _ := testStateWith(t)
	card := &testCard{}
	state.cardWindows = func(onClosed, onBlur, onFocus func()) CardWindow {
		card.onClosed, card.onBlur, card.onFocus = onClosed, onBlur, onFocus
		return card
	}
	state.cursor, state.cursorSet = mygo.Point{X: 400, Y: 300}, true
	return state, fixture, card
}

type testCard struct {
	mu                        sync.Mutex
	shown                     bool
	bounds                    mygo.Rectangle
	onClosed, onBlur, onFocus func()
	updates                   chan func()
}

func (c *testCard) ShowInactive() { c.mu.Lock(); c.shown = true; c.mu.Unlock() }
func (*testCard) Focus()          {}
func (c *testCard) Hide()         { c.mu.Lock(); c.shown = false; c.mu.Unlock() }
func (c *testCard) Update(fn func()) {
	if c.updates != nil {
		c.updates <- fn
		return
	}
	fn()
}
func (c *testCard) SetContentBounds(r mygo.Rectangle) { c.mu.Lock(); c.bounds = r; c.mu.Unlock() }
func (c *testCard) ContentBounds() mygo.Rectangle     { c.mu.Lock(); defer c.mu.Unlock(); return c.bounds }
func (c *testCard) OnClosed(fn func()) func()         { c.onClosed = fn; return func() {} }
func (c *testCard) OnBlur(fn func()) func()           { c.onBlur = fn; return func() {} }
func (c *testCard) OnFocus(fn func()) func()          { c.onFocus = fn; return func() {} }
func (c *testCard) visible() bool                     { c.mu.Lock(); defer c.mu.Unlock(); return c.shown }
