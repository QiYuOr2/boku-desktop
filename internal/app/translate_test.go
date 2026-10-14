package app

import (
	"image"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	"boku/internal/usecase"
)

// The translation card is a state machine around a window: it is shown where
// the pointer is, it says what it is doing, it closes itself when it has been
// ignored, and it stays when it has been pinned. The window itself is faked
// here, because creating one waits for an event loop a test does not have.

// testCard is the card the state machine drives, in place of a window.
type testCard struct {
	mu     sync.Mutex
	shown  bool
	focuse int
	hides  int
	bounds mygo.Rectangle

	onClosed func()
	onBlur   func()
	onFocus  func()
}

func (c *testCard) Show()  { c.mu.Lock(); c.shown = true; c.mu.Unlock() }
func (c *testCard) Focus() { c.mu.Lock(); c.focuse++; c.mu.Unlock() }
func (c *testCard) Hide()  { c.mu.Lock(); c.hides++; c.shown = false; c.mu.Unlock() }
func (c *testCard) Update(fn func()) {
	// The card has no frame of its own, so the update runs where it is
	// asked for: that is the state the next frame would draw.
	fn()
}
func (c *testCard) SetContentBounds(r mygo.Rectangle) { c.mu.Lock(); c.bounds = r; c.mu.Unlock() }
func (c *testCard) ContentBounds() mygo.Rectangle     { c.mu.Lock(); defer c.mu.Unlock(); return c.bounds }
func (c *testCard) OnClosed(fn func()) func()         { c.onClosed = fn; return func() {} }
func (c *testCard) OnBlur(fn func()) func()           { c.onBlur = fn; return func() {} }
func (c *testCard) OnFocus(fn func()) func()          { c.onFocus = fn; return func() {} }

// visible is whether the card is on screen.
func (c *testCard) visible() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.shown }

// hidden is how many times the card was taken off screen.
func (c *testCard) hidden() int { c.mu.Lock(); defer c.mu.Unlock(); return c.hides }

// blur and focus are what the platform reports.
func (c *testCard) blur() {
	c.mu.Lock()
	fn := c.onBlur
	c.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (c *testCard) focus() {
	c.mu.Lock()
	fn := c.onFocus
	c.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// translateState is a state whose translation card is faked, with the
// translation tool handed back so that a test decides what it answers.
func translateState(t testing.TB) (*State, *testTranslateFixture, *testCard) {
	t.Helper()
	state, translate, _ := testStateWith(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	card := &testCard{}
	state.cardWindows = func(onClosed, onBlur, onFocus func()) cardWindow {
		card.onClosed, card.onBlur, card.onFocus = onClosed, onBlur, onFocus
		return card
	}
	state.cursor, state.cursorSet = mygo.Point{X: 400, Y: 300}, true
	return state, translate, card
}

// start asks for a translation the way the shortcut does, and returns once
// the card has been shown. The answer arrives on its own.
func start(t *testing.T, state *State) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		state.onTranslateShortcut()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the shortcut callback did not return: something in it waits")
	}
}

// awaitStatus waits for the card to reach a state. It runs on the goroutine
// the test owns and reads the state the update writes, which is the same
// goroutine the main thread would be.
func awaitStatus(t *testing.T, state *State, want translateStatus) {
	t.Helper()
	await(t, func() bool { return state.translation.status == want },
		"the card stayed in state %d, want %d", state.translation.status, want)
}

// await waits for something the card's own goroutine writes, on the goroutine
// the test owns.
func await(t *testing.T, done func() bool, format string, args ...any) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf(format, args...)
}

func TestTranslationCardShowsTheAnswer(t *testing.T) {
	state, translate, card := translateState(t)
	translate.device.set(domain.ClipContent{
		Kind: domain.ClipKindText, Text: "A compiler converts source code.", Display: "A compiler converts source code.",
	})
	start(t, state)

	if !card.visible() {
		t.Error("the card was not shown")
	}
	if r := card.ContentBounds(); r.X != 412 || r.Y != 312 {
		t.Errorf("the card is at %d,%d, want it near the pointer at 400,300", r.X, r.Y)
	}
	awaitStatus(t, state, translateReady)

	if got := state.translation.source; got != "A compiler converts source code." {
		t.Errorf("the source is %q", got)
	}
	if got := state.translation.result.TranslatedText; got != "译文" {
		t.Errorf("the translation is %q", got)
	}
	if got := state.translation.result.ProviderName; got != "DeepSeek" {
		t.Errorf("the card names the provider %q", got)
	}
	// The card draws the translation, and the footer says where it came from.
	texts := cardTexts(t, state)
	for _, want := range []string{"原文", "译文", "A compiler converts source code.", "译文", "AI 翻译 · DeepSeek"} {
		if !contains(texts, want) {
			t.Errorf("the card does not show %q: %q", want, texts)
		}
	}
}

