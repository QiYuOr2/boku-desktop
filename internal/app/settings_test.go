package app

import (
	"context"
	"errors"
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func settingsText(t *testing.T, state *State) []string {
	t.Helper()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)
	return tt.Texts()
}

// exact reports whether one drawn text is exactly this, rather than
// containing it: what a window must not offer is its own word, and a
// sentence about it is not the same thing.
func exact(texts []string, want string) bool {
	for _, text := range texts {
		if text == want {
			return true
		}
	}
	return false
}

// settingsNavRects is where each sidebar entry is drawn, in the order the
// sidebar shows them. The tester answers with the first element of a text and
// the sidebar is built before the page, so the entries come first.
func settingsNavRects(t *testing.T, tt *ui.Tester) []ui.Rect {
	t.Helper()
	pages := testState(t, newTestProvider(), newTestStore(), "0.1.0").settingsPages()
	rects := make([]ui.Rect, 0, len(pages))
	for _, page := range pages {
		rect, ok := tt.Find(page.name)
		if !ok {
			t.Fatalf("the sidebar does not show %q; the frame shows %q", page.name, tt.Texts())
		}
		if rect.X >= settingsSidebarWidth {
			t.Fatalf("the entry for %q is at x=%.0f, outside the sidebar", page.name, rect.X)
		}
		rects = append(rects, rect)
	}
	return rects
}

// navPixels renders the settings window and answers what color each sidebar
// entry is drawn in, sampled at its own middle: what tells the user which
// section is open is the fill behind it.
func navPixels(t *testing.T, state *State, rects []ui.Rect) []color.RGBA {
	t.Helper()
	return navPixelsAt(t, state, rects, settingsHeight)
}

// navPixelsAt is navPixels in a window of this height. Rendering a frame
// builds the view, which is where the sidebar decides what is open, so it is
// how a test looks at the entry rather than only at the state behind it.
func navPixelsAt(t *testing.T, state *State, rects []ui.Rect, height int) []color.RGBA {
	t.Helper()
	const scale = 2
	img := ui.Render(state.settingsView, settingsWidth, height, scale)
	out := make([]color.RGBA, 0, len(rects))
	for _, rect := range rects {
		x := int(rect.X)*scale + 2
		y := int(rect.Y+rect.H/2) * scale
		out = append(out, img.RGBAAt(x, y))
	}
	return out
}

// The settings window is navigated by a sidebar: one entry per section of the
// page, in the page's own order, at the window's left edge — and the section
// the page is showing is filled in the brand green while the others are not.
func TestSettingsSidebarNamesEverySection(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredentials()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)

	pages := state.settingsPages()
	rects := settingsNavRects(t, tt)
	if len(rects) != 5 {
		t.Fatalf("the sidebar shows %d entries, want the page's five sections", len(rects))
	}
	// The sidebar lists the page's sections in the page's own order, so an
	// entry always points at the section it names.
	for index, page := range pages {
		if page.mark == nil {
			t.Errorf("the entry for %q has no mark to draw", page.name)
		}
		if !tt.HasText(page.name) {
			t.Errorf("the sidebar does not name %q", page.name)
		}
		if index > 0 && rects[index].Y <= rects[index-1].Y {
			t.Errorf("the entry for %q at y=%.0f is not under %q at y=%.0f",
				page.name, rects[index].Y, pages[index-1].name, rects[index-1].Y)
		}
		if rects[index].X > settingsSidebarWidth {
			t.Errorf("the entry for %q reaches x=%.0f, past the sidebar", page.name, rects[index].X)
		}
	}
	// The page comes after the sidebar: the first section's own line is right
	// of every entry.
	head := findText(t, tt, "配置各 AI 服务密钥")
	for _, rect := range rects {
		if rect.X+rect.W > head.X {
			t.Errorf("the entry at x=%.0f+%.0f overlaps the page at x=%.0f", rect.X, rect.W, head.X)
		}
	}

	// One entry is open — the first section, which is what the page shows
	// before anything is scrolled — and it is the one filled in the brand
	// green.
	fills := navPixels(t, state, rects)
	if state.settingsSection != sectionAPIKey {
		t.Fatalf("the sidebar opens %v, want the first section", state.settingsSection)
	}
	if fills[0] != rgba(colorBrand) {
		t.Errorf("the first entry is drawn on %v, want the brand green %v", fills[0], colorBrand)
	}
	for index, fill := range fills[1:] {
		if fill == rgba(colorBrand) {
			t.Errorf("the entry for %q is filled green while another section is open", pages[index+1].name)
		}
		if fill != rgba(colorSidebar) {
			t.Errorf("the entry for %q is drawn on %v, want the sidebar's own face %v",
				pages[index+1].name, fill, colorSidebar)
		}
	}
}

// The sidebar's mark for an entry is the mark its own section draws
// (docs/design.md §2.1): the key, the translation mark, the clipboard, the
// gear, the info circle. The general entry in particular is the gear the
// section's heading carries, not a drawing chosen for the sidebar alone.
func TestSettingsSidebarDrawsEachSectionMark(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	want := map[settingsSectionKind]*ui.SVG{
		sectionAPIKey:    state.keyMark,
		sectionTranslate: state.translationMark,
		sectionClipboard: state.clipboardMark,
		sectionGeneral:   state.settingsMark,
		sectionAbout:     state.infoMark,
	}
	for _, page := range state.settingsPages() {
		mark, known := want[page.kind]
		if !known {
			t.Fatalf("the sidebar has an entry for section %v that this test does not know", page.kind)
		}
		if page.mark != mark {
			t.Errorf("the entry for %q draws a mark of its own, want the mark its section draws", page.name)
		}
	}
}

