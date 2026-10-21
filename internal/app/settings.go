package app

import (
	"boku/internal/presentation"

	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	"boku/internal/kit"

	preferencesui "boku/internal/preferences/ui"
)

// The settings window is a sidebar and the page it navigates: the sidebar
// names every chapter of the page, and the page is the sections themselves,
// one under the other. The sections are still peers — the sidebar does not
// nest them — and the line between two of them still says where one ends.
//
// The window is created when it is first asked for and closed for real
// afterwards: a tray application should cost nothing while nobody is
// configuring it.
const (
	// settingsWidth is the page plus the room the sidebar keeps. The page is
	// the width it has always been (560): a column of words beside it must
	// not narrow the forms underneath, which are the thing being configured.
	settingsSidebarWidth = 132
	settingsPageWidth    = 560
	settingsWidth        = settingsPageWidth + settingsSidebarWidth
	// settingsAnchor is the room the page keeps above the section the
	// sidebar was just asked for: it is what tells the user that the section
	// begins there rather than at the window's edge. The design's own
	// scrollToSection keeps 16 above the target.
	settingsAnchor = 16
	// settingsHeight is the room the page wants: five sections and the
	// footer. A page with more on it scrolls rather than growing the window
	// past the screen.
	settingsHeight = 660
)

// settingsSectionKind names one chapter of the settings page: what the
// sidebar says and what the page shows.
type settingsSectionKind int

const (
	sectionAPIKey settingsSectionKind = iota
	sectionTranslate
	sectionClipboard
	sectionAnime
	sectionBilibili
	sectionQuickLayout
	sectionGeneral
	sectionAbout
)

// settingsPage is one section of the settings page and the sidebar entry that
// reaches it: the entry's name, the mark beside it, and what the section
// draws.
//
// The two are one thing rather than two lists kept in step by hand: a section
// added here is in the sidebar and on the page, and an entry can therefore
// never point at a section that is not there. The entry's name is the
// section's own, which is what tells the user where it goes.
type settingsPage struct {
	kind  settingsSectionKind
	name  string
	mark  *ui.SVG
	build func(c *ui.Context, head *ui.Handle)
}

// settingsPages is the settings page as the sidebar and the page both read
// it, in the order they show it.
func (s *State) settingsPages() []*settingsPage {
	return []*settingsPage{
		{kind: sectionAPIKey, name: "API Key", mark: s.keyMark, build: s.credentialUI.ApiKeySection},
		{kind: sectionTranslate, name: "划词翻译", mark: s.translationMark, build: s.translateSection},
		{kind: sectionClipboard, name: "剪贴板", mark: s.clipboardMark, build: s.clipboardSection},
		{kind: sectionAnime, name: "动画", mark: s.tvMark, build: s.animeSection},
		{kind: sectionBilibili, name: "B站", mark: s.bilibiliMark, build: s.bilibiliSection},
		{kind: sectionQuickLayout, name: "面板布局", mark: s.layoutMark, build: s.layoutUI.SettingsView},
		{kind: sectionGeneral, name: "通用", mark: s.settingsMark, build: s.generalSection},
		{kind: sectionAbout, name: "关于", mark: s.infoMark, build: s.aboutSection},
	}
}

func (s *State) bilibiliSection(c *ui.Context, head *ui.Handle) {
	s.bilibiliUI.SettingsView(c, head, s.readingDesign(s.bilibiliMark))
}

func (s *State) animeSection(c *ui.Context, head *ui.Handle) {
	s.animeUI.SettingsView(c, head, s.readingDesign(s.tvMark))
}

// openSettings shows the settings window, creating it the first time.
func (s *State) openSettings() {
	s.syncActivationPolicy()
	s.RefreshSelectionAccess()
	if s.settingsWindow != nil {
		s.settingsWindow.Show()
		s.settingsWindow.Focus()
		return
	}
	s.credentialUI.LoadCredentials()
	window := mygo.NewWindow(mygo.WindowOptions{
		Title:     appName + " 设置",
		Width:     settingsWidth,
		Height:    settingsHeight,
		MinWidth:  settingsWidth,
		MinHeight: 420,
		Content:   ui.View(s.settingsView),
	})
	s.settingsWindow = window
	s.syncActivationPolicy()
	window.OnFocus(func() { s.RefreshSelectionAccess() })
	window.OnClosed(func() {
		if s.settingsWindow == window {
			s.settingsWindow = nil
			s.syncActivationPolicy()
		}
	})
}