// The annotation lives under the translation, behind a row that opens it: the
// card's job is the translation, and the words a sentence is made of are what
// a reader asks for afterwards.
//
// What is drawn inside is the pieces the translator annotated: a reading above
// the word it belongs to, and a loanword's own word instead when the setting
// asks for it.
func TestTheReadingsOpenUnderTheTranslation(t *testing.T) {
	state, translate, card := translateState(t)
	translate.deepseek.answer(domain.TranslationResult{
		TranslatedText: "编译器是软件",
		SourceLang:     domain.LangJapanese,
		TargetLang:     domain.LangChinese,
		Segments: []domain.Segment{
			{Text: "コンパイラ", Gloss: "compiler", Target: "编译器"},
			{Text: "は", Target: "是"},
			{Text: "機械語", Reading: "キカイゴ", Target: "机器语言"},
			{Text: "ソフト", Gloss: "software", Target: "软件"},
		},
	})
	translate.device.set(domain.ClipContent{Kind: domain.ClipKindText, Text: "コンパイラは機械語ソフト", Display: "コンパイラは機械語ソフト"})
	start(t, state)
	awaitStatus(t, state, translateReady)

	// The source is the sentence itself, and the readings are not drawn
	// until they are asked for — but the row that asks for them is.
	if got := state.translation.source; got != "コンパイラは機械語ソフト" {
		t.Errorf("the source is %q", got)
	}
	closed := cardTexts(t, state)
	for _, want := range []string{"コンパイラは機械語ソフト", "编译器是软件", "注音"} {
		if !contains(closed, want) {
			t.Errorf("the card does not show %q: %q", want, closed)
		}
	}
	for _, unwanted := range []string{"compiler", "キカイゴ", "software"} {
		if contains(closed, unwanted) {
			t.Errorf("the card draws %q before the row was opened: %q", unwanted, closed)
		}
	}

	// Opening it draws the annotated words, and makes the card taller: the
	// window is sized to what the card measures.
	before := card.ContentBounds().Height
	tt := ui.NewTester(state.translateView, translateWidth, translateMaxHeight)
	if err := tt.Click("注音"); err != nil {
		t.Fatalf("the readings row could not be clicked: %v", err)
	}
	if !state.translation.readings {
		t.Fatal("clicking the row did not open the readings")
	}
	if got := card.ContentBounds().Height; got <= before {
		t.Errorf("the card did not grow: %d from %d", got, before)
	}
	open := cardTexts(t, state)
	for _, want := range []string{"compiler", "software", "キカイゴ", "コンパイラ", "機械語"} {
		if !contains(open, want) {
			t.Errorf("the open card does not show %q: %q", want, open)
		}
	}

	// With the gloss turned off it is the reading that is drawn instead.
	if err := state.translate.SetLoanwordGloss(backgroundContext(), false); err != nil {
		t.Fatalf("SetLoanwordGloss failed: %v", err)
	}
	found := cardTexts(t, state)
	if contains(found, "compiler") {
		t.Errorf("the card still draws the loanword: %q", found)
	}
	if !contains(found, "キカイゴ") {
		t.Errorf("the card does not draw the reading: %q", found)
	}

	// And with both off there is nothing to open, so the row is gone.
	if err := state.translate.SetFurigana(backgroundContext(), false); err != nil {
		t.Fatalf("SetFurigana failed: %v", err)
	}
	if found := cardTexts(t, state); contains(found, "注音") {
		t.Errorf("the card still offers an annotation that is switched off: %q", found)
	}
}

// A tool that is off, a tool with nothing configured and a clipboard with
// nothing to translate are three different things to say, and none of them is
// a request to a service.
func TestTranslationCardSaysWhatIsWrong(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, state *State, translate *testTranslateFixture)
		want    translateStatus
		says    string
	}{
		{
			name: "the tool is off",
			prepare: func(t *testing.T, state *State, _ *testTranslateFixture) {
				if err := state.translate.SetEnabled(backgroundContext(), false); err != nil {
					t.Fatalf("SetEnabled failed: %v", err)
				}
			},
			want: translateUnconfigured,
			says: "划词翻译已关闭",
		},
		{
			name: "nothing is configured",
			prepare: func(t *testing.T, state *State, _ *testTranslateFixture) {
				if err := state.translate.SaveSettings(backgroundContext(), func(s *domain.TranslationSettings) {
					s.Provider = "nobody"
				}); err != nil {
					t.Fatalf("SaveSettings failed: %v", err)
				}
			},
			want: translateUnconfigured,
			says: "还没有配置",
		},
		{
			name: "the key is missing",
			prepare: func(t *testing.T, state *State, translate *testTranslateFixture) {
				if err := state.secretService.Delete(backgroundContext(), "deepseek", "apiKey"); err != nil {
					t.Fatalf("Delete failed: %v", err)
				}
				translate.device.set(domain.ClipContent{
					Kind: domain.ClipKindText, Text: "hello", Display: "hello",
				})
			},
			want: translateUnconfigured,
			says: "凭据",
		},
		{
			name:    "the clipboard is empty",
			prepare: func(t *testing.T, state *State, _ *testTranslateFixture) {},
			want:    translateNoText,
			says:    "复制",
		},
		{
			name: "the service failed",
			prepare: func(t *testing.T, state *State, translate *testTranslateFixture) {
				translate.device.set(domain.ClipContent{
					Kind: domain.ClipKindText, Text: "hello", Display: "hello",
				})
				translate.deepseek.fail(domain.ErrProviderTimeout)
			},
			want: translateFailed,
			says: "超时",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, translate, card := translateState(t)
			tc.prepare(t, state, translate)
			start(t, state)
			awaitStatus(t, state, tc.want)

			if !card.visible() {
				t.Error("the card was not shown")
			}
			texts := cardTexts(t, state)
			if !contains(texts, tc.says) {
				t.Errorf("the card does not say %q: %q", tc.says, texts)
			}
		})
	}
}

// A failure is drawn as a failure with the reason, never as an empty
// translation.
func TestTranslationCardShowsTheReason(t *testing.T) {
	state, translate, _ := translateState(t)
	translate.device.set(domain.ClipContent{Kind: domain.ClipKindText, Text: "hello", Display: "hello"})
	translate.deepseek.fail(domain.ErrProviderUnauthorized)
	start(t, state)
	awaitStatus(t, state, translateFailed)

	// A provider error is translated into the interface's own words.
	if !strings.Contains(state.translation.failure, "凭据") {
		t.Errorf("the card's reason is %q", state.translation.failure)
	}
	if state.translation.result.TranslatedText != "" {
		t.Error("a failure produced a translation")
	}
}

