package app

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	"boku/internal/panel/deepseek"
	"boku/internal/translate"
)

// This file is the package's manual harness: what is here needs a human to
// look at the frames it writes, so every check is skipped unless its
// environment variable is set, and nothing is written during `go test ./...`.
// It is named for that role rather than for a production file, because the
// frames come from app.go, card.go, settings.go and clipboard.go at once.

// renderDirEnv is where the manual frames are written. It is read by the
// whole file: the screens of the three windows and the states of the
// translation card.
const renderDirEnv = "BOKU_RENDER_DIR"

// settingsWholePage is a window tall enough to hold the whole settings page,
// so that one frame shows every section: the page itself scrolls in a window
// the size of a screen, and a section below the fold is not in a render.
const settingsWholePage = 1800

// TestRenderScreensForInspection writes the two screens as PNGs, so a
// layout change can be looked at. It is skipped unless BOKU_RENDER_DIR is
// set: nothing is written during an ordinary test run.
func TestRenderScreensForInspection(t *testing.T) {
	dir := os.Getenv(renderDirEnv)
	if dir == "" {
		t.Skip("set BOKU_RENDER_DIR to write the screens")
	}

	writeImage := func(name string, img *image.RGBA) {
		t.Helper()
		file, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		defer file.Close()
		if err := png.Encode(file, img); err != nil {
			t.Fatalf("encode %s: %v", name, err)
		}
	}
	write := func(name string, view func(*ui.Context), width, height int) {
		t.Helper()
		writeImage(name, ui.Render(view, width, height, 2))
	}

	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	loaded(t, state)
	write("flyout.png", state.View, panelWidth, panelHeight)
	state.detailsOpen = true
	write("flyout-details.png", state.View, panelWidth, panelHeight)
	state.detailsOpen = false
	state.loadCredentials()
	write("settings.png", state.settingsView, settingsWidth, settingsHeight)

	// The quick window without a credential.
	missing := testState(t, newTestProvider(), newTestStore(), "0.1.0")
	_, _ = missing.panelService.Refresh(context.Background(), deepseek.PanelID)
	write("flyout-missing.png", missing.View, panelWidth, panelHeight)

	// The quick window after a failed refresh: the old numbers stay, and
	// are marked as old.
	failing := newTestProvider()
	stale := testState(t, failing, withCredential(newTestStore()), "0.1.0")
	loaded(t, stale)
	failing.fail(domain.ErrProviderTimeout)
	_, _ = stale.panelService.Refresh(context.Background(), deepseek.PanelID)
	write("flyout-stale.png", stale.View, panelWidth, panelHeight)

	// The quick window while a fetch runs: the refresh mark turns, and one
	// frame shows it at the angle it was drawn at. The frame is taken
	// through a tester after a moment, so it is not the angle at t = 0 that
	// every ui.Render frame shows.
	held := newTestProvider()
	fetching := testState(t, held, withCredential(newTestStore()), "0.1.0")
	loaded(t, fetching)
	gate := held.hold()
	go func() { _, _ = fetching.panelService.Refresh(context.Background(), deepseek.PanelID) }()
	for deadline := time.Now().Add(2 * time.Second); held.count() < 2 && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	turning := ui.NewTester(fetching.View, panelWidth, panelHeight)
	time.Sleep(spinPeriod / 8)
	turning.Frame()
	writeImage("flyout-refreshing.png", turning.Image())
	close(gate)

	// The settings window with a credential already stored.
	write("settings-stored.png", state.settingsView, settingsWidth, settingsHeight)

	// The quick window with a second panel under the first: the balance and
	// the quota bar, which is the screen the bar was designed for.
	write("flyout-codex.png", bothBlocksState(t).View, panelWidth, panelHeight)

	// The quick window with its clipboard line, and the history window: the
	// two faces the clipboard tool adds.
	device := &testDevice{}
	device.set(domain.ClipContent{
		Kind: domain.ClipKindText,
		Text: "https://example.com/some/rather/long/path?with=query&params=1",
	})
	withClipboard := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	withClipboard.clipboard = testClipboard(t, device, &testToolStore{
		settings: domain.ToolSettings{ClipboardRegionVisible: true, ClipboardMonitoring: true},
	})
	loaded(t, withClipboard)
	withClipboard.clipboard.Read(context.Background())
	write("flyout-clipboard.png", withClipboard.View, panelWidth, panelHeight)

	for _, text := range []string{"first copy", "second copy", `{"kind":"json"}`} {
		device.set(domain.ClipContent{Kind: domain.ClipKindText, Text: text})
		withClipboard.clipboard.Read(context.Background())
	}
	device.set(domain.ClipContent{Kind: domain.ClipKindFile, Files: []string{"/tmp/report.pdf", "/tmp/notes.txt"}})
	withClipboard.clipboard.Read(context.Background())
	write("history.png", withClipboard.historyView, historyWidth, historyHeight)
	write("settings-clipboard.png", withClipboard.settingsView, settingsWidth, settingsHeight)
	// The whole settings page in one window, so that the general section —
	// where the switches are, including the one an optional panel adds — is
	// in the frame rather than below it.
	write("settings-general.png", bothBlocksState(t).settingsView, settingsWidth, settingsWholePage)

	// A history longer than its window: the list scrolls and the footer
	// stays where it is.
	for i := 0; i < 10; i++ {
		device.set(domain.ClipContent{
			Kind: domain.ClipKindText,
			Text: fmt.Sprintf("copy %d: a value long enough to be noticed", i),
		})
		withClipboard.clipboard.Read(context.Background())
	}
	write("history-long.png", withClipboard.historyView, historyWidth, historyHeight)

	// The list answering the pointer: a row under it, and a row that was
	// just clicked. These are hard to judge from a single frame, so both
	// states are written out.
	tt := ui.NewTester(withClipboard.historyView, historyWidth, historyHeight)
	entries := withClipboard.clipboardHistory()
	if len(entries) > 1 {
		if row, ok := tt.Find(entries[1].Line()); ok {
			tt.Move(row.X+row.W-4, row.Y+row.H/2)
			file, err := os.Create(filepath.Join(dir, "history-hover.png"))
			if err != nil {
				t.Fatalf("create history-hover.png: %v", err)
			}
			if err := png.Encode(file, tt.Image()); err != nil {
				t.Fatalf("encode history-hover.png: %v", err)
			}
			file.Close()
		}
	}
	withClipboard.markCopied(entries[0])
	write("history-copied.png", withClipboard.historyView, historyWidth, historyHeight)
	withClipboard.clearCopied()

	// The translation card, in the states a user sees it in: waiting for an
	// answer, holding one, unable to, and off.
	renderTranslationCard(t, "translate-loading.png", translationState{status: translateLoading})
	annotated := translationState{
		status: translateReady,
		source: "コンパイラはソースコードを機械語に変換するソフトです。",
		result: translate.Result{
			SourceText:     "コンパイラはソースコードを機械語に変換するソフトです。",
			TranslatedText: "编译器是将源代码转换成机器语言的软件。",
			SourceLang:     domain.LangJapanese,
			TargetLang:     domain.LangChinese,
			ProviderID:     "deepseek",
			ProviderName:   "DeepSeek",
		},
		pinned: true,
	}
	// The design's own sentence, annotated the way the annotation's rules come
	// out: the loanwords carry the word they came from, the two words with
	// kanji carry a reading, and everything written in kana carries nothing —
	// the parser drops what a model puts where it does not belong, and the
	// open annotation shows only the words that carry something.
	words := []domain.Segment{
		{Text: "コンパイラ", Gloss: "compiler", Target: "编译器"},
		{Text: "は", Target: "是"},
		{Text: "ソースコード", Gloss: "source code", Target: "源代码"},
		{Text: "を", Target: ""},
		{Text: "機械語", Reading: "キカイゴ", Target: "机器语言"},
		{Text: "に", Target: ""},
		{Text: "変換", Reading: "ヘンカン", Target: "转换"},
		{Text: "する", Target: ""},
		{Text: "ソフト", Gloss: "soft", Target: "软件"},
		{Text: "です", Target: ""},
		{Text: "。", Target: ""},
	}
	// Closed, which is how the card arrives: the translation and the row that
	// opens the words behind it.
	renderTranslationCard(t, "translate-ready.png", annotated)
	// The same, with the pointer on that row: its face covers the row.
	renderTranslationCardHovered(t, "translate-ready-hover.png", annotated, "注音")
	// Opening that row asks the translator for the words, which takes a
	// moment: that is the second request, and it is what the card shows while
	// it is in flight.
	loading := annotated
	loading.readings = true
	loading.annotation = annotationState{status: annotationLoading}
	renderTranslationCard(t, "translate-annotation-loading.png", loading)
	// And the answer: the words, not the sentence.
	withReadings := annotated
	withReadings.readings = true
	withReadings.annotation = annotationState{status: annotationReady, segments: words}
	renderTranslationCard(t, "translate-readings.png", withReadings)
	renderTranslationCard(t, "translate-unconfigured.png", translationState{
		status:  translateUnconfigured,
		source:  "Hello",
		failure: "还没有配置凭据。",
	})
	renderTranslationCard(t, "translate-failed.png", translationState{
		status:  translateFailed,
		source:  "Hello",
		failure: "请求超时，请检查网络。",
	})
	renderTranslationCard(t, "translate-empty.png", translationState{
		status:  translateNoText,
		failure: "先选中文本并复制，再按快捷键",
	})
	renderTranslationCard(t, "translate-off.png", translationState{status: translateOff})
	// A translation longer than any card: the body scrolls, and the foot
	// stays on the card's last row.
	renderTranslationCard(t, "translate-long.png", translationState{
		status: translateReady,
		source: strings.Repeat("コンパイラはソースコードを機械語に変換するソフトです。", 4),
		result: translate.Result{
			SourceText:     strings.Repeat("コンパイラはソースコードを機械語に変換するソフトです。", 4),
			TranslatedText: strings.Repeat("编译器是把源代码转换成机器语言的软件。", 40),
			SourceLang:     domain.LangJapanese,
			TargetLang:     domain.LangChinese,
			ProviderID:     "deepseek",
			ProviderName:   "DeepSeek",
		},
	})
}

