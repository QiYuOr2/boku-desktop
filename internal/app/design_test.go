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
// name drawn again inside the window is a second title. The content starts at
// the page's margin, and the version closes the window on its own face.
func TestDesignWindowHasNoTitleOfItsOwn(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredential()
	const scale = 2
	img := ui.Render(state.settingsView, settingsWidth, settingsHeight, scale)
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)

	// The window's name is in the system's title bar, not in the view.
	if exact(tt.Texts(), appName+" 设置") {
		t.Errorf("the window draws its own name inside itself: %q", tt.Texts())
	}
	// The first thing drawn is the section's head, at the page's margin.
	head := findText(t, tt, "DeepSeek")
	if head.Y > pageEdge+4 {
		t.Errorf("the content starts at y=%.0f: something is above it", head.Y)
	}
	if head.X < pageEdge {
		t.Errorf("the section's head starts at x=%.0f, inside the page's margin", head.X)
	}
	// The window's own face is the content's: no band of another color runs
	// across the top of it.
	if got := img.RGBAAt(settingsWidth*scale/2, 2); got != rgba(colorSurface) {
		t.Errorf("the window's top is %v, want its own face %v", got, colorSurface)
	}
	// The version closes the window, on that same face.
	version := findText(t, tt, "Boku 0.1.0")
	if got := img.RGBAAt(settingsWidth*scale/2, int(version.Y+version.H/2)*scale); got != rgba(colorSurface) {
		t.Errorf("the version is drawn on %v, want the window's own face", got)
	}
}

// The settings window's sections are peers: each is a mark, a name and a line
// of its own, and the two names start at the same left edge. Nothing nests
// inside anything else — the data source and the clipboard are two of the same
// thing — and one hairline divides them, drawn inside the page's margin: the
// line between two sections belongs to the page, not to the window.
func TestDesignSectionsArePeers(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredential()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)

	dataSource := findText(t, tt, "DeepSeek")
	clipboard := findText(t, tt, "剪贴板")
	if clipboard.Y <= dataSource.Y {
		t.Fatalf("the clipboard section at y=%.0f is not under the data source at y=%.0f", clipboard.Y, dataSource.Y)
	}
	// The two heads start at the same x, which is what makes them peers: a
	// section nested inside another would be indented under the outer one's
	// head, and the two names would not line up. Both are right of the page's
	// margin, because a head is a mark and then a name.
	if dataSource.X <= pageEdge || clipboard.X <= pageEdge {
		t.Errorf("the sections start at x=%.0f and x=%.0f, inside the page's margin of %.0f",
			dataSource.X, clipboard.X, float32(pageEdge))
	}
	if d := dataSource.X - clipboard.X; d > 1 || d < -1 {
		t.Errorf("the sections start at x=%.0f and x=%.0f: one is indented under the other",
			dataSource.X, clipboard.X)
	}

	// One hairline divides them, it falls between the two heads, and it stays
	// inside the page's margin: the window's own line is the one above its
	// footer, and this is not it.
	const scale = 2
	img := ui.Render(state.settingsView, settingsWidth, settingsHeight, scale)
	lines := 0
	previous := float32(-100)
	for _, y := range pageRows(img, colorBorder, settingsWidth*scale, scale) {
		at := float32(y) / scale
		if at <= dataSource.Y+dataSource.H || at >= clipboard.Y {
			continue
		}
		if img.RGBAAt(0, y) == rgba(colorBorder) || img.RGBAAt(settingsWidth*scale-1, y) == rgba(colorBorder) {
			t.Errorf("the line at y=%.0f between the sections runs to the window's edge", at)
		}
		if at-previous > 1 {
			lines++
		}
		previous = at
	}
	if lines != 1 {
		t.Errorf("%d hairlines divide the two sections, want exactly one", lines)
	}

	// The provider is named once: the section's head says whose credential
	// it wants, and nothing under it says it again.
	named := 0
	for _, text := range tt.Texts() {
		if strings.Contains(text, "DeepSeek") {
			named++
		}
	}
	if named != 1 {
		t.Errorf("the page names the provider %d times, want once: %q", named, tt.Texts())
	}
}

// A section introduces itself with the mark of what it is about, its name and
// one line of explanation. A provider's mark is drawn in the provider's own
// color: it is the logo the user knows, and it is not replaced or recolored.
func TestDesignSectionHeadCarriesTheMark(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredential()
	const scale = 2
	img := ui.Render(state.settingsView, settingsWidth, settingsHeight, scale)
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)

	title := findText(t, tt, "DeepSeek")
	subtitle := findText(t, tt, "API Key 配置")
	if subtitle.Y <= title.Y {
		t.Errorf("the head's line at y=%.0f is not under its name at y=%.0f", subtitle.Y, title.Y)
	}
	if title.X <= pageEdge {
		t.Errorf("the section's name at x=%.0f is not right of its mark", title.X)
	}
	// The mark is drawn in the provider's own color: it is the logo the user
	// knows, and it is not replaced.
	if got := countColor(img, deepseekBrandColor); got < 200 {
		t.Errorf("the DeepSeek mark covers %d pixels, want the logo drawn in its own color", got)
	}
}

// The credential is a value the form keeps out of its way: the field is empty
// and shows only its own hint, and what is stored is a dot and a word in the
// success color, on a line of its own — nothing beside it to press, because
// typing over the field is what replaces it.
func TestDesignCredentialIsShownAsAStatusLine(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredential()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)

	if !tt.HasText("已配置：…0001") {
		t.Errorf("the stored credential is not shown masked: %q", tt.Texts())
	}
	// The field holds nothing: what it draws is its own "sk-......" hint, and
	// the stored value is not in it to be read back.
	if state.form.apiKey != "" {
		t.Errorf("the field holds %q, want the empty field that draws its hint", state.form.apiKey)
	}
	// The line is the whole of what the window says about it: the actions
	// that used to sit at its end are gone, and replacing what is stored is
	// done by typing in the field.
	status := findText(t, tt, "已配置：…0001")
	if status.W <= 0 || status.X <= pageEdge {
		t.Errorf("the status line at x=%.0f, %.0f wide is not inside the page", status.X, status.W)
	}
}

// The form's main action is the brand green, filled, and the action beside it
// is on the content's own face: two buttons of the same height, one of them
// the one the form is for.
func TestDesignActionsAreFilledAndOutlined(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredential()
	const scale = 2
	img := ui.Render(state.settingsView, settingsWidth, settingsHeight, scale)
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)

	save := findText(t, tt, "保存")
	test := findText(t, tt, "测试连接")
	if diff := save.H - test.H; diff > 1 || diff < -1 {
		t.Errorf("the two actions are %.0f and %.0f tall: they are not one row of controls", save.H, test.H)
	}
	if test.X >= save.X {
		t.Errorf("the secondary action at x=%.0f is not left of the primary at x=%.0f", test.X, save.X)
	}
	// The primary is filled: its line is the brand green from one edge of the
	// button to the other, and it takes a button's width rather than a word's.
	line := int(save.Y)*scale + 2
	left, right := -1, -1
	for x := 0; x < settingsWidth*scale; x++ {
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
	if right-left < 40*scale {
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
	state.loadCredential()
	const scale = 2
	img := ui.Render(state.settingsView, settingsWidth, settingsHeight, scale)
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)

	plain := findTextPrefix(t, tt, "存放在")
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
	if got := countColor(img, colorTextMuted); got < 20 {
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
	if balance.H <= label.H*2 {
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