// The card is as tall as what is in it: the frame draws the card, and what it
// drew is what the window is then sized to, inside the card's own bounds.
func TestTranslationCardSizesItselfToItsContent(t *testing.T) {
	state, _, card := translateState(t)
	state.showTranslation(mygo.Point{X: 10, Y: 10}, translationState{status: translateReady, result: usecaseResultFor("短")})

	// A frame of the card, drawn the way its window would draw it. The frame
	// is as tall as the card was before it drew, which is what a window that
	// has not resized yet looks like.
	ui.Render(state.translateView, translateWidth, translateHeight, 2)
	if state.cardHeight <= 0 {
		t.Fatal("the card did not measure what it drew")
	}
	if got := card.ContentBounds().Height; got != clampInt(state.cardHeight, translateMinHeight, translateMaxHeight) {
		t.Errorf("the window is %d tall, want what the card measured (%d)", got, state.cardHeight)
	}
	if got := card.ContentBounds().Height; got < translateMinHeight || got > translateMaxHeight {
		t.Errorf("the window is %d tall, outside the card's bounds", got)
	}
	if got, want := card.ContentBounds().Width, translateWidth; got != want {
		t.Errorf("the card is %d wide, want the design's %d", got, want)
	}

	// A longer answer measures taller than a short one.
	short := state.cardHeight
	frame := card.ContentBounds().Height
	state.translation.result.TranslatedText = strings.Repeat("编译器把源代码转换成机器语言。", 12)
	ui.Render(state.translateView, translateWidth, frame, 2)
	if state.cardHeight <= short {
		t.Errorf("the card measured %d with a long answer, and %d with a short one", state.cardHeight, short)
	}
	// But never taller than the tallest: past that the body scrolls.
	if got := card.ContentBounds().Height; got > translateMaxHeight {
		t.Errorf("the card grew to %d, past its own limit of %d", got, translateMaxHeight)
	}
	if got := state.cardHeight; got > translateMaxHeight {
		t.Errorf("the card measured %d, past its own limit", got)
	}
}

// The card is measured even when it is taller than the window it is being
// drawn into.
//
// That is the ordinary case, not an edge one: the window is always one frame
// behind the content, so the first frame of a long answer is drawn in a short
// window, and what the card measures there is what the window is then resized
// to. A card whose height is capped by the window it is in can therefore never
// grow past the height it started at.
//
// What this asserts is the wiring — the frame measures, and the window is set
// to what it measured. The case it is written about is invisible here: the
// offscreen harness does not constrain a view to the height it is rendered
// at, so a card the window would squeeze still reports its content height.
// What keeps the card un-squeezable is in translate_view.go — it is not
// clipped — and that was checked in a real window, by
// docs/design.md §8's rule.
func TestTranslationCardMeasuresPastItsWindow(t *testing.T) {
	state, _, card := translateState(t)
	long := strings.Repeat("编译器把源代码转换成机器语言的软件。", 12)
	state.showTranslation(mygo.Point{X: 10, Y: 10}, translationState{
		status: translateReady, result: usecaseResultFor(long),
	})
	if got := card.ContentBounds().Height; got != translateHeight {
		t.Fatalf("the card's window starts at %d, want %d", got, translateHeight)
	}

	ui.Render(state.translateView, translateWidth, translateHeight, 2)
	if state.cardHeight <= translateHeight {
		t.Errorf("the card measured %d in a %d-tall window: the window capped what it measured",
			state.cardHeight, translateHeight)
	}
	if got := card.ContentBounds().Height; got != clampInt(state.cardHeight, translateMinHeight, translateMaxHeight) {
		t.Errorf("the window is %d tall, want what the card measured (%d)", got, state.cardHeight)
	}
}

// The card is one rounded sheet: what is inside it is cut to the card's own
// shape, so the foot — a square row at the bottom — does not paint over the
// two lower corners and leave the card round on top and square underneath.
//
// The property is asked at the corners rather than per pixel: every corner of
// the card is the face the card sits on, so the outline it draws is round at
// all four of them.
func TestTheCardIsOneRoundedSheet(t *testing.T) {
	state, _, _ := translateState(t)
	state.translation = translationState{status: translateReady, result: usecaseResultFor("短")}
	const scale = 2
	img := ui.Render(state.translateView, translateWidth, translateHeight, scale)
	bottom, ok := cardBottomRow(img)
	if !ok {
		t.Fatal("the card drew no bottom edge")
	}
	outside := img.RGBAAt(0, 0)
	for _, corner := range [][2]int{
		{0, 0}, {img.Rect.Dx() - 1, 0},
		{0, bottom}, {img.Rect.Dx() - 1, bottom},
	} {
		if got := img.RGBAAt(corner[0], corner[1]); got != outside {
			t.Errorf("the card's corner at %d,%d is %v, and the face it sits on is %v: "+
				"something inside the card paints outside its outline", corner[0], corner[1], got, outside)
		}
	}
}

// cardBottomRow is the last row of a frame the card's own outline crosses: its
// bottom edge, which is the only line of the card that reaches the middle of
// the frame.
func cardBottomRow(img *image.RGBA) (int, bool) {
	middle := img.Rect.Dx() / 2
	bottom, found := 0, false
	for y := 0; y < img.Rect.Dy(); y++ {
		if img.RGBAAt(middle, y) == rgba(colorBorder) {
			bottom, found = y, true
		}
	}
	return bottom, found
}

// The card asks for the frames that measure what it drew.
//
// MyGo's Element.Bounds is the previous frame's rectangle, so every frame
// measures the card as it *was*. A card whose content has just changed
// therefore has to ask for the frame after it: without one it measures the
// height the window already has, nothing resizes, nothing asks again, and the
// answer sits in a card too small for it until something else — a click —
// asks for a frame. The frames are owed when the card is shown and when the
// answer arrives, which are the two moments its content changes.
func TestTheCardOwesItselfTheFramesThatMeasureIt(t *testing.T) {
	state, translate, _ := translateState(t)
	translate.device.set(domain.ClipContent{
		Kind: domain.ClipKindText, Text: "A compiler converts source code.", Display: "A compiler converts source code.",
	})

	start(t, state)
	if state.cardSettle == 0 {
		t.Error("showing the card did not ask for the frames that measure it")
	}
	awaitStatus(t, state, translateReady)
	if state.cardSettle == 0 {
		t.Error("the answer arriving did not ask for the frames that measure it")
	}
}