// The sidebar's entries are what navigates the page: the whole entry is the
// target, clicking one marks it open straight away, and it is what sends the
// page to that section.
func TestSettingsSidebarOpensTheSectionItNames(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredentials()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)
	rects := settingsNavRects(t, tt)
	translate := rects[sectionTranslate]

	// The whole entry answers, not only the words in it: the click lands in
	// the entry's leading room, left of its label.
	tt.ClickAt(translate.X+2, translate.Y+translate.H/2)
	tt.Frame()

	// The entry is open from the click on: the page is a scroll away from
	// where it is going, and the entry the user pressed is the one marked.
	if state.settingsSection != sectionTranslate {
		t.Errorf("the sidebar opens %v after its entry was clicked, want 划词翻译", state.settingsSection)
	}
	fills := navPixels(t, state, rects)
	if fills[sectionTranslate] != rgba(colorBrand) {
		t.Errorf("the clicked entry is drawn on %v, want the brand green %v",
			fills[sectionTranslate], colorBrand)
	}
	if fills[sectionAPIKey] == rgba(colorBrand) {
		t.Error("the entry of the section the page is leaving is still filled green")
	}

	// The page is on its way to that section: the section below the first is
	// below the top of the content, so the page has started moving down.
	if state.settingsReveal == nil {
		t.Error("the click did not send the page anywhere: no section is left to be reached")
	}
	if state.settingsScroll.Y <= 0 {
		t.Errorf("the page is at %.1f, want the second section below the first",
			state.settingsScroll.Y)
	}
}

// While the page is travelling, the entry the user clicked is the only one
// drawn green. It is read from a rendered frame rather than from the state,
// because that is where the flicker was: a frame drawn mid-travel used to
// light the entry of a section the page was passing, and then the one asked
// for once the page arrived.
func TestSettingsSidebarKeepsTheClickedEntryLitWhileTravelling(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredentials()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)
	rects := settingsNavRects(t, tt)

	tt.ClickAt(rects[sectionGeneral].X+2, rects[sectionGeneral].Y+rects[sectionGeneral].H/2)
	tt.Frame()
	if !state.settingsScroll.Scrolling() {
		t.Fatal("the page is not travelling, so there is nothing to check")
	}

	// Frames are drawn while the page is on its way — drawing is what a
	// window does every frame, and it builds the view again. Each drawing
	// costs its own few frames of travel, so a dozen of them already covers
	// most of the way.
	for frame := 0; frame < 12 && state.settingsScroll.Scrolling(); frame++ {
		fills := navPixelsAt(t, state, rects, settingsHeight)
		if fills[sectionGeneral] != rgba(colorBrand) {
			t.Fatalf("frame %d: the clicked entry is drawn on %v, want the brand green %v",
				frame, fills[sectionGeneral], colorBrand)
		}
		for index, fill := range fills {
			if index != int(sectionGeneral) && fill == rgba(colorBrand) {
				t.Fatalf("frame %d: the entry of section %d is lit while the page travels to another",
					frame, index)
			}
		}
		tt.Frame()
	}
}

// A second click while the page is still travelling to the first one is
// answered: the page turns toward the entry clicked last and stops there,
// rather than carrying on to the section that was clicked first.
func TestSettingsSidebarRetargetsWhileTravelling(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredentials()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)
	rects := settingsNavRects(t, tt)

	// A section far down the page, and then a nearer one before the page has
	// got anywhere near the first.
	tt.ClickAt(rects[sectionGeneral].X+2, rects[sectionGeneral].Y+rects[sectionGeneral].H/2)
	tt.Frame()
	for frame := 0; frame < 3; frame++ {
		tt.Frame()
	}
	if !state.settingsScroll.Scrolling() {
		t.Fatal("the first travel finished too quickly to be interrupted")
	}
	tt.ClickAt(rects[sectionTranslate].X+2, rects[sectionTranslate].Y+rects[sectionTranslate].H/2)
	tt.Frame()

	// The click is answered at once: the entry it names is the open one.
	if state.settingsSection != sectionTranslate {
		t.Errorf("the sidebar opens %d after the second click, want 划词翻译", state.settingsSection)
	}
	if !state.settingsAiming || state.settingsAimAt != sectionTranslate {
		t.Errorf("the page is aiming at %v/%d, want the section clicked last",
			state.settingsAiming, state.settingsAimAt)
	}
	// The page turns toward it rather than carrying on downwards.
	for frame := 0; frame < 240 && state.settingsScroll.Scrolling(); frame++ {
		tt.Frame()
		if state.settingsScroll.Y >= state.settingsScroll.MaxY-1 {
			break
		}
	}

	// It stopped at the section clicked last: its head at the anchor, and
	// that entry open. The one clicked first is above the page's top.
	translate := findText(t, tt, "任意语言 → 中文 · AI 翻译")
	if translate.Y < settingsAnchor || translate.Y > settingsAnchor+24 {
		t.Errorf("the section clicked last is at y=%.1f, want its head at the anchor of y=%.0f",
			translate.Y, float32(settingsAnchor))
	}
	if state.settingsScroll.Y >= state.settingsScroll.MaxY-1 {
		t.Error("the page ran to its end: it kept going after the second click")
	}
	if state.settingsSection != sectionTranslate {
		t.Errorf("the sidebar opens %d once the page stopped, want 划词翻译",
			state.settingsSection)
	}
	if state.settingsAiming {
		t.Error("the page has stopped and is still aiming at something")
	}
}

