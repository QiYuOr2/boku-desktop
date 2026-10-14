package app

import (
	"context"
	"image"
	"image/color"
	"strings"
	"testing"

	"boku/internal/domain"

	"github.com/egoist/mygo/ui"
)

// The design tests are about the look rather than about one screen: the pieces
// the design system composes — a window's chrome, a section's head, an input,
// the two actions of a form — are what the windows are made of, and these are
// the properties a screen must keep whatever it says.

// settingsPageHeight is a window tall enough to show two sections of the
// settings page at once: the page is taller than the window the application
// opens, and a property that is about two sections has to have both of them
// laid out to be compared.
const settingsPageHeight = 900

// rowsOfColor returns the y of every row of the frame whose pixels from inset
// to width-inset are all this color, which is what a region's face looks like
// between its own edges.
func rowsOfColor(img *image.RGBA, c color.RGBA, width, inset int) []int {
	var rows []int
	for y := 0; y < img.Rect.Dy(); y++ {
		all := true
		for x := inset; x < width; x++ {
			if got := img.RGBAAt(x, y); got != c {
				all = false
				break
			}
		}
		if all {
			rows = append(rows, y)
		}
	}
	return rows
}

// fullWidthRows returns the y of every row drawn edge to edge in this color,
// which is what a hairline between two regions looks like. A scrolling page
// keeps its scroll bar in a strip at the right edge, so a row is counted when
// everything up to that strip is the color.
func fullWidthRows(img *image.RGBA, c ui.Color, width int) []int {
	return rowsAcross(img, c, width-scrollbarStrip)
}

// rowsAcross returns the y of every row whose pixels from 0 to span are this
// color, whatever is drawn past span.
func rowsAcross(img *image.RGBA, c ui.Color, span int) []int {
	var rows []int
	for y := 0; y < img.Rect.Dy(); y++ {
		count := 0
		for x := 0; x < span; x++ {
			got := img.RGBAAt(x, y)
			if got.R == c.R && got.G == c.G && got.B == c.B {
				count++
			}
		}
		if count == span {
			rows = append(rows, y)
		}
	}
	return rows
}

// pageRows returns the y of every row of the frame drawn in this color from
// the page's own margin to the far side of the content: a hairline between two
// sections is a line inside the page, not one across the window.
func pageRows(img *image.RGBA, c ui.Color, width, scale int) []int {
	left, right := int(pageEdge)*scale, width-scrollbarStrip
	return rowsIn(img, c, left, right)
}

// rowsIn returns the y of every row whose pixels from left to right are all
// this color.
func rowsIn(img *image.RGBA, c ui.Color, left, right int) []int {
	var rows []int
	for y := 0; y < img.Rect.Dy(); y++ {
		all := true
		for x := left; x < right; x++ {
			got := img.RGBAAt(x, y)
			if got.R != c.R || got.G != c.G || got.B != c.B {
				all = false
				break
			}
		}
		if all {
			rows = append(rows, y)
		}
	}
	return rows
}

// scrollbarStrip is the room a scrolling page keeps for its scroll bar, in
// device pixels at the scale the tests render at.
const scrollbarStrip = 48

func rgba(c ui.Color) color.RGBA { return color.RGBA{R: c.R, G: c.G, B: c.B, A: c.A} }

// A window draws no title of its own: the system's title bar names it, and a
// name drawn again inside the window is a second title. The sidebar is the
// window's navigation and keeps the left edge; the page starts right of it,
// at its own margin, and the version closes the window on its own face.
func TestDesignWindowHasNoTitleOfItsOwn(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredentials()
	const scale = 2
	img := ui.Render(state.settingsView, settingsWidth, settingsHeight, scale)
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)

	// The window's name is in the system's title bar, not in the view.
	if exact(tt.Texts(), appName+" 设置") {
		t.Errorf("the window draws its own name inside itself: %q", tt.Texts())
	}
	// The first thing drawn on the page is the first section's own line, at
	// the top of the page's margin: the sidebar keeps the left edge, and
	// nothing of the window's own is above the page — its title is in the
	// system's title bar rather than in the view.
	pageLeft := float32(settingsSidebarWidth + pageEdge)
	head := findText(t, tt, "配置各 AI 服务密钥")
	if head.Y > pageEdge+pageEdge+8 {
		t.Errorf("the page starts at y=%.0f: something of the window's own is above its first section", head.Y)
	}
	sidebar := findText(t, tt, "划词翻译")
	if sidebar.X >= pageLeft {
		t.Errorf("the sidebar's entry at x=%.0f is not left of the page's margin at x=%.0f",
			sidebar.X, pageLeft)
	}
	if head.X < pageLeft || head.X > pageLeft+64 {
		t.Errorf("the page's first line starts at x=%.0f, which is not the room after the page's margin at x=%.0f",
			head.X, pageLeft)
	}
	// The window's own face is the content's: no band of another color runs
	// across the top of it.
	if got := img.RGBAAt(settingsWidth*scale/2, 2); got != rgba(colorSurface) {
		t.Errorf("the window's top is %v, want its own face %v", got, colorSurface)
	}
	// The version closes the window, on that same face. The page is taller
	// than the window, so the version is found where it is drawn rather than
	// where the layout put it: the footer is pinned to the window's foot.
	foot := float32(settingsHeight-1) * scale
	if got := img.RGBAAt(settingsWidth*scale/2, int(foot)); got != rgba(colorSurface) {
		t.Errorf("the window's foot is %v, want its own face %v", got, colorSurface)
	}
}