// fitCard reports whether it actually resized the window: that is what tells
// the frame it ran in that the card has to be laid out again.
func TestFittingTheCardReportsAResize(t *testing.T) {
	state, _, card := translateState(t)
	state.showTranslation(mygo.Point{X: 10, Y: 10}, translationState{status: translateReady})
	work := mygo.Rectangle{Width: 1920, Height: 1080}

	if state.fitCard(work, 1, translateHeight) {
		t.Error("fitting the card to the height it already has was reported as a resize")
	}
	if !state.fitCard(work, 1, translateHeight+80) {
		t.Error("growing the card was not reported as a resize")
	}
	if got := card.ContentBounds().Height; got != translateHeight+80 {
		t.Errorf("the card is %d tall, want %d", got, translateHeight+80)
	}
}

// A translation longer than the card is not a reason for the foot to leave the
// window.
//
// The card stops growing at its own cap, the body scrolls under the hairline,
// and the foot keeps the card's last row — it is where the card is pinned,
// dragged and closed, so it cannot be what a long answer pushes off the
// bottom.
func TestALongTranslationScrollsUnderTheFoot(t *testing.T) {
	state, _, card := translateState(t)
	long := strings.Repeat("编译器是把源代码转换成机器语言的软件。", 40)
	state.showTranslation(mygo.Point{X: 10, Y: 10}, translationState{
		status: translateReady,
		source: strings.Repeat("コンパイラはソースコードを機械語に変換するソフトです。", 4),
		result: usecase.Result{
			SourceText:     long,
			TranslatedText: long,
			SourceLang:     domain.LangJapanese,
			TargetLang:     domain.LangChinese,
			ProviderID:     "deepseek",
			ProviderName:   "DeepSeek",
		},
	})

	// The card measures its own cap, not the answer's height.
	ui.Render(state.translateView, translateWidth, translateHeight, 2)
	if state.cardHeight != translateMaxHeight {
		t.Errorf("the card measured %d, want its own cap %d", state.cardHeight, translateMaxHeight)
	}
	if got := card.ContentBounds().Height; got != translateMaxHeight {
		t.Errorf("the window is %d tall, want the card's %d", got, translateMaxHeight)
	}

	// And the foot is drawn, on the card's last row.
	tt := ui.NewTester(state.translateView, translateWidth, translateMaxHeight)
	foot, ok := tt.Find("AI 翻译 · DeepSeek")
	if !ok {
		t.Fatal("the foot is not drawn: a long answer pushed it out of the card")
	}
	if bottom := foot.Y + foot.H; bottom > float32(translateMaxHeight) {
		t.Errorf("the foot ends at %.0f, past the card's %d", bottom, translateMaxHeight)
	}
	if foot.Y < float32(translateMaxHeight)-60 {
		t.Errorf("the foot is at y=%.0f, not on the card's last row", foot.Y)
	}
}

// usecaseResultFor is one line of translation, for the size tests.
func usecaseResultFor(text string) usecase.Result {
	return usecase.Result{
		SourceText:     "hello",
		TranslatedText: text,
		SourceLang:     domain.LangEnglish,
		TargetLang:     domain.LangChinese,
		ProviderID:     "deepseek",
		ProviderName:   "DeepSeek",
	}
}

// usize is a width as an int, for a comparison in a test.
func usize(v int) int { return v }

// The foot of the card is the handle that moves it: a press starts the drag,
// the pointer's travel moves the window, and a pinned card stays where it was
// put.
//
// The travel is what moves it, not the pointer's position: the press
// remembers where the pointer was, and a move puts the window that far from
// where it started. A card that added the pointer's own position would jump
// to the pointer and then chase it.
func TestDraggingTheCardByItsFoot(t *testing.T) {
	state, _, card := translateState(t)
	state.cursor = mygo.Point{X: 200, Y: 200}
	state.showTranslation(mygo.Point{X: 200, Y: 200}, translationState{status: translateReady})
	start := card.ContentBounds()

	// The press is taken at the pointer's own place, which is 200,200; the
	// window is not the pointer, and the card must not jump to it.
	if !state.dragCard(ui.InputEvent{Kind: ui.InputPointerDown}) {
		t.Fatal("the press on the foot was not taken as a drag")
	}
	if !state.translation.dragging {
		t.Error("the drag did not start")
	}
	if state.translateCloseTimer != nil {
		t.Error("a drag left a close timer running")
	}

	// A press is not a move: the card is where it was.
	state.cursor = mygo.Point{X: 260, Y: 240}
	if !state.dragCard(ui.InputEvent{Kind: ui.InputPointerMove, Button: 0}) {
		t.Error("the move was not taken")
	}
	moved := card.ContentBounds()
	if moved.X != start.X+60 || moved.Y != start.Y+40 {
		t.Errorf("the card moved to %d,%d, want %d,%d (the pointer's travel)",
			moved.X, moved.Y, start.X+60, start.Y+40)
	}
	// The size is the card's own throughout: a drag moves it, it does not
	// resize it.
	if moved.Width != start.Width || moved.Height != start.Height {
		t.Errorf("the drag resized the card: %+v from %+v", moved, start)
	}
	// Every move is measured from the press, not from the move before it:
	// the pointer is now 75 across and 10 down, so that is where the card
	// goes — not 60+75 from where it started.
	state.cursor = mygo.Point{X: 275, Y: 210}
	state.dragCard(ui.InputEvent{Kind: ui.InputPointerMove, Button: 0})
	if got := card.ContentBounds(); got.X != start.X+75 || got.Y != start.Y+10 {
		t.Errorf("the second move put the card at %d,%d, want %d,%d", got.X, got.Y, start.X+75, start.Y+10)
	}
	if !state.dragCard(ui.InputEvent{Kind: ui.InputPointerUp}) {
		t.Error("the release was not taken")
	}
	if state.translation.dragging {
		t.Error("the drag did not end")
	}
	// A move after the release is not a drag.
	state.cursor = mygo.Point{X: 400, Y: 400}
	if state.dragCard(ui.InputEvent{Kind: ui.InputPointerMove, Button: 0}) {
		t.Error("a move after the release was taken as a drag")
	}
	if got := card.ContentBounds(); got.X != start.X+75 || got.Y != start.Y+10 {
		t.Errorf("the card moved after the release: %+v", got)
	}
}