// The entry the sidebar draws and the section at the top of the page are the
// same chapter as soon as the page has arrived: the highlight does not trail
// the scroll by a frame. A frame is what moves the page, and the frame after
// it is the one that reads where the page is — so the reading is done in that
// frame, with nothing drawn in between.
func TestSettingsSidebarAgreesWithThePageWhenThePageStops(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredentials()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)
	rects := settingsNavRects(t, tt)

	tt.ClickAt(rects[sectionGeneral].X+2, rects[sectionGeneral].Y+rects[sectionGeneral].H/2)
	// One frame sets the page moving, and the frames after it carry it. The
	// frame that finds the scroll stopped is one whose boxes are still those
	// of the offset the page has left, so it hands the entry over.
	for frame := 0; frame < 240; frame++ {
		tt.Frame()
		if !state.settingsAiming {
			break
		}
	}
	// This is the frame that reads the page where it now is. The entry has to
	// be the chapter the frame itself marks, in this frame: a frame later
	// would be the highlight trailing the scroll. The page is at its end,
	// where the last section is the one it managed to show.
	tt.Frame()
	if state.settingsAiming {
		t.Fatal("the page never stopped")
	}
	if state.settingsSection != sectionAbout {
		t.Errorf("the sidebar opens %d on the frame after the page stopped, want the section at the page's end",
			state.settingsSection)
	}
	fills := navPixels(t, state, rects)
	if fills[sectionAbout] != rgba(colorBrand) {
		t.Errorf("the entry for the page's end is drawn on %v, want the brand green %v",
			fills[sectionAbout], colorBrand)
	}
	if fills[sectionGeneral] == rgba(colorBrand) {
		t.Error("the entry clicked is still lit although the page moved past it")
	}
}

// The section asked for is the last one but one, which the page is not tall
// enough to put at the top: it stops at its end, and the entry that keeps the
// page is the one the user clicked.
func TestSettingsSidebarGlidesAndKeepsTheEntryOpen(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredentials()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)
	rects := settingsNavRects(t, tt)

	tt.ClickAt(rects[sectionGeneral].X+2, rects[sectionGeneral].Y+rects[sectionGeneral].H/2)
	tt.Frame()

	// The frame after the click is on the way, not there: the page has left
	// where it was, and has not arrived.
	start := state.settingsScroll.Y
	if start <= 0 {
		t.Fatalf("the page did not leave the top: at %.1f", start)
	}
	if !state.settingsScroll.Scrolling() {
		t.Fatal("the page jumped to the section instead of travelling to it")
	}
	if !state.settingsAiming {
		t.Fatal("the page arrived in one frame, so there is no travel to check")
	}

	// Every frame on the way moves the page further from where it started,
	// and the entry the user clicked is the open one on all of them: the
	// frames are driven without drawing anything, because drawing a frame of
	// its own is what a render does, and it would carry the page along with
	// it.
	reached, steps := start, 0
	for frame := 0; frame < 240; frame++ {
		tt.Frame()
		if !state.settingsScroll.Scrolling() {
			break
		}
		if state.settingsSection != sectionGeneral {
			t.Fatalf("frame %d: the sidebar opens %d on the way, want what was clicked",
				frame, state.settingsSection)
		}
		if !state.settingsAiming {
			t.Fatalf("frame %d: the travel was given up while the page is still moving", frame)
		}
		if state.settingsScroll.Y < reached {
			t.Fatalf("frame %d: the page went backwards, to %.1f from %.1f",
				frame, state.settingsScroll.Y, reached)
		}
		if state.settingsScroll.Y > reached {
			reached, steps = state.settingsScroll.Y, steps+1
		}
		if !state.settingsScroll.Scrolling() {
			break
		}
	}
	// The frame that stops the scroll is the one that reads the offset before
	// it stopped, so the sidebar is told it arrived on the frame after.
	tt.Frame()

	if steps < 3 {
		t.Errorf("the page moved in %d steps, want a scroll rather than a jump", steps)
	}
	// It came to rest at the end of the page: the general section is the
	// fourth, and the page is not tall enough for its head to reach the
	// anchor, so the offset it settles at is as far as the page scrolls.
	if target := state.settingsScroll.MaxY; reached < target-2 || reached > target+2 {
		t.Errorf("the page came to rest at %.1f, want the end of the page at %.1f", reached, target)
	}
	// The page is not moving any more, and it is not on its way anywhere.
	if state.settingsAiming {
		t.Error("the page stopped and is still aiming at something")
	}
	// The entry that stays open is the section the page came to rest on. The
	// page could not bring the section that was clicked up to the middle — that
	// is what the end of a page is — and at its end the section it managed to
	// show is the last one, so that is the one lit.
	if state.settingsSection != sectionAbout {
		t.Errorf("the sidebar opens %d with the page at its end, want the last section",
			state.settingsSection)
	}
	fills := navPixels(t, state, rects)
	if fills[sectionAbout] != rgba(colorBrand) {
		t.Errorf("the last entry is drawn on %v, want the brand green %v",
			fills[sectionAbout], colorBrand)
	}
	if fills[sectionGeneral] == rgba(colorBrand) {
		t.Error("the entry of a section the page scrolled past is lit")
	}
}