// renderTranslationCard writes one state of the translation card, so that the
// look of a state can be judged without pressing the shortcut to reach it.
//
// The card sizes itself to what it drew, so the frame is rendered twice: once
// into a window tall enough to show everything, to find out how tall the card
// turned out, and once at that height, which is the card as it appears.
func renderTranslationCard(t *testing.T, name string, state translationState) {
	t.Helper()
	dir := os.Getenv(renderDirEnv)
	if dir == "" {
		return
	}
	app := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	app.translation = state
	const scale = 2
	tall := ui.Render(app.translateView, translateWidth, translateMaxHeight+200, scale)
	height := cardHeightIn(tall, scale)
	img := ui.Render(app.translateView, translateWidth, height, scale)
	file, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		t.Fatalf("encode %s: %v", name, err)
	}
}

// renderTranslationCardHovered is renderTranslationCard with the pointer
// resting on the row of that name: the face a row takes under the pointer is
// the one thing about it a plain render cannot show.
func renderTranslationCardHovered(t *testing.T, name string, state translationState, label string) {
	t.Helper()
	dir := os.Getenv(renderDirEnv)
	if dir == "" {
		return
	}
	app := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	app.translation = state
	const scale = 2
	tall := ui.Render(app.translateView, translateWidth, translateMaxHeight+200, scale)
	// The tester draws the frame the pointer is over, which is the one that
	// has the hover in it.
	tt := ui.NewTester(app.translateView, translateWidth, cardHeightIn(tall, scale))
	rect, ok := tt.Find(label)
	if !ok {
		t.Fatalf("%s is not drawn, so it cannot be hovered", label)
	}
	tt.Move(rect.X+rect.W/2, rect.Y+rect.H/2)
	file, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	defer file.Close()
	if err := png.Encode(file, tt.Image()); err != nil {
		t.Fatalf("encode %s: %v", name, err)
	}
}