// A pointer crossing the foot with no button held is not a drag. The foot
// takes the moves that pass over it as well as the ones that belong to a
// press, and a card on its way out after losing the keyboard must not follow
// the pointer across it.
func TestAPointerCrossingTheFootDoesNotMoveTheCard(t *testing.T) {
	state, _, card := translateState(t)
	state.showTranslation(mygo.Point{X: 200, Y: 200}, translationState{status: translateReady})
	before := card.ContentBounds()

	// The framework reports a move that belongs to no press with -1.
	if state.dragCard(ui.InputEvent{Kind: ui.InputPointerMove, Button: -1, X: 40, Y: 30}) {
		t.Error("a move with no button held was taken as a drag")
	}
	if got := card.ContentBounds(); got != before {
		t.Errorf("the card moved although nothing was pressed: %+v from %+v", got, before)
	}

	// A press, then the keyboard goes elsewhere before the release: what the
	// user was dragging with it is forgotten, so the pointer cannot carry on
	// moving the card.
	state.dragCard(ui.InputEvent{Kind: ui.InputPointerDown})
	state.cursor = mygo.Point{X: 260, Y: 240}
	state.dragCard(ui.InputEvent{Kind: ui.InputPointerMove, Button: 0})
	moved := card.ContentBounds()
	card.blur()
	state.endCardDrag()
	state.cursor = mygo.Point{X: 400, Y: 400}
	if state.dragCard(ui.InputEvent{Kind: ui.InputPointerMove, Button: 0}) {
		t.Error("a move after the keyboard was lost was taken as a drag")
	}
	if got := card.ContentBounds(); got != moved {
		t.Errorf("the card moved after it lost the keyboard: %+v from %+v", got, moved)
	}
	state.closeTranslation()
}

// A pinned card is dragged like any other: the pin is about the card staying
// open when the keyboard goes elsewhere, not about it staying where it was
// put. A card the user has pinned is one they are working with, and moving it
// out of the way is part of that.
func TestAPinnedCardStillDrags(t *testing.T) {
	state, _, card := translateState(t)
	state.cursor = mygo.Point{X: 200, Y: 200}
	state.showTranslation(mygo.Point{X: 200, Y: 200}, translationState{status: translateReady, pinned: true})
	state.translation.pinned = true
	start := card.ContentBounds()

	if !state.dragCard(ui.InputEvent{Kind: ui.InputPointerDown}) {
		t.Fatal("a pinned card did not take the press as a drag")
	}
	if !state.translation.dragging {
		t.Error("a pinned card did not start dragging")
	}
	state.cursor = mygo.Point{X: 260, Y: 240}
	state.dragCard(ui.InputEvent{Kind: ui.InputPointerMove, Button: 0})
	if got := card.ContentBounds(); got.X != start.X+60 || got.Y != start.Y+40 {
		t.Errorf("the pinned card moved to %d,%d, want %d,%d", got.X, got.Y, start.X+60, start.Y+40)
	}
	state.dragCard(ui.InputEvent{Kind: ui.InputPointerUp})
	if state.translation.dragging {
		t.Error("the drag did not end")
	}
	// And it is still pinned: moving it is not un-pinning it.
	if !state.translation.pinned {
		t.Error("dragging unpinned the card")
	}
}

// The card is kept on the screen it is on: a drag that would take it off the
// edge stops at the edge.
func TestDraggingKeepsTheCardOnScreen(t *testing.T) {
	state, _, card := translateState(t)
	state.cursor = mygo.Point{X: 200, Y: 200}
	state.showTranslation(mygo.Point{X: 200, Y: 200}, translationState{status: translateReady})
	start := card.ContentBounds()
	state.dragCard(ui.InputEvent{Kind: ui.InputPointerDown})
	state.cursor = mygo.Point{X: -1000, Y: -1000}
	state.dragCard(ui.InputEvent{Kind: ui.InputPointerMove, Button: 0})
	if got := card.ContentBounds(); got.X < 0 || got.Y < 0 {
		t.Errorf("the card was dragged off the screen: %+v", got)
	}
	// The grab point is kept even at the edge: coming back from beyond it
	// moves the card as soon as the pointer is back inside the screen,
	// rather than after it has retraced the distance it was held at the
	// edge for.
	state.cursor = mygo.Point{X: 199, Y: 199}
	state.dragCard(ui.InputEvent{Kind: ui.InputPointerMove, Button: 0})
	if got := card.ContentBounds(); got.X != start.X-1 || got.Y != start.Y-1 {
		t.Errorf("coming back from the edge put the card at %+v, want %d,%d", got, start.X-1, start.Y-1)
	}
	state.cursor = mygo.Point{X: 5000, Y: 5000}
	state.dragCard(ui.InputEvent{Kind: ui.InputPointerMove, Button: 0})
	if got := card.ContentBounds(); got.X+got.Width > 1920 || got.Y+got.Height > 1080 {
		t.Errorf("the card was dragged off the other edge: %+v", got)
	}
	state.dragCard(ui.InputEvent{Kind: ui.InputPointerUp})
}