// A click on an entry takes the page to that section: the page is scrolled to
// the section's own place and stops with its head at the anchor, which is
// what tells the user that the section begins there.
//
// The section asked for is the second one: the page is taller than the window
// but not taller than twice it, so the last sections cannot reach the anchor —
// the page stops at its end rather than scrolling past it.
func TestSettingsSidebarTakesThePageToTheSection(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredentials()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)
	rects := settingsNavRects(t, tt)

	// Where the section is before anything is scrolled. Its box begins a
	// little above this line — the box holds the mark and the name as well —
	// so the offset the page stops at is a few points less than this.
	translate := findText(t, tt, "任意语言 → 中文 · AI 翻译")
	want := translate.Y - settingsAnchor

	tt.ClickAt(rects[sectionTranslate].X+2, rects[sectionTranslate].Y+rects[sectionTranslate].H/2)
	// The page travels: a scroll is a glide rather than a jump. Frames are
	// run until it stops.
	for frame := 0; frame < 90; frame++ {
		before := state.settingsScroll.Y
		tt.Frame()
		if state.settingsScroll.Y == before {
			break
		}
	}

	if got := state.settingsScroll.Y; got < want-24 || got > want {
		t.Errorf("the page stopped at %.1f, want the section's own place, a little under %.1f", got, want)
	}
	// And the section is where it was asked to be: its head at the anchor,
	// with the sidebar saying that is the open one.
	traveled := findText(t, tt, "任意语言 → 中文 · AI 翻译")
	if traveled.Y < settingsAnchor || traveled.Y > settingsAnchor+24 {
		t.Errorf("the section asked for is at y=%.1f, want its head at the anchor of y=%.0f",
			traveled.Y, float32(settingsAnchor))
	}
	if state.settingsSection != sectionTranslate {
		t.Errorf("the sidebar opens %d with the section at the anchor, want 划词翻译",
			state.settingsSection)
	}
	if keys := findText(t, tt, "配置各 AI 服务密钥"); keys.Y >= traveled.Y {
		t.Errorf("the first section at y=%.0f is not above the section asked for at y=%.0f",
			keys.Y, traveled.Y)
	}
}

// The sidebar follows the page as well: the entry changes when a section's
// heading comes up past the middle of the page, so the green entry is always
// the chapter the reader is in rather than the one that happens to be at the
// top.
func TestSettingsSidebarFollowsThePage(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredentials()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)
	rects := settingsNavRects(t, tt)

	// The page as it is drawn: where the clipboard heading is, and where the
	// page has to be scrolled to for that heading to sit on the middle of the
	// page. The room the page scrolls in is not the window: it is the window
	// less its footer, and the frame measures it as it draws.
	clipboard := findText(t, tt, "监听与预览设置")
	if clipboard.Y <= settingsAnchor {
		t.Fatalf("the clipboard heading is at y=%.0f, already at the middle", clipboard.Y)
	}
	tt.Frame()
	if state.settingsRoom <= 0 {
		t.Fatalf("the page's room is %.1f, want what the frame measured", state.settingsRoom)
	}
	middle := state.settingsRoom / 2
	state.settingsScroll.Y = clipboard.Y - middle
	tt.Frame()
	tt.Frame()

	if state.settingsSection != sectionClipboard {
		t.Errorf("the sidebar opens %d with the clipboard heading on the middle of the page, want 剪贴板",
			state.settingsSection)
	}
	fills := navPixels(t, state, rects)
	if fills[sectionClipboard] != rgba(colorBrand) {
		t.Errorf("the entry for the section in view is drawn on %v, want the brand green %v",
			fills[sectionClipboard], colorBrand)
	}
	if fills[sectionAPIKey] == rgba(colorBrand) || fills[sectionTranslate] == rgba(colorBrand) {
		t.Error("an entry above the section in view is still filled green")
	}
	// Just above that point the entry above it is the one open: the change
	// happens at the middle, not at the top.
	state.settingsScroll.Y = clipboard.Y - middle - 40
	tt.Frame()
	tt.Frame()
	if state.settingsSection != sectionTranslate {
		t.Errorf("the sidebar opens %d just before the heading reaches the middle, want the section above",
			state.settingsSection)
	}
}

// The settings window is a page of settings, and connecting a data source is
// one section of it: the title bar names the window, the section carries the
// provider's own mark and name, and the page says where a credential is going
// before it is handed over.
func TestSettingsShowsTheConnectForm(t *testing.T) {
	state := testState(t, newTestProvider(), newTestStore(), "0.1.0")
	texts := settingsText(t, state)

	for _, want := range []string{
		"DeepSeek",
		"API Key",
		"测试",
		"保存",
		"Boku 0.1.0",
	} {
		if !contains(texts, want) {
			t.Errorf("the settings window does not show %q; it shows %q", want, texts)
		}
	}
	// The window says what it is once — in the system's title bar — and not
	// again inside itself. Nor the address the provider asks, which is the
	// provider's business. These are matched whole: a sentence about the
	// clipboard may well contain the word "关闭" without offering a close
	// action.
	for _, unwanted := range []string{appName + " 设置", "配置数据源", "关闭", "API endpoint", "api.deepseek.com"} {
		if exact(texts, unwanted) {
			t.Errorf("the settings window still says %q: %q", unwanted, texts)
		}
	}
	// The key section is the first thing the window draws: nothing of the
	// window's own is above it, since its name is in the system's title bar
	// rather than in the view.
	marked := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)
	head, ok := marked.Find("API Key")
	if !ok {
		t.Fatalf("the key section was not found; the window shows %q", texts)
	}
	if head.Y > pageEdge+4 {
		t.Errorf("the key section starts at y=%.0f: something is above it", head.Y)
	}
}