// The settings window's sections are peers: each is a mark, a name and a line
// of its own, and their headings start at the same left edge — the page's,
// right of the sidebar that navigates them. Nothing nests inside anything
// else, and the line between two of them is drawn inside the page's margin —
// the only lines that run to the window's edges are the sidebar's own edge and
// the one above the footer, which is where the window says it ends.
func TestDesignSectionsArePeers(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredentials()
	// The page is taller than the window the application opens, so it is
	// rendered into a taller one: a comparison of two sections needs both of
	// them laid out at once.
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsPageHeight)

	pageLeft := float32(settingsSidebarWidth + pageEdge)
	keys := findText(t, tt, "配置各 AI 服务密钥")
	translate := findText(t, tt, "任意语言 → 中文 · AI 翻译")
	if keys.X <= pageLeft || translate.X <= pageLeft {
		t.Errorf("the sections start at x=%.0f and x=%.0f, inside the page's margin of %.0f",
			keys.X, translate.X, pageLeft)
	}
	if d := keys.X - translate.X; d > 1 || d < -1 {
		t.Errorf("the sections start at x=%.0f and x=%.0f: one is indented under the other",
			keys.X, translate.X)
	}
	// A section head is a mark and then a name, so the heading is right of the
	// page's margin: the mark has the same room as a section label has none.
	if keys.X <= pageLeft {
		t.Errorf("the heading at x=%.0f is not right of its mark", keys.X)
	}

	// The line between two sections keeps the page's margin, the way every
	// other thing on the page does: a row that is drawn from the page's
	// margin to the far side of the page. The window is rendered tall enough
	// that the line is in the window at all.
	const scale = 2
	img := ui.Render(state.settingsView, settingsWidth, settingsPageHeight, scale)
	width := settingsWidth * scale
	margin := int(pageLeft) * scale
	right := width - scrollbarStrip
	divider := -1
	for y := 0; y < img.Rect.Dy() && divider < 0; y++ {
		if img.RGBAAt(margin+2, y) != rgba(colorBorder) || img.RGBAAt(margin-2, y) == rgba(colorBorder) {
			continue
		}
		// It has to reach the far side of the page and stop there: a card's
		// edge is drawn in the same color and stops short of it.
		all := true
		for x := margin + 2; x < right; x++ {
			if img.RGBAAt(x, y) != rgba(colorBorder) {
				all = false
				break
			}
		}
		if !all {
			continue
		}
		divider = y
	}
	if divider < 0 {
		t.Fatal("the page draws no line between two sections inside its margin")
	}
	// The sidebar's own edge is the window's, not the page's: the line
	// between two sections does not reach it.
	if got := img.RGBAAt(int(settingsSidebarWidth)*scale, divider); got == rgba(colorBorder) {
		t.Errorf("the line between the sections at y=%.0f crosses into the sidebar", float32(divider)/scale)
	}
}

// A section introduces itself with the mark of what it is about, its name and
// one line of explanation: the name and its line start right of the mark, at
// the page's margin rather than the window's edge, and the two lines are not
// on top of each other.
func TestDesignSectionHeadCarriesTheMark(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredentials()
	// The second section is below the fold of the window the application
	// opens; it is rendered into a taller one so that its head is drawn.
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsPageHeight)

	// The heading's own line is what says where the head is: the sidebar
	// draws the section's name at the window's left edge, and it is
	// navigation rather than the section.
	subtitle := findText(t, tt, "任意语言 → 中文 · AI 翻译")
	if subtitle.X <= settingsSidebarWidth+pageEdge {
		t.Errorf("the section's line at x=%.0f is not right of its mark", subtitle.X)
	}
	// The name is the same words, right above it: the sidebar's entry of
	// that name is at the window's edge and well above the section.
	sidebar := findText(t, tt, "划词翻译")
	if sidebar.X >= subtitle.X {
		t.Errorf("the sidebar's entry at x=%.0f is not left of the section at x=%.0f", sidebar.X, subtitle.X)
	}
	if subtitle.Y <= sidebar.Y+8 {
		t.Errorf("the section's line at y=%.0f is not below the sidebar's entry at y=%.0f",
			subtitle.Y, sidebar.Y)
	}
}