// The card closes itself once it has been ignored for as long as the setting
// says, and the timer is cancelled by anything that means the user came back.
func TestTranslationCardClosesAfterBeingIgnored(t *testing.T) {
	state, _, card := translateState(t)
	if err := state.translate.SetCloseDelay(backgroundContext(), 30*time.Millisecond); err != nil {
		t.Fatalf("SetCloseDelay failed: %v", err)
	}
	state.showTranslation(mygo.Point{X: 10, Y: 10}, translationState{status: translateReady})

	card.blur()
	if !card.visible() {
		t.Fatal("the card closed the moment it lost the keyboard")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && card.visible() {
		time.Sleep(2 * time.Millisecond)
	}
	if card.visible() {
		t.Fatal("the card stayed open although it was ignored")
	}
	if card.hidden() == 0 {
		t.Error("the card was never taken off screen")
	}
}

// A pinned card never closes itself: that is what the pin is for.
func TestPinnedCardStays(t *testing.T) {
	state, _, card := translateState(t)
	if err := state.translate.SetCloseDelay(backgroundContext(), 10*time.Millisecond); err != nil {
		t.Fatalf("SetCloseDelay failed: %v", err)
	}
	state.showTranslation(mygo.Point{X: 10, Y: 10}, translationState{status: translateReady, pinned: true})
	state.translation.pinned = true

	card.blur()
	time.Sleep(80 * time.Millisecond)
	if !card.visible() {
		t.Error("a pinned card closed itself")
	}
	if state.translateCloseTimer != nil {
		t.Error("a pinned card started a close timer")
	}
}

// Coming back to the card cancels the close it asked for.
func TestComingBackToTheCardCancelsTheClose(t *testing.T) {
	state, _, card := translateState(t)
	if err := state.translate.SetCloseDelay(backgroundContext(), 2*time.Second); err != nil {
		t.Fatalf("SetCloseDelay failed: %v", err)
	}
	state.showTranslation(mygo.Point{X: 10, Y: 10}, translationState{status: translateReady})
	card.blur()
	if state.translateCloseTimer == nil {
		t.Fatal("losing the keyboard did not start a close")
	}
	card.focus()
	if state.translateCloseTimer != nil {
		t.Error("taking the keyboard back did not cancel the close")
	}
	state.closeTranslation()
}

// Pinning while the timer runs keeps the card: the user asked for it between
// the click elsewhere and the close.
func TestPinningDuringTheDelayKeepsTheCard(t *testing.T) {
	state, _, card := translateState(t)
	if err := state.translate.SetCloseDelay(backgroundContext(), 40*time.Millisecond); err != nil {
		t.Fatalf("SetCloseDelay failed: %v", err)
	}
	state.showTranslation(mygo.Point{X: 10, Y: 10}, translationState{status: translateReady})
	card.blur()
	state.toggleTranslationPin()
	time.Sleep(120 * time.Millisecond)
	if !card.visible() {
		t.Error("the card closed although it was pinned while the timer ran")
	}
	if !state.translation.pinned {
		t.Error("the pin did not take")
	}
	state.closeTranslation()
}

// Closing the card hides it rather than destroying it: the next press reuses
// it.
func TestClosingTheCardHidesIt(t *testing.T) {
	state, _, card := translateState(t)
	state.showTranslation(mygo.Point{X: 10, Y: 10}, translationState{status: translateReady})
	state.closeTranslation()

	if card.visible() {
		t.Error("the card is still on screen")
	}
	if state.translateWindow == nil {
		t.Error("the card was thrown away, and the next press would build another")
	}
	if state.translateCloseTimer != nil {
		t.Error("a close timer survived the close")
	}
}

// The pin action says what state it is in, because a mark that only says
// "pin" leaves the state unsaid.
func TestPinLabelSaysTheState(t *testing.T) {
	if got := pinLabel(true); got != "取消固定" {
		t.Errorf("pinned label = %q", got)
	}
	if got := pinLabel(false); got != "固定浮窗" {
		t.Errorf("unpinned label = %q", got)
	}
}

// The foot is the handle the card is dragged by, but the actions on it are
// still their own.
//
// The row takes every pointer event that lands on it, and the framework gives
// a press to the innermost element that handles its input as it comes — which
// is the row, since a button only reports Clicked. Without the actions saying
// anything, a click on the pin or the close mark is swallowed as the start of
// a drag: the button never presses, nothing happens, and the pin never turns
// green.
func TestTheFootsActionsTakeTheirOwnClicks(t *testing.T) {
	state, _, card := translateState(t)
	state.showTranslation(mygo.Point{X: 10, Y: 10}, translationState{status: translateReady, result: usecaseResultFor("短")})
	tt := ui.NewTester(state.translateView, translateWidth, translateHeight)

	if err := tt.Click("固定浮窗"); err != nil {
		t.Fatalf("the pin could not be clicked: %v", err)
	}
	if !state.translation.pinned {
		t.Error("clicking the pin did not pin the card")
	}
	if !card.visible() {
		t.Error("clicking the pin closed the card")
	}

	// The action is named by what it does next, so the same mark is the way
	// back out.
	if err := tt.Click("取消固定"); err != nil {
		t.Fatalf("the pin could not be clicked again: %v", err)
	}
	if state.translation.pinned {
		t.Error("clicking the pin again did not unpin the card")
	}

	if err := tt.Click("关闭"); err != nil {
		t.Fatalf("the close mark could not be clicked: %v", err)
	}
	if card.visible() {
		t.Error("clicking the close mark did not close the card")
	}
}

// The row under those actions is still the handle: a press that lands on the
// foot's own face — the line it shows, not one of its marks — drags the card.
//
// It goes through the framework's input here rather than calling dragCard, so
// that what the frame builds is what is being asked: the actions shield
// themselves, and everything else on the row belongs to the drag.
func TestTheFootStillDragsTheCard(t *testing.T) {
	state, _, card := translateState(t)
	state.cursor = mygo.Point{X: 200, Y: 200}
	state.showTranslation(mygo.Point{X: 200, Y: 200}, translationState{status: translateReady, result: usecaseResultFor("短")})
	start := card.ContentBounds()
	tt := ui.NewTester(state.translateView, translateWidth, translateMaxHeight)
	label, ok := tt.Find("AI 翻译 · DeepSeek")
	if !ok {
		t.Fatal("the foot's line is not drawn")
	}
	center := mygo.Point{X: int(label.X + label.W/2), Y: int(label.Y + label.H/2)}

	// Where the pointer is for the drag is the screen, which the test owns;
	// where it presses is the card, which the tester owns.
	press := state.cursor
	tt.Press(float32(center.X), float32(center.Y))
	if !state.translation.dragging {
		t.Fatal("a press on the foot did not start a drag")
	}
	state.cursor = mygo.Point{X: press.X + 60, Y: press.Y + 40}
	tt.Move(float32(center.X+60), float32(center.Y+40))
	if got := card.ContentBounds(); got.X != start.X+60 || got.Y != start.Y+40 {
		t.Errorf("the card moved to %d,%d, want %d,%d", got.X, got.Y, start.X+60, start.Y+40)
	}
	tt.Release(float32(center.X+60), float32(center.Y+40))
	if state.translation.dragging {
		t.Error("the drag did not end")
	}
}

// The readings are for Japanese: the design draws a segmented Japanese answer
// with readings, and every other language as the paragraph it is. An English
// answer handed the same pieces would be a row of pale words with an empty
// line above each — and a row that opens nothing is a control nobody wants.
func TestTheReadingsAreForJapaneseOnly(t *testing.T) {
	segments := []domain.Segment{
		{Text: "機械語", Reading: "キカイゴ", Target: "机器语言"},
		{Text: "は", Target: "是"},
	}
	// The source language the card reads is the one the use case detected in
	// the text, so each case carries its own text: the same segments are
	// handed over in all three, and only the Japanese one is drawn with them.
	for _, tc := range []struct {
		name   string
		source domain.Language
		text   string
		want   bool
	}{
		{"japanese", domain.LangJapanese, "機械語は", true},
		{"english", domain.LangEnglish, "A compiler is", false},
		{"korean", domain.LangKorean, "컴파일러는", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, translate, _ := translateState(t)
			translate.deepseek.answer(domain.TranslationResult{
				TranslatedText: "机器语言是",
				SourceLang:     tc.source,
				TargetLang:     domain.LangChinese,
				Segments:       segments,
			})
			translate.device.set(domain.ClipContent{Kind: domain.ClipKindText, Text: tc.text, Display: tc.text})
			start(t, state)
			awaitStatus(t, state, translateReady)

			if got := contains(cardTexts(t, state), "注音"); got != tc.want {
				t.Errorf("the card offers the readings = %v, want %v", got, tc.want)
			}
			if !tc.want {
				return
			}
			tt := ui.NewTester(state.translateView, translateWidth, translateMaxHeight)
			if err := tt.Click("注音"); err != nil {
				t.Fatalf("the readings row could not be clicked: %v", err)
			}
			if !contains(cardTexts(t, state), "キカイゴ") {
				t.Error("the open card does not draw the reading")
			}
			if got := state.translation.source; got != tc.text {
				t.Errorf("the source is %q, want %q", got, tc.text)
			}
		})
	}
}