// A service is one section among the settings window's sections, and one card
// among the services the section configures: the card's own name heads it, and
// the field under it starts at the same place — the card is a box of its own
// rather than a column of rows beside a logo.
func TestSettingsPutsTheProvidersInCards(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredentials()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)

	// The card's name and its field are found by what they are: the field
	// carries its card's name, because the card keeps no room for a label
	// over it.
	name := findText(t, tt, "DeepSeek")
	field := findText(t, tt, "DeepSeek API Key")
	section := findText(t, tt, "配置各 AI 服务密钥")
	warning := findTextPrefix(t, tt, "凭据明文保存在")

	if name.X < pageEdge {
		t.Errorf("the service's card reaches the page's edge at x=%.0f", name.X)
	}
	if field.X > name.X {
		t.Errorf("the card's field at x=%.0f is indented under the card's own name at x=%.0f",
			field.X, name.X)
	}
	if field.Y <= name.Y {
		t.Errorf("the card's field at y=%.0f is not under the card's own name at y=%.0f",
			field.Y, name.Y)
	}
	if section.Y >= name.Y {
		t.Errorf("the first card at y=%.0f is not under the section's head at y=%.0f", name.Y, section.Y)
	}
	if warning.Y < section.Y {
		t.Errorf("the fields are out of order: head y=%.0f, warning y=%.0f", section.Y, warning.Y)
	}
}

// A data source is one section among the settings window's sections, not the
// window: the cards sit inside the key section rather than above it.
func TestSettingsCardsComeAfterTheSectionHead(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredentials()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)

	head := findText(t, tt, "API Key")
	card := findText(t, tt, "DeepSeek")
	if card.Y <= head.Y {
		t.Errorf("the first card at y=%.0f is not under the section's head at y=%.0f", card.Y, head.Y)
	}
}

// The credential is stored in the clear today, and the window has to say so
// rather than imply otherwise.
func TestSettingsAdmitsTheCredentialIsNotEncrypted(t *testing.T) {
	state := testState(t, newTestProvider(), newTestStore(), "0.1.0")
	texts := settingsText(t, state)
	if !contains(texts, "明文") {
		t.Errorf("the settings window does not warn about plain-text storage: %q", texts)
	}
}

// A stored credential is a fact the window states and nothing more: the card
// says the service is configured, and no part of the value — not its last four
// characters either — is drawn anywhere.
func TestSettingsShowsNothingOfAStoredCredential(t *testing.T) {
	store := withCredential(newTestStore())
	state := testState(t, newTestProvider(), store, "0.1.0")
	state.loadCredentials()

	if !state.form.hasCredential["deepseek"] {
		t.Fatal("the form does not see the stored credential")
	}
	// The stored value is not in the field: the field is what a new key is
	// typed into, and it draws its own hint while it is empty.
	if typed := state.cardInput("deepseek"); typed != "" {
		t.Errorf("the field holds %q, want the empty field that draws its hint", typed)
	}
	texts := settingsText(t, state)
	if !contains(texts, "已配置") {
		t.Errorf("the settings window does not say the service is configured: %q", texts)
	}
	for _, text := range texts {
		for _, leak := range []string{"sk-test-0001", "…0001"} {
			if contains([]string{text}, leak) {
				t.Errorf("the settings window draws %q, which is part of the credential: %q", leak, text)
			}
		}
	}
}

// Typing is what replaces a stored credential, and it does so on its own: the
// window offers no 更换 to press, and until the field holds something there is
// nothing new to save. Removing it is a separate action, and it is asked twice
// — see TestSettingsAsksTwiceBeforeRemovingACredential.
func TestSettingsATypedKeyIsTheReplacement(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredentials()
	deepseekCard := translateCard{id: "deepseek", credentialKey: "apiKey", needsCredential: true}

	// The window as it opens: the card says a credential is stored, and the
	// field is empty, which is the state that has nothing new in it.
	if !state.credentialIsShown(deepseekCard) {
		t.Fatal("a stored credential with an empty field is not read as stored")
	}
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)
	if !tt.HasText("已配置") {
		t.Errorf("the window does not say a credential is stored: %q", tt.Texts())
	}
	for _, gone := range []string{"更换", "确认", "取消"} {
		// The window draws these as actions, so an action by that name is
		// what is looked for: a sentence that happens to contain the word
		// ("冲突时请更换") is not an action.
		for _, label := range []string{gone, "快捷键 " + gone} {
			if exact(tt.Texts(), label) {
				t.Errorf("the window still offers %q as an action: %q", label, tt.Texts())
			}
		}
	}

	// Typing into the field is the whole of asking to replace it. The field
	// belongs to one card at a time, so what was typed is filed under the
	// card it was typed into.
	state.showCardInput("deepseek")
	state.form.input = "sk-replaced-0002"
	if state.credentialIsShown(deepseekCard) {
		t.Error("a typed key is still read as nothing new to save")
	}
	// Moving to the other card files it, and coming back finds it again.
	state.showCardInput("custom")
	if got := state.cardInput("deepseek"); got != "sk-replaced-0002" {
		t.Errorf("the typed key was lost when the field moved: %q", got)
	}
	if got := state.keyValue("deepseek"); got != "sk-replaced-0002" {
		t.Errorf("reading the card gives %q", got)
	}
}

// Saving without a key says what is missing instead of doing nothing.
func TestSettingsRefusesToSaveAnEmptyKey(t *testing.T) {
	state := testState(t, newTestProvider(), newTestStore(), "0.1.0")
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)

	if err := tt.Click("保存"); err != nil {
		t.Fatalf("click 保存: %v", err)
	}
	tt.Frame()
	card, ok := state.form.status["deepseek"]
	if !ok {
		t.Fatalf("the card said nothing; the window shows %q", tt.Texts())
	}
	if card.status != statusError {
		t.Fatalf("status = %d, want an error", card.status)
	}
	if !contains([]string{card.text}, "API Key") {
		t.Errorf("the form does not say what is missing: %q", card.text)
	}
}