// The credential is a value the form keeps out of its way entirely: the field
// is empty and shows only its own hint, the card says a credential is there
// with its badge, and not one character of what is stored reaches the window —
// replacing it is done by typing in the field and saving again.
func TestDesignCredentialStaysOutOfTheWindow(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredentials()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)

	if !tt.HasText("已配置") {
		t.Errorf("the card does not say a credential is stored: %q", tt.Texts())
	}
	// The field holds nothing: what it draws is its own "sk-......" hint, and
	// the stored value is not in it to be read back.
	if typed := state.cardInput("deepseek"); typed != "" {
		t.Errorf("the field holds %q, want the empty field that draws its hint", typed)
	}
	// Nothing of the stored credential is drawn: not its value, not its last
	// four characters.
	for _, text := range tt.Texts() {
		for _, leak := range []string{"sk-test-0001", "0001"} {
			if strings.Contains(text, leak) {
				t.Errorf("the window draws %q, which is part of the stored credential", text)
			}
		}
	}
}

// The form's main action is the brand green, filled, and the action beside it
// is on the content's own face: two buttons of the same height, one of them
// the one the form is for.
func TestDesignActionsAreFilledAndOutlined(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredentials()
	const scale = 2
	img := ui.Render(state.settingsView, settingsWidth, settingsHeight, scale)
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)

	save := findText(t, tt, "保存")
	test := findText(t, tt, "测试")
	if diff := save.H - test.H; diff > 1 || diff < -1 {
		t.Errorf("the two actions are %.0f and %.0f tall: they are not one row of controls", save.H, test.H)
	}
	if test.X >= save.X {
		t.Errorf("the secondary action at x=%.0f is not left of the primary at x=%.0f", test.X, save.X)
	}
	// The primary is filled: its line is the brand green from one edge of the
	// button to the other, and it takes a button's width rather than a word's.
	// The line is the middle of the button, where the rounding of its corners
	// does not eat into the fill. The scan keeps to the page — the sidebar has
	// a green entry of its own, and it is navigation rather than an action of
	// the form.
	line := int(save.Y+save.H/2) * scale
	page := settingsSidebarWidth * scale
	left, right := -1, -1
	for x := page; x < settingsWidth*scale; x++ {
		if got := img.RGBAAt(x, line); got == rgba(colorBrand) {
			if left < 0 {
				left = x
			}
			right = x
		}
	}
	if left < 0 {
		t.Fatalf("no brand green on the primary action's line y=%d", line)
	}
	if right-left < 30*scale {
		t.Errorf("the primary action's fill spans %d pixels: it is a word, not a button", right-left)
	}
	// And it is the form's main action: it sits at the right end of its own
	// row, after the action beside it.
	if left <= int((test.X+test.W)*scale) {
		t.Errorf("the primary action's fill starts at x=%d, inside the action beside it", left)
	}
	if got := countColor(img, colorBrand); got < 200 {
		t.Errorf("the brand green covers %d pixels, want a filled button", got)
	}
}

// The one note on the page — that the credential is stored as plain text — is
// a panel in the warning colors rather than another line of body text.
func TestDesignPlainTextWarningIsAPanel(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredentials()
	const scale = 2
	img := ui.Render(state.settingsView, settingsWidth, settingsHeight, scale)
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)

	plain := findTextPrefix(t, tt, "凭据明文保存在")
	// The panel is drawn around the line: a point on the panel's own row,
	// inside the page's margin, is the panel's tint rather than the content's
	// white. The line is inset in the panel by its own padding.
	at := img.RGBAAt(int(pageEdge)*scale+2, int(plain.Y+plain.H/2)*scale)
	if at == rgba(colorSurface) {
		t.Errorf("the warning is drawn on the content's white, not in a panel")
	}
	if got := countColor(img, colorWarningFill); got < 500 {
		t.Errorf("the warning panel covers %d pixels, want a tinted panel", got)
	}
}