// The annotation is a request of its own, made when the user opens it: the
// translation does not wait for the pieces, and nothing is asked for until
// someone wants them.
func TestTheAnnotationIsAskedForWhenItIsOpened(t *testing.T) {
	state, translate, _ := translateState(t)
	translate.deepseek.answer(domain.TranslationResult{TranslatedText: "编译器是软件"})
	translate.deepseek.annotate(domain.TranslationResult{Segments: []domain.Segment{
		{Text: "コンパイラ", Gloss: "compiler", Target: "编译器"},
		{Text: "は", Target: "是"},
		{Text: "機械語", Reading: "キカイゴ", Target: "机器语言"},
	}})
	translate.device.set(domain.ClipContent{
		Kind: domain.ClipKindText, Text: "コンパイラは機械語", Display: "コンパイラは機械語",
	})
	start(t, state)
	awaitStatus(t, state, translateReady)

	// One request, and it asked for a paragraph: no annotation to wait for.
	if got := translate.deepseek.requestCount(); got != 1 {
		t.Fatalf("the translator was asked %d times before the annotation was opened", got)
	}
	if request := translate.deepseek.lastRequest(); request.Annotation.Wants() {
		t.Errorf("the translation asked for the annotation: %+v", request.Annotation)
	}
	if state.translation.annotation.status != annotationIdle {
		t.Errorf("the annotation was asked for before it was opened: %+v", state.translation.annotation)
	}

	// Opening it asks for the words of the source — not the clipboard, and
	// not the paragraph — and the answer is kept.
	tt := ui.NewTester(state.translateView, translateWidth, translateMaxHeight)
	if err := tt.Click("注音"); err != nil {
		t.Fatalf("the annotation row could not be clicked: %v", err)
	}
	if got := translate.deepseek.requestCount(); got != 2 {
		t.Fatalf("opening the annotation asked the translator %d times, want 2", got)
	}
	request := translate.deepseek.lastRequest()
	if !request.Annotation.Wants() {
		t.Errorf("the second request does not ask for the annotation: %+v", request.Annotation)
	}
	if request.Text != "コンパイラは機械語" {
		t.Errorf("the second request is for %q, want the source text", request.Text)
	}
	await(t, func() bool { return state.translation.annotation.status == annotationReady },
		"the annotation answer never arrived: %+v", state.translation.annotation)
	if got := len(state.translation.annotation.segments); got != 3 {
		t.Errorf("the annotation carries %d pieces, want 3", got)
	}

	// Closing and opening it again is not another request: the answer is
	// kept until the card is about something else. The row is named by what
	// is behind it, so it says how many words there are now.
	if err := tt.Click("注音 · 2 个词"); err != nil {
		t.Fatalf("the annotation row does not say what is behind it: %v", err)
	}
	if err := tt.Click("注音 · 2 个词"); err != nil {
		t.Fatalf("the annotation row could not be opened again: %v", err)
	}
	if got := translate.deepseek.requestCount(); got != 2 {
		t.Errorf("reopening the annotation asked the translator again: %d requests", got)
	}
}

// The source is the text that was copied, line breaks and all. The pieces the
// translator answers with are the annotation under the translation, and they
// do not replace it: joining them would put the user's lines on one line.
func TestTheCardKeepsTheCopiedLines(t *testing.T) {
	state, translate, _ := translateState(t)
	text := "一行目\n二行目"
	translate.device.set(domain.ClipContent{Kind: domain.ClipKindText, Text: text, Display: text})
	translate.deepseek.answer(domain.TranslationResult{TranslatedText: "第一行\n第二行"})
	start(t, state)
	awaitStatus(t, state, translateReady)

	if got := state.translation.source; got != text {
		t.Errorf("the source is %q, want %q", got, text)
	}
	found := cardTexts(t, state)
	for _, want := range []string{text, "第一行\n第二行"} {
		if !contains(found, want) {
			t.Errorf("the card does not show %q: %q", want, found)
		}
	}
	// And what was sent is the same text: the format is what is translated.
	if got := translate.deepseek.lastRequest().Text; got != text {
		t.Errorf("the text sent is %q, want %q", got, text)
	}
}