// settingsView builds the settings window: the sidebar that names every
// section, the page those sections are on, and the version at its foot.
//
// The window draws no title bar of its own: the system's title bar already
// names it, and repeating that name inside the window is a second title.
// The sidebar is not a second title either: it says where each section is,
// and what is drawn at the top of the page is the section itself.
func (s *State) settingsView(c *ui.Context) {
	s.useDesign(c)
	presentation.WindowFrame(c).Children(func() {
		ui.Row(c).FillWidth().Grow(1).AlignItems(ui.Stretch).Children(func() {
			s.settingsSidebar(c)
			ui.Column(c).Fill().Grow(1).Children(func() {
				// The page takes the room that is left of the column and
				// scrolls inside it: a scroll container sized by its content
				// has nothing to scroll.
				body := ui.Scroll(c).FillWidth().Grow(1).TrackScroll(&s.settingsScroll)
				body.Children(func() {
					presentation.Content(c, false, func() {
						s.settingsPageContent(c, body)
					})
				})
			})
		})
		presentation.Footer(c, s.versionLine())
	})
	s.updateCheckDialogView(c)
}

// settingsPageContent draws the sections, one under the other, and then puts
// the page where the sidebar last asked for it.
//
// The walk is the sidebar's own list, so the page and the sidebar can only
// agree: the sections are drawn in the order the entries are, and the boxes
// of the sections and of their headings are what the sidebar reads.
func (s *State) settingsPageContent(c *ui.Context, body ui.Element) {
	pages := s.settingsPages()
	sections := make([]settingsSectionBox, 0, len(pages))
	for index, page := range pages {
		if index > 0 {
			presentation.BetweenSections(c)
		}
		// Each section is one box, so that the arithmetic has the box's own
		// top to work from: MyGo gives an element the box the frame before it
		// laid out, which is the first one a click can act on. Its head is a
		// box of its own inside it: what the sidebar follows is where the
		// heading is, not where the section begins.
		//
		// What is kept of the two is a handle rather than the element: an
		// element belongs to the frame that built it, and a handle is how a
		// piece of a view is found again in the next one.
		section := ui.Box(c).FillWidth()
		record := settingsSectionBox{kind: page.kind}
		section.Bind(&record.box)
		build := page.build
		section.Children(func() { build(c, &record.head) })
		sections = append(sections, record)
	}
	s.settingsScrollTo(c, sections, body)
}

// settingsSectionBox is one section as the sidebar reads it: what it is, and
// the handles to the box it is drawn in and to the box its heading is drawn
// in. Handles rather than elements: an element is the frame that built it, and
// the sidebar reads these again in every frame.
type settingsSectionBox struct {
	kind settingsSectionKind
	box  ui.Handle
	head ui.Handle
}

// settingsScrollTo puts the page on its way to the section the sidebar asked
// for, and keeps that entry open until the page has arrived.
//
// The page travels rather than jumps: the offset is handed to the scroll
// container as a ScrollTo, so MyGo glides to it frame by frame, as the
// design's own scroll-behavior does.
func (s *State) settingsScrollTo(c *ui.Context, sections []settingsSectionBox, body ui.Element) {
	s.settingsRoom = body.Bounds().H
	s.settingsSection = s.sectionInView(c, sections, body)
	// The sidebar's own request, if there is one: a section's box is where it
	// is drawn with the page where it is, so moving the page down by that much
	// puts the section at the anchor. MyGo gives an element the box of the
	// frame before, which is why this runs in the frame after the click and
	// not in the frame of it.
	if pending := s.settingsReveal; pending != nil {
		// The entry the user clicked is the open one for as long as the page
		// is moving: where the page is on the way says nothing about where it
		// is going, and lighting the sections it passes is what the user sees
		// as a flicker.
		s.settingsSection = *pending
		s.settingsAim(c, *pending, sections)
		if !s.settingsScroll.Scrolling() {
			// The page has arrived when the scroll it was given has stopped.
			// The entry is left to the scroll from the frame after — the one
			// whose boxes are laid out where the page now is — so that the
			// entry and the section never disagree.
			s.settingsReveal, s.settingsAiming, s.settingsAimAt = nil, false, 0
		}
	}
}

// settingsAim points the page at a section, or at the section clicked after
// it: a second click while the first one is still travelling retargets the
// scroll from where the page is now, which is what makes the last entry
// clicked the one that wins.
//
// A section's box moves with the page it is drawn on, so where the page is
// going is worked out once per destination and then left alone: re-aiming
// every frame would chase a target running away from the scroll. A section
// the page has no room to bring to the anchor — the ones at the end of a page
// never reach its top — leaves the page resting at its end, which the
// container does for itself.
//
// The box is where the section is drawn at the offset the frame before laid
// the page out at, and the target is an offset: the two are added with the
// page where it is now, which is the offset the box was measured at.
func (s *State) settingsAim(c *ui.Context, kind settingsSectionKind, sections []settingsSectionBox) {
	if s.settingsAiming && s.settingsAimAt == kind {
		return
	}
	index := int(kind)
	if index >= len(sections) {
		return
	}
	box := c.Resolve(sections[index].box)
	if box.Bounds().H <= 0 {
		return
	}
	s.settingsAiming, s.settingsAimAt = true, kind
	s.settingsScroll.ScrollTo(0, s.settingsScroll.Y+box.Bounds().Y-settingsAnchor, ui.Smooth)
}