// cardHeightIn is how tall the card turned out in a frame: the card draws its
// own outline, and the frame is filled with the card's face, so the card ends
// at the last row where its outline crosses the middle of the frame. It is how
// the harness asks the card how tall it is without a window to resize.
//
// The outline is looked for in the middle rather than at the left edge: the
// left edge is the card's side, which every row of the card has, so the last
// row with a pixel there is wherever the corner's arc happens to end — a
// radius short of the bottom. Only the card's own bottom edge crosses the
// middle.
func cardHeightIn(img *image.RGBA, scale int) int {
	middle := img.Rect.Dx() / 2
	last := 0
	for y := 0; y < img.Rect.Dy(); y++ {
		if img.RGBAAt(middle, y) == rgba(colorBorder) {
			last = y
		}
	}
	if last == 0 {
		// Nothing drew an outline: fall back to the few rows the frame has,
		// so that a broken state is still written out and can be looked at.
		return translateMinHeight
	}
	return last/scale + 1
}

// dumpRectsEnv makes the manual test print where every text of both windows
// sits. A layout assertion in the tests is written against these numbers.
const dumpRectsEnv = "BOKU_DUMP_RECTS"

// TestDumpRectsForInspection prints the rectangles of both windows. It is
// skipped unless BOKU_DUMP_RECTS is set, and prints nothing during an
// ordinary test run.
func TestDumpRectsForInspection(t *testing.T) {
	if os.Getenv(dumpRectsEnv) == "" {
		t.Skip("set BOKU_DUMP_RECTS to print the rectangles")
	}

	dump := func(name string, view func(*ui.Context), width, height int) {
		tt := ui.NewTester(view, width, height)
		fmt.Printf("---- %s ----\n", name)
		for _, text := range tt.Texts() {
			if rect, ok := tt.Find(text); ok {
				fmt.Printf("%-40q x=%6.1f y=%6.1f w=%6.1f h=%5.1f\n", text, rect.X, rect.Y, rect.W, rect.H)
			}
		}
		img := ui.Render(view, width, height, 2)
		fmt.Printf("divider rows: %v\n", dividerRows(img, ui.LightTheme().Border, width*2))
	}

	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	loaded(t, state)
	state.loadCredentials()
	dump("flyout", state.View, panelWidth, panelHeight)
	dump("settings", state.settingsView, settingsWidth, settingsHeight)
}