// A service with no key is not a service the picker offers, and the window
// says which one to configure rather than failing later.
func TestSettingsReportsWhichServiceIsConfigured(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	texts := settingsText(t, state)
	// DeepSeek has the panel's key and the user's own service has no
	// address, so the two cards say different things.
	if !contains(texts, "已配置") || !contains(texts, "未配置") {
		t.Errorf("the cards do not report their state: %q", texts)
	}
	if !contains(texts, "仅显示已配置 API Key 的服务可选") {
		t.Errorf("the picker does not say what it is showing: %q", texts)
	}
}

// The five sections the design asks for, and the sections it does not: the
// reading and rent pages are not part of this build.
func TestSettingsShowsTheFiveSections(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredentials()
	texts := settingsText(t, state)
	for _, want := range []string{
		"API Key", "配置各 AI 服务密钥",
		"划词翻译", "任意语言 → 中文 · AI 翻译",
		"剪贴板", "监听与预览设置",
		"通用", "启动与外观",
		"关于", "版本与构建",
	} {
		if !contains(texts, want) {
			t.Errorf("the settings window does not show %q; it shows %q", want, texts)
		}
	}
	for _, unwanted := range []string{"阅读", "房租", "每日目标（分钟）", "检查更新"} {
		if exact(texts, unwanted) {
			t.Errorf("the settings window shows %q, which this build does not do", unwanted)
		}
	}
}