// sectionInView is the section the sidebar marks open: the last one whose
// heading has crossed the middle of the page. A reader is looking at a
// section once its heading is in the upper half of the page, so the entry
// changes when the next heading comes up past the middle of it rather than
// only when it reaches the top.
//
// The comparison is on the screen: a box is where the element is drawn, and
// the middle of the page is the middle of the room the page scrolls in.
//
// One exception at the end of the page: the last sections are never brought
// to the middle — there is nothing below them to scroll — so the page resting
// at its end opens the last section whose heading it managed to show. Without
// it, the entries at the foot of the sidebar could never be lit at all.
func (s *State) sectionInView(c *ui.Context, sections []settingsSectionBox, body ui.Element) settingsSectionKind {
	active := settingsSectionKind(0)
	if len(sections) == 0 {
		return active
	}
	middle := body.Bounds().Y + body.Bounds().H/2
	atEnd := s.settingsScroll.Y >= s.settingsScroll.MaxY-1
	last := len(sections) - 1
	for _, section := range sections {
		// A section the frame before this one did not lay out has no box to
		// read, and nothing may be concluded from a zero: it is not a heading
		// at the top of the page, it is one that has not been drawn yet.
		head := c.Resolve(section.head)
		if head.Bounds().H <= 0 {
			continue
		}
		if head.Bounds().Y > middle {
			break
		}
		active = section.kind
	}
	if atEnd && active != sections[last].kind {
		if head := c.Resolve(sections[last].head); head.Bounds().Y <= body.Bounds().H {
			active = sections[last].kind
		}
	}
	return active
}

// settingsSidebar is the column of entries down the left of the window: one
// per section of the page, with the one the page is showing filled in the
// brand green.
func (s *State) settingsSidebar(c *ui.Context) {
	active := s.settingsSection
	ui.Column(c).
		Width(float32(settingsSidebarWidth)).
		Shrink(0).
		Background(presentation.ColorSidebar).
		// The sidebar's own edge: the line is only on the side that faces the
		// page, because that is the side where the two regions meet.
		BorderWidth(0, 0, 0, 1).
		BorderColor(presentation.ColorBorder).
		Padding(float32(12), float32(8)).
		Gap(float32(2)).
		Children(func() {
			for _, page := range s.settingsPages() {
				if s.settingsNav(c, page, page.kind == active) {
					s.openSettingsSection(page.kind)
				}
			}
		})
}

// settingsNav is one entry of the sidebar: the section's mark and its name,
// filled in the brand green while the page is showing that section.
//
// The whole entry is the target rather than the words in it: a row that is
// about one place should answer anywhere on it. The green is the same green
// the picked segment of a form wears, because it means the same thing.
func (s *State) settingsNav(c *ui.Context, page *settingsPage, open bool) bool {
	row := ui.ButtonBase(c).Label(page.name).
		FillWidth().
		Focusable().
		Role(ui.RoleTab).
		Selected(open).
		Cursor(ui.CursorPointer).
		Padding(float32(6), float32(8)).
		Radius(presentation.RadiusField).
		Justify(ui.Start).
		AlignItems(ui.Center).
		Gap(float32(8))
	ink := presentation.ColorTextBody
	switch {
	case open:
		row.Background(presentation.ColorBrand)
		ink = presentation.ColorOnBrand
	case row.Hovered() || row.Focused():
		row.Background(presentation.ColorHover)
	}
	if !open && row.Pressed() {
		row.Background(presentation.ColorActive)
	}
	icon := presentation.ColorTextMuted
	if open {
		icon = presentation.ColorOnBrand
	}
	row.Children(func() {
		if page.mark != nil {
			ui.Icon(c, page.mark).Size(float32(16), float32(16)).Shrink(0).TextColor(icon)
		}
		ui.Text(c, page.name).
			FontSize(presentation.SizeSmall).
			FontWeight(500).
			TextColor(ink).
			SingleLine()
	})
	return row.Clicked()
}

// openSettingsSection answers a click on the sidebar: the page is taken to
// that section, and the entry is marked open from this frame rather than from
// where the scroll has reached.
//
// Both halves matter. Reading the scroll alone would mark the entry the page
// has not left yet while it travels; and the click has to leave the section
// behind, because the travel is worked out in the next frame, once the box of
// the section it is going to exists.
func (s *State) openSettingsSection(kind settingsSectionKind) {
	pages := s.settingsPages()
	for index, page := range pages {
		if page.kind != kind {
			continue
		}
		s.settingsSection = kind
		s.settingsReveal = &pages[index].kind
		break
	}
	// The click was answered in this frame, and the page moves in the next
	// one: a frame is what carries the request to the scroll container.
	s.Refresh()
}