// The open annotation shows the words, not the sentence: a piece of kana has
// nothing to show, and the sentence is already above it.
func TestTheOpenAnnotationShowsTheWords(t *testing.T) {
	state, translate, _ := translateState(t)
	translate.deepseek.answer(domain.TranslationResult{TranslatedText: "编译器是软件"})
	translate.deepseek.annotate(domain.TranslationResult{Segments: []domain.Segment{
		{Text: "コンパイラ", Gloss: "compiler", Target: "编译器"},
		{Text: "は", Target: "是"},
		{Text: "機械語", Reading: "キカイゴ", Target: "机器语言"},
		{Text: "する", Target: ""},
		{Text: "。", Target: ""},
	}})
	translate.device.set(domain.ClipContent{
		Kind: domain.ClipKindText, Text: "コンパイラは機械語する。", Display: "コンパイラは機械語する。",
	})
	start(t, state)
	awaitStatus(t, state, translateReady)

	tt := ui.NewTester(state.translateView, translateWidth, translateMaxHeight)
	if err := tt.Click("注音"); err != nil {
		t.Fatalf("the annotation row could not be clicked: %v", err)
	}
	await(t, func() bool { return state.translation.annotation.status == annotationReady },
		"the annotation answer never arrived")

	open := cardTexts(t, state)
	for _, want := range []string{"コンパイラ", "compiler", "機械語", "キカイゴ"} {
		if !contains(open, want) {
			t.Errorf("the open annotation does not show %q: %q", want, open)
		}
	}
	// The pieces with nothing to say are not drawn — as words of their own.
	for _, unwanted := range []string{"は", "する", "。"} {
		for _, text := range open {
			if text == unwanted {
				t.Errorf("the open annotation shows the piece %q: %q", unwanted, open)
			}
		}
	}
}

// A request for the annotation that fails says so where the words would have
// been, rather than leaving the row open on nothing.
func TestTheAnnotationSaysWhenItFails(t *testing.T) {
	state, translate, _ := translateState(t)
	translate.deepseek.answer(domain.TranslationResult{TranslatedText: "编译器是软件"})
	translate.device.set(domain.ClipContent{
		Kind: domain.ClipKindText, Text: "コンパイラ", Display: "コンパイラ",
	})
	start(t, state)
	awaitStatus(t, state, translateReady)

	translate.deepseek.fail(domain.ErrProviderBadResponse)
	tt := ui.NewTester(state.translateView, translateWidth, translateMaxHeight)
	if err := tt.Click("注音"); err != nil {
		t.Fatalf("the annotation row could not be clicked: %v", err)
	}
	await(t, func() bool { return state.translation.annotation.status == annotationFailed },
		"the failed annotation was not reported: %+v", state.translation.annotation)
	if state.translation.annotation.failure == "" {
		t.Error("the annotation failed without saying why")
	}
	if !contains(cardTexts(t, state), state.translation.annotation.failure) {
		t.Errorf("the card does not say why the annotation failed: %q", cardTexts(t, state))
	}
}

// A word the source uses twice is one chip, not two: the annotation is a list
// of the words the sentence is made of, and the same word twice in it says
// nothing new. A word that says two different things is two chips, because
// those are two words that happen to be written alike.
func TestTheAnnotationMergesTheSameWord(t *testing.T) {
	state, translate, _ := translateState(t)
	translate.deepseek.answer(domain.TranslationResult{TranslatedText: "编译器是软件"})
	translate.deepseek.annotate(domain.TranslationResult{Segments: []domain.Segment{
		{Text: "サキュバス", Gloss: "succubus", Target: "魅魔"},
		{Text: "は", Target: "是"},
		{Text: "クエスト", Gloss: "quest", Target: "任务"},
		{Text: "を", Target: ""},
		{Text: "サキュバス", Gloss: "succubus", Target: "魅魔"},
		{Text: "クエスト", Gloss: "quest", Target: "任务"},
		// The same kanji, read two ways: two words, and both are kept.
		{Text: "人", Reading: "ヒト", Target: "人"},
		{Text: "人", Reading: "ジン", Target: "人"},
	}})
	translate.device.set(domain.ClipContent{
		Kind: domain.ClipKindText, Text: "サキュバスはクエストをサキュバスクエスト人", Display: "サキュバスはクエストをサキュバスクエスト人",
	})
	start(t, state)
	awaitStatus(t, state, translateReady)

	tt := ui.NewTester(state.translateView, translateWidth, translateMaxHeight)
	if err := tt.Click("注音"); err != nil {
		t.Fatalf("the annotation row could not be clicked: %v", err)
	}
	await(t, func() bool { return state.translation.annotation.status == annotationReady },
		"the annotation answer never arrived")

	words := state.annotatedWords()
	if got := len(words); got != 4 {
		t.Errorf("the annotation has %d words, want 4: %+v", got, words)
	}
	counts := map[string]int{}
	for _, text := range cardTexts(t, state) {
		counts[text]++
	}
	for _, once := range []string{"サキュバス", "クエスト", "ヒト", "ジン"} {
		if got := counts[once]; got != 1 {
			t.Errorf("%q is drawn %d times, want once: %+v", once, got, counts)
		}
	}
	// And the row says how many words are behind it, not how many pieces the
	// translator sent.
	if !contains(cardTexts(t, state), "注音 · 4 个词") {
		t.Errorf("the row does not count the words it holds: %q", cardTexts(t, state))
	}
}

// cardTexts is what one frame of the card shows.
func cardTexts(t *testing.T, state *State) []string {
	t.Helper()
	tt := ui.NewTester(state.translateView, translateWidth, translateHeight)
	return tt.Texts()
}