// The shortcut row: what the key does, what it is bound to, and how to record
// another one. The other keys the design draws are not implemented, and the
// window says so instead of drawing buttons that do nothing.
func TestSettingsShowsTheShortcutRow(t *testing.T) {
	state, _, shortcuts := testStateWith(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.RegisterShortcuts()
	texts := settingsText(t, state)
	for _, want := range []string{
		"快捷键",
		"划词后弹出翻译",
		"选中文字后按此键显示浮窗",
		"Ctrl + Shift + T",
		"恢复默认快捷键",
		"点击快捷键可重新录制；与系统或其他软件冲突时请更换。",
		"朗读、固定与关闭的组合键尚未实现。",
	} {
		if !contains(texts, want) {
			t.Errorf("the shortcut row does not show %q; it shows %q", want, texts)
		}
	}
	if got := shortcuts.bound(); got != "Ctrl+Shift+T" {
		t.Errorf("the registered shortcut is %q, want the default", got)
	}
	// The design's other keys are not registered either: one binding is the
	// whole of what this build does with the keyboard.
	if registrations, _ := shortcuts.counts(); registrations != 1 {
		t.Errorf("the application registered %d shortcuts, want one", registrations)
	}
}

// A registration the platform refuses is visible rather than silent, and the
// application keeps running: docs/tools.md §6.
func TestSettingsShowsARegistrationFailure(t *testing.T) {
	state, _, shortcuts := testStateWith(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	shortcuts.failWith(errors.New("already registered by another application"))
	state.RegisterShortcuts()

	texts := settingsText(t, state)
	if !contains(texts, "无法注册") {
		t.Errorf("a failed registration is not reported: %q", texts)
	}
	if shortcuts.bound() != "" {
		t.Errorf("something was bound although registration failed: %q", shortcuts.bound())
	}
	// The window still draws: a tray application without a shortcut starts,
	// and the section it failed to register is on the page.
	if texts := settingsText(t, state); len(texts) == 0 {
		t.Error("the settings window draws nothing")
	}
}

func TestSettingsWithoutAPanel(t *testing.T) {
	state := testState(t, newTestProvider(), newTestStore(), "0.1.0")
	state.panelService = emptyPanelService(t)
	texts := settingsText(t, state)
	if !contains(texts, "剪贴板") {
		t.Errorf("the window does not draw its own sections: %q", texts)
	}
}

// The window is closed by the close button the system draws in its title
// bar; the view therefore has no action of its own, and its footer is the
// version alone.
func TestSettingsHasNoCloseAction(t *testing.T) {
	state := testState(t, newTestProvider(), newTestStore(), "0.1.0")
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)
	if exact(tt.Texts(), "关闭") {
		t.Errorf("the settings window offers 关闭 in its own view: %q", tt.Texts())
	}
	if version, ok := tt.Find("Boku 0.1.0"); !ok {
		t.Errorf("the settings footer does not show the version: %q", tt.Texts())
	} else if version.W > 100 {
		t.Errorf("the version is %.0f wide: something was added beside it", version.W)
	}
}

// countColor counts the pixels drawn in exactly this color: a mark drawn in
// a flat color has a large interior of it, so a handful of pixels means it
// was not drawn at all.
func countColor(img *image.RGBA, want ui.Color) int {
	count := 0
	for i := 0; i+3 < len(img.Pix); i += 4 {
		if img.Pix[i] == want.R && img.Pix[i+1] == want.G && img.Pix[i+2] == want.B && img.Pix[i+3] == want.A {
			count++
		}
	}
	return count
}

// The plain-text warning names the file the credentials are in rather than the
// whole path: the line is a hint, and the about box spells the path out.
func TestCredentialFileIsTheFileName(t *testing.T) {
	cases := map[string]string{
		`C:\Users\me\AppData\Roaming\Boku\secrets.json`: "secrets.json",
		"/home/me/.local/share/boku/secrets.json":       "secrets.json",
		"secrets.json":      "secrets.json",
		`C:\Users\me\Boku\`: `C:\Users\me\Boku\`,
		"本机文件":              "本机文件",
	}
	for path, want := range cases {
		if got := fileOf(path); got != want {
			t.Errorf("fileOf(%q) = %q, want %q", path, got, want)
		}
	}
}

// The warning is one line rather than a panel with a title over it, it names
// the file, and it sits under the section's head.
func TestSettingsWarningIsOneLine(t *testing.T) {
	state := testState(t, newTestProvider(), newTestStore(), "0.1.0")
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)

	warning, ok := textWithPrefix(tt.Texts(), "凭据明文保存在")
	if !ok {
		t.Fatalf("the window does not warn about plain-text storage: %q", tt.Texts())
	}
	for _, want := range []string{"本机文件", "加密密钥库"} {
		if !contains([]string{warning}, want) {
			t.Errorf("the warning does not say %q: %q", want, warning)
		}
	}
	at := findTextPrefix(t, tt, "凭据明文保存在")
	if at.H > 22 {
		t.Errorf("the warning is %.0f tall: it is a panel with more than one line in it", at.H)
	}
	if head := findText(t, tt, "配置各 AI 服务密钥"); at.Y <= head.Y {
		t.Errorf("the warning at y=%.0f is not under the section's head at y=%.0f", at.Y, head.Y)
	}
}

func TestCredentialPathIsShownAndNeverEmpty(t *testing.T) {
	state := testState(t, newTestProvider(), newTestStore(), "0.1.0")
	if got := state.credentialPath(); got == "" {
		t.Error("the warning has no path to name")
	}
	// The warning names the file; the whole path is in the about box, which
	// is where a user goes to find it.
	texts := settingsText(t, state)
	if !contains(texts, state.credentialPath()) {
		t.Errorf("the settings window does not name where the credentials live: %q", texts)
	}
}

// The cards the window draws are the translators the registry has, with what
// each one needs: the picker and the key section are the same list, read
// twice.
func TestTranslateCardsFollowTheRegistry(t *testing.T) {
	state := testState(t, newTestProvider(), newTestStore(), "0.1.0")
	cards := state.translateCards()
	if len(cards) != 2 {
		t.Fatalf("cards = %+v, want both translators", cards)
	}
	// The one in use comes first.
	if cards[0].id != "deepseek" || cards[0].displayName != "DeepSeek" {
		t.Errorf("the first card is %+v", cards[0])
	}
	if cards[0].credentialKey != "apiKey" || !cards[0].needsCredential {
		t.Errorf("the first card does not ask for a key: %+v", cards[0])
	}
	// The user's own service needs no key and has no address yet.
	if cards[1].id != "custom" || cards[1].needsCredential || cards[1].configured {
		t.Errorf("the custom card is %+v, want an unconfigured service that needs no key", cards[1])
	}
}

func TestCredentialLabel(t *testing.T) {
	cases := map[string]string{
		"apiKey":   "API Key",
		"adminKey": "Admin Key",
		"steamId":  "Steam ID",
		"":         "凭据",
		"other":    "other",
	}
	for key, want := range cases {
		if got := credentialLabel(key); got != want {
			t.Errorf("credentialLabel(%q) = %q, want %q", key, got, want)
		}
	}
}

// The API key section is the settings page's dense one: each card is two lines,
// so the field and the two actions that act on it share one row rather than
// stacking under it.
func TestSettingsPutsTheCardActionsOnTheFieldsRow(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredentials()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)

	field := findText(t, tt, "DeepSeek API Key")
	test := findText(t, tt, "测试")
	save := findText(t, tt, "保存")

	// The field keeps the room that is left of the line and the actions keep
	// their own at its end: none of the three is inside another.
	if field.X+field.W > test.X+1 {
		t.Errorf("the field ends at x=%.0f, inside the action at x=%.0f", field.X+field.W, test.X)
	}
	if test.X+test.W > save.X+1 {
		t.Errorf("the secondary action ends at x=%.0f, inside the primary at x=%.0f", test.X+test.W, save.X)
	}
	// And they are one row: the three share a line rather than stacking.
	centre := func(r ui.Rect) float32 { return r.Y + r.H/2 }
	for name, rect := range map[string]ui.Rect{"测试": test, "保存": save} {
		if d := centre(rect) - centre(field); d > 6 || d < -6 {
			t.Errorf("the %s action is %.0f from the middle of the field's own line", name, d)
		}
	}
	if field.H < 30 || field.H > 34 {
		t.Errorf("the field is %.0f tall, want the design's h-8 field beside its actions", field.H)
	}
}

// A dense card is the design's own size, not the form's: the field and the
// actions beside it are 32 high, and the actions are written small so that
// three things fit one line.
func TestSettingsDenseControlsAreTheDesignsH8(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredentials()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)

	for _, label := range []string{"测试", "保存"} {
		rect := findText(t, tt, label)
		if rect.H < 28 || rect.H > 34 {
			t.Errorf("the %s action is %.0f tall, want the design's h-8", label, rect.H)
		}
	}
	// The same size as each other: one row of things rather than a big one
	// and a small one.
	test := findText(t, tt, "测试")
	if save := findText(t, tt, "保存"); test.H != save.H {
		t.Errorf("the two actions are %.0f and %.0f tall", test.H, save.H)
	}
}

// The key section introduces itself a size down from the others, because its
// cards are: its line and its name are the design's 11 and 14. A smaller mark
// does not move the name, so the sections' names stay one column.
func TestSettingsTheKeySectionIsTheDenseOne(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredentials()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsPageHeight)

	keys := findText(t, tt, "配置各 AI 服务密钥")
	translate := findText(t, tt, "任意语言 → 中文 · AI 翻译")
	if keys.H >= translate.H {
		t.Errorf("the key section's line is %.0f tall and another section's %.0f: the dense section is not a size down",
			keys.H, translate.H)
	}
	if d := keys.X - translate.X; d > 1 || d < -1 {
		t.Errorf("the key section's line starts at x=%.0f and another section's at x=%.0f: the sections' names are not one column",
			keys.X, translate.X)
	}
}

// Removing a credential is offered by the card that holds one, and only by
// that card: a service with nothing stored has nothing to remove.
func TestSettingsOffersRemovalOnlyForWhatIsStored(t *testing.T) {
	state := testState(t, newTestProvider(), newTestStore(), "0.1.0")
	state.loadCredentials()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)
	if exact(tt.Texts(), "删除") {
		t.Errorf("the window offers to remove a credential that is not stored: %q", tt.Texts())
	}
}

// A stored credential is removed from its own card, and only after being asked
// twice: the first press turns the word into the question, and 取消 leaves the
// credential where it was — the key is the one thing here that typing cannot
// put back.
func TestSettingsAsksTwiceBeforeRemovingACredential(t *testing.T) {
	store := withCredential(newTestStore())
	state := testState(t, newTestProvider(), store, "0.1.0")
	state.loadCredentials()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)

	if !exact(tt.Texts(), "删除") {
		t.Fatalf("the card holding a credential does not offer to remove it: %q", tt.Texts())
	}
	if err := tt.Click("删除"); err != nil {
		t.Fatalf("click 删除: %v", err)
	}
	tt.Frame()
	if state.form.deleteArmed != "deepseek" {
		t.Fatalf("the first press did not ask first: armed=%q", state.form.deleteArmed)
	}
	if !exact(tt.Texts(), "确认删除") || !exact(tt.Texts(), "取消") {
		t.Errorf("the card does not say what the second press would do: %q", tt.Texts())
	}
	if _, found := storedSecret(t, state); !found {
		t.Error("the credential was removed by the first press alone")
	}

	if err := tt.Click("取消"); err != nil {
		t.Fatalf("click 取消: %v", err)
	}
	tt.Frame()
	if state.form.deleteArmed != "" {
		t.Errorf("the question is still open after 取消: %q", state.form.deleteArmed)
	}
	if exact(tt.Texts(), "确认删除") {
		t.Errorf("the card still asks after 取消: %q", tt.Texts())
	}
	if _, found := storedSecret(t, state); !found {
		t.Error("a question that was cancelled removed the credential anyway")
	}
}

// Confirming the question removes the credential: the card works on it, the
// write lands in the store, and the card stops saying it holds anything.
func TestSettingsRemovesAConfirmedCredential(t *testing.T) {
	store := withCredential(newTestStore())
	state := testState(t, newTestProvider(), store, "0.1.0")
	state.loadCredentials()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)

	if err := tt.Click("删除"); err != nil {
		t.Fatalf("click 删除: %v", err)
	}
	tt.Frame()
	if err := tt.Click("确认删除"); err != nil {
		t.Fatalf("click 确认删除: %v", err)
	}
	tt.Frame()
	if state.form.busy != "deepseek" || state.form.deleteArmed != "" {
		t.Errorf("the card is busy=%q, armed=%q, want the removal under way",
			state.form.busy, state.form.deleteArmed)
	}
	// The write runs off the main thread, and the view has no window to hear
	// the answer through: wait for the store, then hand the answer in the way
	// the window would.
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, found := storedSecret(t, state)
		if !found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the confirmed removal never reached the store")
		}
		time.Sleep(time.Millisecond)
	}
	state.credentialForgotten("deepseek", nil)

	if state.form.busy != "" {
		t.Errorf("the card is still busy: %q", state.form.busy)
	}
	if state.form.hasCredential["deepseek"] {
		t.Error("the card still says it holds a credential")
	}
	if got := state.form.status["deepseek"]; got.status != statusOK || got.text != "已删除。" {
		t.Errorf("the card says %+v, want the removal reported", got)
	}
	// What is gone cannot be removed again.
	if tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight); exact(tt.Texts(), "删除") {
		t.Errorf("the card still offers to remove what is gone: %q", tt.Texts())
	}
}

// A removal that fails says what happened and leaves the card as it was, with
// the way to try again still on it.
func TestSettingsReportsAFailedRemoval(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredentials()
	state.form.busy = "deepseek"

	state.credentialForgotten("deepseek", errors.New("the file is read-only"))

	if state.form.busy != "" {
		t.Errorf("the card is still busy after the answer: %q", state.form.busy)
	}
	got, ok := state.form.status["deepseek"]
	if !ok || got.status != statusError {
		t.Fatalf("the card says %+v, want a failure", got)
	}
	if !contains([]string{got.text}, "read-only") {
		t.Errorf("the failure does not say what went wrong: %q", got.text)
	}
	if !state.form.hasCredential["deepseek"] {
		t.Error("a failed removal forgot the credential anyway")
	}
	if tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight); !exact(tt.Texts(), "删除") {
		t.Errorf("the card no longer offers to remove the credential: %q", tt.Texts())
	}
}

// storedSecret reads the one credential the tests store, as a test has to read
// it: the window never does, and the value only comes back as its last four
// characters.
func storedSecret(t *testing.T, state *State) (string, bool) {
	t.Helper()
	masked, found, err := state.secretService.Masked(context.Background(), "deepseek", "apiKey")
	if err != nil {
		t.Fatalf("read the stored credential: %v", err)
	}
	return masked, found
}