// clipboardSection is the clipboard tool: the two switches, the record range
// and the way to the history.
//
// Both switches start off, and the section says why: the clipboard is the most
// sensitive thing Boku can read. Turning monitoring off stops the reading
// rather than hiding it, and the form says that too, because "off" that
// still reads would be a lie.
func (s *State) clipboardSection(c *ui.Context, head *ui.Handle) {
	s.clipboardUI.SettingsView(c, head, s.clipboardDesign())
}

// generalSection is the application's own behavior: what it does when it
// starts, where its data is, and what the platform starts for the user.
//
// The last of those is the "开机自动启动" switch, which is a fact about the
// machine rather than a preference Boku stores: the section asks the platform
// every frame (autostart.go), so a start the user disabled somewhere else
// shows as disabled here. The switch is drawn only where the platform can
// register one; failures remain visible below it.
func (s *State) generalSection(c *ui.Context, head *ui.Handle) {
	s.preferencesUI.GeneralView(c, head, preferencesui.GeneralProps{
		Mark: s.settingsMark, Appearance: s.appearance.Mode(), SetAppearance: s.setAppearance,
		AppearanceStatus: s.appearanceStatusLine(), Panels: s.panelUI.SettingsView, Status: s.panelStatus,
		DataPath: s.dataPath(), RevealDataPath: s.revealDataPath,
	})
}

// aboutSection is what the application is: the version, what it was built
// with, where its files are, and a button to check whether there is a newer
// version.
func (s *State) aboutSection(c *ui.Context, head *ui.Handle) {
	presentation.Inset(c, func() {
		presentation.Stacked(c, float32(16), func() {
			presentation.SectionHead(c, head, s.infoMark, presentation.ColorStoneInk, "关于", "版本与构建")

			ui.Column(c).FillWidth().
				Background(presentation.ColorField).
				Border(1, presentation.ColorBorder).
				Radius(presentation.RadiusField).
				Padding(float32(12)).
				Children(func() {
					presentation.Stacked(c, float32(6), func() {
						aboutLine(c, "版本", s.versionLine())
						aboutLine(c, "平台", runtime.GOOS+" / "+runtime.GOARCH)
						aboutLine(c, "构建", runtime.Version())
						aboutLine(c, "凭据", s.credentialUI.CredentialPath())
					})
				})

			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
				if button := presentation.DesignButton(c, "复制版本信息", presentation.ButtonSecondary); button.Clicked() {
					s.copyVersionInfo()
				}
				s.updateCheckButton(c)
				ui.Spacer(c)
			})
		})
	})
}

// aboutLine is one row of the about box: what it is on the left, what it is
// on the right, both on the field's own face.
func aboutLine(c *ui.Context, label, value string) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
		ui.Text(c, label).FontSize(presentation.SizeSmall).TextColor(presentation.ColorTextMuted).SingleLine().Shrink(0)
		ui.Spacer(c)
		ui.Text(c, value).
			FontSize(presentation.SizeSmall).
			TextColor(presentation.ColorText).
			Font("monospace").
			SingleLine()
	})
}

// dataPath is where the application keeps its files, or a line saying that
// it does not have one.
func (s *State) dataPath() string {
	path, err := mygo.App.Path(mygo.PathUserData)
	if err != nil || strings.TrimSpace(path) == "" {
		return "本机用户目录"
	}
	return path
}

// revealDataPath opens the data directory in the system's file manager. It
// is what a user wants after reading a path: not to copy it, but to look.
func (s *State) revealDataPath() {
	path, err := mygo.App.Path(mygo.PathUserData)
	if err != nil || strings.TrimSpace(path) == "" {
		return
	}
	mygo.Shell.OpenPath(path)
}

// copyVersionInfo puts what the about box says on the clipboard, because a
// bug report wants it and a person should not have to retype it.
func (s *State) copyVersionInfo() {
	if s.clipboard == nil {
		return
	}
	info := strings.Join([]string{
		s.versionLine(),
		runtime.GOOS + "/" + runtime.GOARCH,
		runtime.Version(),
	}, " · ")
	entry := domain.ClipboardEntry{
		ID:          "about-version",
		Kind:        domain.ClipKindText,
		Content:     info,
		Display:     info,
		ContentHash: kit.ClipContentHash(info),
		CopiedAt:    time.Now().UTC(),
	}
	s.clipboard.CopyAsync(entry, nil)
}