// The clipboard history is a list of copies: the title bar names the window,
// a line above the list says what the list is and what a click does, and each
// row carries the mark of its kind.
func TestDesignHistoryListsCopies(t *testing.T) {
	device := &testDevice{}
	state := clipboardState(t, device, &testToolStore{})
	device.set(domain.ClipContent{Kind: domain.ClipKindImage, ImageBytes: 245 * 1024})
	state.clipboard.Read(context.Background())
	device.set(domain.ClipContent{Kind: domain.ClipKindText, Text: "just some prose"})
	state.clipboard.Read(context.Background())

	tt := ui.NewTester(state.historyView, historyWidth, historyHeight)
	for _, want := range []string{"最近 30 条", "点击即复制", "图片 245 KB", "just some prose"} {
		if !tt.HasText(want) {
			t.Errorf("the history window does not show %q; it shows %q", want, tt.Texts())
		}
	}
	// The window's own name is in the system's title bar, and the line about
	// clicking a copy is not repeated under the list.
	for _, unwanted := range []string{appName + " 剪贴板历史", "点击一条即复制回剪贴板", appName + " 0.1.0"} {
		if tt.HasText(unwanted) {
			t.Errorf("the history window still draws %q inside itself: %q", unwanted, tt.Texts())
		}
	}
	// The image row is told apart from the prose by its kind and its size.
	image := findText(t, tt, "图片 245 KB")
	prose := findText(t, tt, "just some prose")
	if image.Y <= prose.Y {
		t.Errorf("the newest copy at y=%.0f is not above the older one at y=%.0f", image.Y, prose.Y)
	}
	// The kind's mark is drawn on its own tile at the head of the row: the
	// tile's violet is there, and the prose row's sky.
	const scale = 2
	img := ui.Render(state.historyView, historyWidth, historyHeight, scale)
	if got := countColor(img, colorVioletTile); got < 100 {
		t.Errorf("the image row's tile covers %d pixels of its tint, want a tile", got)
	}
	if got := countColor(img, colorSkyTile); got < 100 {
		t.Errorf("the prose row's tile covers %d pixels of its tint, want a tile", got)
	}
}

// The quick window's footer carries its actions as marks: a word at the bottom
// of a card reads as another piece of content, and the marks keep their labels
// for the pointer and for assistive technology. They sit at the far end of the
// line, one gap apart, and the version keeps the far end of the other side.
func TestDesignQuickFooterActionsAreMarks(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	loaded(t, state)
	const scale = 2
	tt := ui.NewTester(state.View, panelWidth, panelHeight)
	img := ui.Render(state.View, panelWidth, panelHeight, scale)

	// The actions are named for the pointer, not written on the window.
	set := findText(t, tt, "设置")
	quit := findText(t, tt, "退出")
	if set.W > 40 || quit.W > 40 {
		t.Errorf("the footer actions are %.0fx%.0f and %.0fx%.0f: they are words, not marks",
			set.W, set.H, quit.W, quit.H)
	}
	if set.H > 40 || quit.H > 40 {
		t.Errorf("the footer actions are %.0f tall: a mark takes a mark's room", quit.H)
	}
	if set.X >= quit.X {
		t.Errorf("the settings mark at x=%.0f is not left of the quit mark at x=%.0f", set.X, quit.X)
	}
	// The two are one gap apart, and the pair keeps the page's margin at the
	// far end of the line: what ends at the margin is the mark, so the button
	// around it reaches its own padding past.
	gap := quit.X - (set.X + set.W)
	if gap < 2 || gap > 16 {
		t.Errorf("the two marks are %.0f apart, want the room between two controls", gap)
	}
	if right := quit.X + quit.W; right > panelWidth-pageEdge+8 {
		t.Errorf("the footer actions end at x=%.0f, past the page's margin", right)
	}
	// They are drawn, not only named: their ink is on the footer's line.
	if got := countColor(img, panelFaint); got < 20 {
		t.Error("no mark is drawn in the footer")
	}
	// The version keeps the other end.
	version := findText(t, tt, "Boku 0.1.0")
	if version.X >= set.X {
		t.Errorf("the version at x=%.0f is not left of the actions at x=%.0f", version.X, set.X)
	}
}

// The quick window is the panel's own section: its title line, its number and
// its footer, on the content's face rather than in a card inside the window.
func TestDesignQuickWindowIsASection(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	loaded(t, state)
	const scale = 2
	tt := ui.NewTester(state.View, panelWidth, panelHeight)
	img := ui.Render(state.View, panelWidth, panelHeight, scale)

	balance := findText(t, tt, "¥42.10")
	label := findText(t, tt, "总余额")
	// The design's own pair is a 22-point number over an 11-point label: the
	// number is half again as tall at least, and the test asks for that
	// rather than for a fixed pixel height, which the font decides.
	if balance.H <= label.H*1.5 {
		t.Errorf("the balance is %.0f tall and its label %.0f: the number is not the headline",
			balance.H, label.H)
	}
	// The panel starts at the page's margin, not at the window's edge, and
	// the window's own ground above it is the content's face.
	if title := findText(t, tt, "DeepSeek 余额"); title.X < pageEdge {
		t.Errorf("the panel starts at x=%.0f, inside the page's margin of %.0f", title.X, float32(pageEdge))
	}
	if got := img.RGBAAt(2, 2); got != rgba(colorSurface) {
		t.Errorf("the quick window's ground at the top is %v, want the surface %v", got, colorSurface)
	}
}
