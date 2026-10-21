package app

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/clipboard"
	contractbangumi "boku/internal/contract/bangumi"
	contractdeepseek "boku/internal/contract/deepseek"
	contractfree "boku/internal/contract/freetranslate"
	"boku/internal/credential"
	"boku/internal/domain"
	"boku/internal/kit"
	"boku/internal/panel"
	"boku/internal/translate"
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
		{kind: sectionAPIKey, name: "API Key", mark: s.keyMark, build: s.apiKeySection},
		{kind: sectionTranslate, name: "划词翻译", mark: s.translationMark, build: s.translateSection},
		{kind: sectionClipboard, name: "剪贴板", mark: s.clipboardMark, build: s.clipboardSection},
		{kind: sectionAnime, name: "动画", mark: s.tvMark, build: s.animeSection},
		{kind: sectionGeneral, name: "通用", mark: s.settingsMark, build: s.generalSection},
		{kind: sectionAbout, name: "关于", mark: s.infoMark, build: s.aboutSection},
	}
}

func (s *State) animeSection(c *ui.Context, head *ui.Handle) {
	if s.animeService == nil {
		return
	}
	inset(c, func() {
		stacked(c, float32(16), func() {
			sectionHead(c, head, s.tvMark, ui.Hex(animePrimary), "动画更新", "Bangumi「在看」今日放送 · api.bgm.tv")
			on := s.animeService.Enabled()
			ui.Checkbox(c, &on, "在悬浮窗显示今日动画更新").FontSize(sizeBody).OnClick(func() { s.saveAnime(on) })
			notice(c, notePlain, "仅展示「在看」且今日有新集的动画；无更新时不占位。")
			stacked(c, float32(8), func() {
				sectionLabel(c, "Bangumi 用户名")
				field := ui.TextInput(c, &s.animeUsername).Placeholder("用户名或 UID，如 sai").FillWidth().Height(heightControl)
				field.OnSubmit(func() { s.saveAnime(on) })
				notice(c, notePlain, "读取公开「在看」列表；不会保存或请求 Bangumi 密码、Token。")
			})
			stacked(c, float32(8), func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					sectionLabel(c, "Access Token（可选）")
					ui.Spacer(c)
					if link := wordButton(c, "获取 Token"); link.Clicked() {
						c.OpenURL("https://next.bgm.tv/demo/access-token")
					}
				})
				ui.Row(c).FillWidth().Gap(float32(6)).Children(func() {
					ui.TextInput(c, &s.animeTokenInput).Placeholder("仅私有收藏或写接口时需要").FillWidth().Grow(1).Height(heightControl)
					if button := designButton(c, "保存", buttonSecondary); button.Clicked() {
						s.saveAnimeToken()
					}
				})
				cardHint(c, "公开在看列表无需 Token；私有收藏会以 Authorization: Bearer 发送。")
			})
			stacked(c, float32(8), func() {
				sectionLabel(c, "刷新间隔（分钟）")
				ui.TextInput(c, &s.animeRefreshInput).FillWidth().Height(heightControl)
				cardHint(c, "后台轮询在看列表与章节放送日")
			})
			ui.Column(c).FillWidth().Background(colorField).Border(1, colorBorder).Radius(radiusField).Padding(float32(12)).Children(func() {
				ui.Text(c, "数据来源说明").FontSize(sizeSmall).FontWeight(600).TextColor(colorTextMuted)
				cardHint(c, "在看：GET /v0/users/…/collections?subject_type=2&type=3")
				cardHint(c, "新集：GET /v0/episodes?subject_id=…，按 airdate 筛选上海今日")
				cardHint(c, "Bangumi 仅提供放送日期；请求带应用名与联系方式。")
			})
			ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
				if button := designButton(c, "保存并刷新", buttonSecondary).Shrink(0); button.Clicked() {
					s.saveAnime(on)
				}
			})
		})
	})
}

func (s *State) saveAnimeToken() {
	if s.secretService == nil || strings.TrimSpace(s.animeTokenInput) == "" {
		return
	}
	token, window := strings.TrimSpace(s.animeTokenInput), s.settingsWindow
	go func() {
		err := s.secretService.Set(context.Background(), contractbangumi.ProviderID, "accessToken", token)
		if window != nil {
			window.Update(func() {
				if err != nil {
					s.panelStatus = "Bangumi Token 未保存：" + friendlyError(err)
				} else {
					s.animeTokenInput = ""
					s.panelStatus = "Bangumi Access Token 已保存。"
				}
				s.Refresh()
			})
		}
	}()
}

func (s *State) saveAnime(enabled bool) {
	if s.animeService == nil {
		return
	}
	minutes, minutesErr := strconv.Atoi(strings.TrimSpace(s.animeRefreshInput))
	if minutesErr != nil {
		s.panelStatus = "动画设置未保存：刷新间隔必须是数字。"
		s.Refresh()
		return
	}
	service, window := s.animeService, s.settingsWindow
	username := s.animeUsername
	go func() {
		err := service.Configure(context.Background(), enabled, username, minutes)
		if err == nil && enabled && strings.TrimSpace(username) != "" {
			err = service.RefreshIfNeeded(context.Background())
		}
		if window != nil {
			window.Update(func() {
				if err != nil {
					s.panelStatus = "动画设置未保存：" + friendlyError(err)
				}
				s.animeUsername = service.Username()
				s.animeRefreshInput = strconv.Itoa(service.RefreshMinutes())
				s.cacheAnimeCovers()
				s.fitQuickWindow()
				s.Refresh()
			})
		}
	}()
}

// customTranslatorID is the translator whose settings the user fills in
// themselves. It is named here as well as in the storage package because the
// window has two things about it that no other translator has: an address
// and a name.
const customTranslatorID = "custom"

// freeTranslatorIDs are the free engines: no key, nothing to configure, and
// what a fresh installation starts on. They are named here for the reason the
// storage package names them — the settings file holds the same strings, and
// one copy of a value that must match is one chance to get it wrong.
const (
	freeMicrosoftID = contractfree.MicrosoftEdge
	freeBilibiliID  = contractfree.Bilibili
)

// isFreeTranslator reports whether a translator is one of the free engines,
// which is what tells the picker to draw it in the free group and the API Key
// section to leave it out.
func isFreeTranslator(id string) bool {
	return id == freeMicrosoftID || id == freeBilibiliID
}

// The connect form's answers.
const (
	statusNone = iota
	statusOK
	statusError
)

// cardStatus is what one API key card last said: its own line, because the
// cards are independent and a failure in one must not be drawn under
// another.
type cardStatus struct {
	status int
	text   string
}

// connectForm is the settings window's state: what the user typed in each
// card, which providers have a credential stored, and what the last attempt
// answered.
//
// A card per provider rather than one form: the providers are configured
// independently, and the key one of them holds must not look like the key
// another was given.
type connectForm struct {
	// keys is what the user typed, per translator id, and only that. A
	// stored credential never goes into a field.
	keys map[string]string
	// hasCredential is whether each provider has one.
	hasCredential map[string]bool
	// newName, newURL and newModel are the service being added, as typed into the
	// blank card at the foot of the section. They are not credentials: they
	// live in the settings file, in the clear, and they are cleared once the
	// service is added.
	newName  string
	newURL   string
	newModel string
	// busy is the card that is being tested or saved, or empty.
	busy string
	// busyNote says what that card is busy with, as the line under it reads.
	// A card does three things — connect, save, remove — and only the first
	// of them is a connection.
	busyNote string
	// deleteArmed is the card whose stored credential has been asked about
	// and not yet confirmed: the card that is one press away from losing it.
	deleteArmed string
	// status is what each card's last attempt said.
	status map[string]cardStatus
	// inputCard is the card the credential field is showing, and input is
	// what has been typed into it.
	//
	// The field needs one string that outlives a frame — the framework binds
	// it by address — while the cards are drawn in a loop and only one of
	// them has a field at a time. So the typing lives here, and it is filed
	// under its card as soon as another card's field is built.
	inputCard string
	input     string
}

// translateCard is one translator as the window shows it: what it is called,
// where it asks, and whether it can be used now.
type translateCard struct {
	id              string
	displayName     string
	docsURL         string
	endpoint        string
	credentialKey   string
	needsCredential bool
	configured      bool
	supportsImage   bool
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
	s.loadCredentials()
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
	windowFrame(c).Children(func() {
		ui.Row(c).FillWidth().Grow(1).AlignItems(ui.Stretch).Children(func() {
			s.settingsSidebar(c)
			ui.Column(c).Fill().Grow(1).Children(func() {
				// The page takes the room that is left of the column and
				// scrolls inside it: a scroll container sized by its content
				// has nothing to scroll.
				body := ui.Scroll(c).FillWidth().Grow(1).TrackScroll(&s.settingsScroll)
				body.Children(func() {
					content(c, false, func() {
						s.settingsPageContent(c, body)
					})
				})
			})
		})
		footer(c, s.versionLine())
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
			betweenSections(c)
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
		Background(colorSidebar).
		// The sidebar's own edge: the line is only on the side that faces the
		// page, because that is the side where the two regions meet.
		BorderWidth(0, 0, 0, 1).
		BorderColor(colorBorder).
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
		Radius(radiusField).
		Justify(ui.Start).
		AlignItems(ui.Center).
		Gap(float32(8))
	ink := colorTextBody
	switch {
	case open:
		row.Background(colorBrand)
		ink = colorOnBrand
	case row.Hovered() || row.Focused():
		row.Background(colorHover)
	}
	if !open && row.Pressed() {
		row.Background(colorActive)
	}
	icon := colorTextMuted
	if open {
		icon = colorOnBrand
	}
	row.Children(func() {
		if page.mark != nil {
			ui.Icon(c, page.mark).Size(float32(16), float32(16)).Shrink(0).TextColor(icon)
		}
		ui.Text(c, page.name).
			FontSize(sizeSmall).
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

// apiKeySection is where the services are configured: one card per
// translator, each with what it needs, where to get it, and whether what is
// stored works.
//
// The section is the settings page's dense one. The design packs a service
// into two lines — its name and state, then its field with the actions beside
// it — so the head above the cards is a size down as well, and the warning
// above them is one small line rather than a panel with a title.
//
// The warning is required by §5 of the security design: the credentials are
// in a plain-text file, and the window has to say so. A user who believes a
// key is encrypted keeps worse ones in it than a user who knows it is not.
func (s *State) apiKeySection(c *ui.Context, head *ui.Handle) {
	inset(c, func() {
		stacked(c, float32(16), func() {
			sectionHeadSmall(c, head, s.keyMark, colorStoneInk, "API Key", "配置各 AI 服务密钥")

			// Only the services that have a key: this section is a key form,
			// and a card here is a field to fill in. The services that need
			// nothing are chosen in the translation section instead, where the
			// user is deciding what to translate with rather than what to
			// type.
			cards := keyedTranslateCards(s.translateCards())
			stacked(c, float32(12), func() {
				for _, card := range cards {
					s.apiKeyCard(c, card)
				}
				// The blank card is always the last one, and it is always
				// there: adding a second service is how a user who already
				// has one gets another, and a section whose only way to add is
				// to email the author is not one.
				s.newCustomServiceCard(c)
			})
		})
	})
}

// keyedTranslateCards is the services the API Key section draws: the ones that
// have a key to store, in the order they came in, with the user's own services
// last.
//
// The user's own services belong to this section even though their keys are
// optional: a card is where an address and a name are typed, and those are
// configuration rather than a credential. What is left out is the free engines,
// which have nothing to type.
//
// They are drawn last because they are a different kind of thing from the rest:
// the named services are a fixed catalogue the application knows, and these are
// the user's own entries, added by them. Reading a list is easier when what is
// fixed comes first and what was added comes after it.
func keyedTranslateCards(cards []translateCard) []translateCard {
	keyed := make([]translateCard, 0, len(cards))
	own := make([]translateCard, 0, len(cards))
	for _, card := range cards {
		if translate.IsCustomService(card.id) {
			own = append(own, card)
			continue
		}
		if card.needsCredential {
			keyed = append(keyed, card)
		}
	}
	return append(keyed, own...)
}

// apiKeyCard is one service: its name and its state on the first line, the
// field with the actions that act on it on the second, and one small line
// under them saying where the value comes from.
//
// Everything the card can do is on the field's own row, because the card is
// two lines: a service is read at a glance rather than scrolled through, and
// two of them still fit above the fold.
func (s *State) apiKeyCard(c *ui.Context, card translateCard) {
	ui.Column(c).FillWidth().
		Background(colorSurface).
		Border(1, colorBorder).
		Radius(radiusField).
		Padding(float32(10), float32(12)).
		Children(func() {
			stacked(c, float32(6), func() {
				s.apiKeyCardHead(c, card)
				s.credentialRow(c, card)
				s.cardStatusLine(c, card)
				s.apiKeyCardNote(c, card)
			})
		})
}

// apiKeyCardHead is the card's first line: what the service is called, whether
// it can be used, and — once a credential is stored — the way to take that
// credential away again.
//
// Every card wears the state badge, the user's own included: a service of
// theirs is configured when it has an address, which is a fact the badge can
// state without saying anything untrue. What a card does NOT wear is a badge
// for a credential it does not need — see credentialHint.
//
// 删除 is on every card the user can take something away from, which is two
// kinds of card and not one:
//
//   - a built-in service, where what goes is the key it holds — so a card with
//     nothing stored has nothing for the word to do;
//   - one of the user's own services, where what goes is the service itself —
//     and there is always something to remove, whether or not it has a key.
//     A local service needs no key at all, and gating the word on one left the
//     user with a card they had added and could not get rid of.
func (s *State) apiKeyCardHead(c *ui.Context, card translateCard) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
		ui.Text(c, card.displayName).
			FontSize(sizeCardName).
			FontWeight(600).
			TextColor(colorTextSoft).
			SingleLine()
		configuredBadge(c, card.configured)
		ui.Spacer(c)
		if !cardCanBeRemoved(card, s.form.hasCredential[card.id]) {
			return
		}
		s.deleteActions(c, card)
	})
}

// cardCanBeRemoved reports whether 删除 does anything on a card.
//
// The two kinds of card answer it differently and for the same reason — the
// word is drawn when there is something for it to take away: a service of the
// user's own is always removable, and a built-in one only once it holds a key.
func cardCanBeRemoved(card translateCard, hasCredential bool) bool {
	return translate.IsCustomService(card.id) || hasCredential
}

// newCustomServiceCard is the blank card at the foot of the section: where the
// user adds a service of their own.
//
// It is a card rather than a button because adding one is the same shape of
// work as configuring one — a name, an address, a key — and the user has just
// read three cards that look like this. A dialog would be a second place to
// learn the same form.
//
// It is drawn even when services exist: a user who already has one is exactly
// the user who wants a second.
func (s *State) newCustomServiceCard(c *ui.Context) {
	ui.Column(c).FillWidth().
		Background(colorSurface).
		Border(1, colorBorder).
		Radius(radiusField).
		Padding(float32(10), float32(12)).
		Children(func() {
			stacked(c, float32(6), func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
					ui.Text(c, customServiceCardTitle).
						FontSize(sizeCardName).
						FontWeight(600).
						TextColor(colorTextSoft).
						SingleLine()
					kindBadge(c, colorStoneTile, colorStoneInk, "OpenAI 兼容")
				})
				stacked(c, float32(6), func() {
					s.newServiceNameField(c)
					s.newServiceURLField(c)
					s.newServiceModelField(c)
				})
				s.newServiceRow(c)
				s.cardStatusLine(c, translateCard{id: newCustomServiceID})
			})
		})
}

// customServiceCardTitle is what the blank card is called.
const customServiceCardTitle = "自定义"

// newCustomServiceID is the id the blank card's status is filed under. It is
// not a translator id — the service gets one when it is added — and it is
// spelled so that it cannot collide with one.
const newCustomServiceID = "custom-new"

// deleteActions is what can be done with a stored credential, and it asks
// twice: the first press turns the word into a question, and only the second
// one removes anything.
//
// Removing a credential is the one thing here that typing cannot undo — the
// key itself is gone — so the design's single red word becomes two, and the
// user can leave without answering by pressing 取消. It is the two-step
// confirmation the credential UI design asks for, drawn in the
// card that holds the credential rather than in a dialog over it.
func (s *State) deleteActions(c *ui.Context, card translateCard) {
	if s.form.deleteArmed != card.id {
		if action := textAction(c, "删除", colorDanger); action.Clicked() {
			s.armDelete(card.id)
		}
		return
	}
	if action := textAction(c, "确认删除", colorDanger); action.Clicked() {
		s.deleteCredential(card)
	}
	if action := textAction(c, "取消", colorTextMuted); action.Clicked() {
		s.cancelDelete()
	}
}

// credentialRow is the card's second line: the field itself, and everything
// that can be done with what is in it. The field takes the room that is left
// of the line and the actions keep their own, so the row reads as the value
// and then what to do with it.
func (s *State) credentialRow(c *ui.Context, card translateCard) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(6)).Children(func() {
		s.credentialField(c, card)
		if button := cardButton(c, "测试", buttonSecondary); button.Clicked() {
			s.testCredential(card)
		}
		label := "保存"
		if card.id == customTranslatorID {
			label = "添加"
		}
		if button := cardButton(c, label, buttonPrimary); button.Clicked() {
			s.saveCredential(card)
		}
	})
}

// apiKeyCardNote is the smallest line under a card.
//
// It is empty for every card: the line that used to be here said where a
// service's balance shows, and it was removed rather than rewritten — the
// balance is not what an API key form is about, and the address it repeated is
// already in the field above it. The call is kept so that a card that does owe
// the user a line has somewhere to put it.
func (s *State) apiKeyCardNote(c *ui.Context, card translateCard) {
	_ = card
}

// configuredBadge says whether a service is configured, in the color that
// answers the question: green when it is, quiet when it is not.
func configuredBadge(c *ui.Context, configured bool) {
	tile, ink, label := colorStoneTile, colorStoneInk, "未配置"
	if configured {
		tile, ink, label = colorEmeraldTile, colorEmeraldInk, "已配置"
	}
	kindBadge(c, tile, ink, label)
}

// credentialField is the credential itself. It holds what the user is typing
// and never what is stored: a stored credential is a fact about the account
// rather than a value to retype, so the field stays empty and draws its own
// hint whether something is stored or not.
//
// The card keeps no room for a label over the field, so the field is named
// where a label would have been read: for assistive technology, and for
// whoever cannot see the name at the head of the card.
func (s *State) credentialField(c *ui.Context, card translateCard) {
	s.showCardInput(card.id)
	field := ui.TextInput(c, &s.form.input)
	field.Password().Placeholder(credentialHint(card)).SingleLine()
	field.Label(card.displayName + " " + credentialLabel(card.credentialKey))
	field.Disabled(s.form.busy != "")
	fieldFrame(c, field).Grow(1).Font("monospace")
}

// showCardInput points the one credential field at a card, filing what was
// typed into the card it belonged to.
//
// The window draws a field per card but binds them all to one string — the
// framework needs an address that outlives a frame, and a map entry has no
// address that does — so the value follows the field around: what a user
// typed into one card is still there when the frame comes back to it.
func (s *State) showCardInput(id string) {
	if s.form.inputCard == id {
		return
	}
	if s.form.inputCard != "" {
		if s.form.keys == nil {
			s.form.keys = map[string]string{}
		}
		s.form.keys[s.form.inputCard] = s.form.input
	}
	s.form.inputCard = id
	s.form.input = ""
	if s.form.keys != nil {
		s.form.input = s.form.keys[id]
	}
}

// keyValue is what was typed into a card, for the actions that read it. It is
// the same reading whether the card is the one on screen or the one before
// it: a test button can be pressed in the frame the field is drawn in.
func (s *State) keyValue(id string) string { return s.cardInput(id) }

// clearCardInput forgets what was typed into a card, once it has been saved.
//
// It clears the card's filed value whether or not that card is the one the
// single field is currently bound to: a save can happen in the frame after the
// user moved to another card, and leaving the value filed would put it back in
// the field the next time the frame drew it — a saved key reappearing in the
// box it was typed into.
func (s *State) clearCardInput(id string) {
	if s.form.keys != nil {
		delete(s.form.keys, id)
	}
	if s.form.inputCard == id {
		s.form.input = ""
	}
}

// newServiceNameField, newServiceURLField and newServiceModelField are the
// fields of the card
// that adds a service. They are not credentials: they are on the window's own
// face, not masked, and they are stored in the settings file once the service
// is added.
//
// They are named by their own hint rather than by a label above them — the
// design's compact card keeps its labels where the value goes — so each
// carries its name for assistive technology as well.
func (s *State) newServiceNameField(c *ui.Context) {
	field := ui.TextInput(c, &s.form.newName)
	field.Placeholder("名称，如本地 Ollama").Label("名称").SingleLine()
	field.Disabled(s.form.busy != "")
	fieldFrame(c, field).FillWidth()
}

func (s *State) newServiceURLField(c *ui.Context) {
	field := ui.TextInput(c, &s.form.newURL)
	field.Placeholder("Base URL  https://api.example.com/v1").Label("Base URL").SingleLine()
	field.Disabled(s.form.busy != "")
	fieldFrame(c, field).FillWidth().Font("monospace")
}

func (s *State) newServiceModelField(c *ui.Context) {
	field := ui.TextInput(c, &s.form.newModel)
	field.Placeholder("模型名称，如 gpt-4o-mini").Label("模型名称").SingleLine()
	field.Disabled(s.form.busy != "")
	fieldFrame(c, field).FillWidth().Font("monospace")
}

// newServiceRow is the blank card's second line: the optional key and the one
// action that puts the service into the list above.
//
// It is not the same row as a configured card's: that one saves a change to a
// service that exists, and this one creates one. Two words, because they are
// two different things — 保存 would say the service is already there.
func (s *State) newServiceRow(c *ui.Context) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(6)).Children(func() {
		s.showCardInput(newCustomServiceID)
		field := ui.TextInput(c, &s.form.input)
		field.Password().Placeholder("API Key（可选）").SingleLine()
		field.Label("自定义服务 API Key")
		field.Disabled(s.form.busy != "")
		fieldFrame(c, field).Grow(1).Font("monospace")

		if button := cardButton(c, "添加", buttonPrimary); button.Clicked() {
			s.addCustomService()
		}
	})
}

// cardStatusLine reports what one card's last attempt said, under that card.
// A card that is busy says what it is busy with: a removal is not a
// connection test, and the line must not tell the user it is one.
func (s *State) cardStatusLine(c *ui.Context, card translateCard) {
	if s.form.busy == card.id {
		text := s.form.busyNote
		if text == "" {
			text = "正在连接 " + card.displayName + "…"
		}
		ui.Row(c).AlignItems(ui.Center).Gap(float32(8)).Children(func() {
			ui.Spinner(c)
			ui.Text(c, text).
				FontSize(sizeSmall).
				TextColor(colorTextMuted)
		})
		return
	}
	state, ok := s.form.status[card.id]
	if !ok {
		return
	}
	switch state.status {
	case statusOK:
		notice(c, noteSuccess, state.text)
	case statusError:
		notice(c, noteDanger, state.text)
	}
}

// armDelete asks the user to say it twice: the card's red word becomes the
// question 确认删除, with 取消 beside it.
func (s *State) armDelete(id string) {
	s.form.deleteArmed = id
	s.Refresh()
}

// cancelDelete leaves the stored credential where it is, which is what a
// second press on 取消 is for.
func (s *State) cancelDelete() {
	s.form.deleteArmed = ""
	s.Refresh()
}

// deleteCredential removes the credential a card holds — and, for a card that
// is one of the user's own services, the service itself.
//
// The two are different things and the confirmation means different things with
// them: a service of the user's own is an entry they added, and 删除 on it
// takes the entry away, name and address and key together. A built-in
// service's name and address are not the user's to remove, so its 删除 takes
// the key alone and the card stays where it is.
//
// The write runs off the main thread like every other write in this file; the
// answer comes back through Window.Update, which is the only way a goroutine
// may touch this state.
func (s *State) deleteCredential(card translateCard) {
	if s.form.busy != "" || s.translate == nil {
		return
	}
	ownService := translate.IsCustomService(card.id)
	if !ownService && s.secretService == nil {
		return
	}
	secrets, window := s.secretService, s.settingsWindow
	service := s.translate
	id, key := card.id, card.credentialKey
	useFreeAfterDeletion := shouldUseFreeProviderAfterCredentialDeletion(id, s.translateSettingsOr().ProviderOr())
	s.form.busy, s.form.deleteArmed = card.id, ""
	slog.Info("translator configuration deletion requested", "event", "translator_configuration_deletion_requested", "provider", card.id, "customService", ownService)
	if ownService {
		s.form.busyNote = "正在删除这个服务…"
	} else {
		s.form.busyNote = "正在删除 " + credentialLabel(key) + "…"
	}
	s.setCardStatus(id, statusNone, "")

	go func() {
		ctx := backgroundContext()
		var err error
		if ownService {
			// The order is the reverse of adding one: the service is
			// forgotten first, so that nothing asks an address that is about
			// to be gone, and its key is dropped after.
			err = service.RemoveCustomTranslator(ctx, id)
			if err == nil {
				err = forgetCredential(ctx, secrets, id, key, s.form.hasCredential[id])
			}
			if err == nil {
				// And its translator goes with it. The cards are drawn from
				// the registry, so a service taken out of the settings but
				// left in the registry is a card that stays on the page with
				// no address and nothing to ask.
				if syncErr := s.unregisterOwnServices(); syncErr != nil {
					slog.Warn("the registry could not be brought in step", "error", syncErr)
				}
			}
		} else {
			err = secrets.Delete(ctx, id, key)
		}
		if err == nil && useFreeAfterDeletion {
			err = service.SetProvider(ctx, translate.DefaultProvider)
		}
		if window == nil {
			// No window: this state exists only while a test builds the
			// view, and there is nothing to hand the answer to.
			return
		}
		if err != nil {
			slog.Warn("translator configuration deletion failed", "event", "translator_configuration_deletion_failed", "provider", id, "error", err)
		} else {
			slog.Info("translator configuration deleted", "event", "translator_configuration_deleted", "provider", id, "customService", ownService)
		}
		window.Update(func() { s.credentialForgotten(id, useFreeAfterDeletion, err) })
	}()
}

// forgetCredential drops a service's key, when it has one.
//
// A service the user runs themselves may have been added with no key at all —
// a model on their own machine needs none — and asking the store to delete
// something that was never there answers ErrSecretNotFound. That is not a
// failure of the removal: the service is gone, which is what the user asked
// for, and reporting "not found" over it would tell them the deletion did not
// work when it did.
func forgetCredential(ctx context.Context, secrets *credential.Secrets, id, key string, stored bool) error {
	if secrets == nil || !stored {
		return nil
	}
	err := secrets.Delete(ctx, id, key)
	if errors.Is(err, domain.ErrSecretNotFound) {
		return nil
	}
	return err
}

// credentialForgotten is what a card says once its credential is gone: what is
// stored is read again, so the card stops calling itself configured and the
// 删除 that had to be confirmed is not drawn any more.
func (s *State) credentialForgotten(id string, usedFreeProvider bool, err error) {
	s.form.busy = ""
	if err != nil {
		s.setCardStatus(id, statusError, friendlyError(err))
		return
	}
	s.reloadCredentialsAfterSave()
	message := "已删除。"
	if usedFreeProvider {
		message = "已删除，已切换到免费服务。"
	}
	s.setCardStatus(id, statusOK, message)
}

// shouldUseFreeProviderAfterCredentialDeletion keeps the one built-in paid
// service from remaining selected after its only credential disappears. Own
// services retain their explicit selection semantics when removed.
func shouldUseFreeProviderAfterCredentialDeletion(removedID, selectedID string) bool {
	return removedID == contractdeepseek.ProviderID && selectedID == contractdeepseek.ProviderID
}

// translateSection is the translation tool's own settings: whether it runs,
// what it asks, how much it annotates, what its window does, and which key
// opens it.
//
// Everything but the switch is drawn only while the tool is on. The rest of the
// section configures a tool that is not running — which service to ask, how to
// annotate, which key opens it — and a page full of controls for something the
// user has just turned off is a page that invites them to configure a thing
// that will not happen. The switch stays, because it is how they turn it back
// on.
func (s *State) translateSection(c *ui.Context, head *ui.Handle) {
	if s.translate == nil {
		return
	}
	settings := s.translateSettingsOr()
	inset(c, func() {
		stacked(c, float32(16), func() {
			sectionHead(c, head, s.translationMark, colorStoneInk, "划词翻译", "任意语言 → 中文 · 免费基础 + AI 翻译")

			stacked(c, float32(12), func() {
				enabled := settings.Enabled
				ui.Checkbox(c, &enabled, "启用划词翻译").
					FontSize(sizeBody).
					OnClick(func() { s.setTranslationEnabled(enabled) })

				if !showsTranslationControls(settings) {
					notice(c, notePlain, "划词翻译已关闭，快捷键不再占用。打开后可以在这里选择翻译服务。")
					s.translateNoticeLine(c)
					return
				}

				s.providerPicker(c, settings)
				s.annotationSwitches(c, settings)
				s.windowSwitches(c, settings)
				s.shortcutSwitches(c)
				s.translateNoticeLine(c)
			})
		})
	})
}

// translateNoticeLine is what the last change to the tool's settings said.
func (s *State) translateNoticeLine(c *ui.Context) {
	if s.translateNotice != "" {
		notice(c, notePlain, s.translateNotice)
	}
}

// showsTranslationControls reports whether the translation section draws the
// settings that configure the tool — the service picker, the annotation
// switches, the window switches and the shortcut — or only the switch that
// turns it on.
//
// It is the switch itself that decides, because it is the same fact: a tool
// that is not running has nothing to configure, and the only thing its section
// owes the user is the way back on.
func showsTranslationControls(settings translate.Settings) bool {
	return settings.Enabled
}

// providerPicker is the choice of translator, in the two groups the design
// draws: the free services (no key, nothing to configure) above, and the
// services that need an API key below.
//
// The split is not decoration. A user who has just installed Boku has to be
// able to answer "which of these do I have to do something about" at a glance,
// and the answer is "none of the first group".
//
// Each row is one translator, and picking one picks that translator: the free
// engines are two choices, not one choice with a pool behind it. A user who
// picks 微软 Edge 翻译 asks it, and if it fails the card says so rather than
// quietly handing the text to the other endpoint — the choice is theirs, and
// the answer is about the one they made.
func (s *State) providerPicker(c *ui.Context, settings translate.Settings) {
	cards := pickerTranslateCards(s.translateCards())
	stacked(c, float32(8), func() {
		sectionLabel(c, "翻译服务")

		free, keyed := splitTranslateCards(cards)
		if len(free) > 0 {
			stacked(c, float32(6), func() {
				pickerGroupLabel(c, "基础翻译（免费 · 无需配置）")
				for _, card := range free {
					s.providerOption(c, card, settings)
				}
			})
		}
		if len(keyed) > 0 {
			stacked(c, float32(6), func() {
				pickerGroupLabel(c, "AI 大模型（需配置 API Key）")
				for _, card := range keyed {
					s.providerOption(c, card, settings)
				}
			})
		}
		notice(c, notePlain, "免费服务无需密钥；AI 服务仅显示已配置 API Key 的可选。")
	})
}

// pickerTranslateCards is the rows the picker draws, in the order the registry
// gave them.
//
// The order is never rearranged — not even to put the service in use first —
// because a list of choices that moves while the user is choosing is a list the
// user cannot aim at: the row they press must still be the row they pressed.
//
// A service the user has to configure is hidden until they have: the user's own
// service has an address to type before it is anything, and a row that cannot be
// picked yet is a row that only takes up room. The ones that need a key are
// still shown unconfigured, because that is how the user learns what they could
// configure.
func pickerTranslateCards(cards []translateCard) []translateCard {
	pickable := make([]translateCard, 0, len(cards))
	for _, card := range cards {
		if card.id == customTranslatorID && !card.configured {
			continue
		}
		pickable = append(pickable, card)
	}
	return pickable
}

// splitTranslateCards is the picker's two groups: the free engines, and the
// services that have something to configure.
//
// The user's own service is in the second group even though its key is
// optional: its card is the one with an address to type, so it belongs under
// the heading that says something has to be configured.
func splitTranslateCards(cards []translateCard) (free, keyed []translateCard) {
	for _, card := range cards {
		if isFreeTranslator(card.id) {
			free = append(free, card)
			continue
		}
		keyed = append(keyed, card)
	}
	return free, keyed
}

// pickerGroupLabel is the small line above a group of rows: smaller and
// quieter than sectionLabel, because it names the group rather than the form.
func pickerGroupLabel(c *ui.Context, label string) {
	ui.Text(c, label).FontSize(sizeSmall).FontWeight(500).TextColor(colorTextMuted)
}

// providerOption is one row of the picker: the service's name, its state, and
// a dot that says which one is in use.
//
// The whole row is the target rather than the dot alone: a row that is about
// one choice should answer anywhere on it. A service with nothing configured
// is drawn but cannot be chosen, which is what tells the user what to
// configure.
//
// The state on the right is the design's own three words: a free engine is
// 免费 — the reason it can be picked at all — a keyed one with a credential is
// 已配置, and one without is 未配置. The chip is the sky one the design gives
// it, because it says "nothing to do here" rather than "you did this right".
//
// 免费 is the free engines' own word, and only theirs: the user's own service
// needs no key either, but it is not one of the free ones — it is a service
// the user runs, and calling it 免费 would be telling them something about a
// price Boku does not know.
func (s *State) providerOption(c *ui.Context, card translateCard, settings translate.Settings) {
	selected := settings.ProviderOr() == card.id && card.configured
	row := ui.ButtonBase(c).Label("使用 "+card.displayName).
		FillWidth().
		Padding(float32(8), float32(12)).
		Radius(radiusField).
		Border(1, colorBorder).
		Disabled(!card.configured)
	switch {
	case selected:
		row.Background(colorBrandTint).Border(1, colorBrand)
	case row.Hovered() && card.configured:
		row.Background(colorHover)
	}
	row.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
			// The dot says which one is in use without taking the keyboard:
			// the row itself is the control.
			ui.Box(c).Size(float32(12), float32(12)).Radius(999).Shrink(0).
				Border(1, chooseColor(selected, colorBorder)).
				Background(chooseColor(selected, colorSurface)).
				Children(func() {
					if selected {
						ui.Box(c).Size(float32(6), float32(6)).Radius(999).
							Background(colorBrand)
					}
				})
			ui.Text(c, card.displayName).
				FontSize(sizeBody).
				TextColor(chooseTextColor(card.configured)).
				SingleLine()
			if card.supportsImage {
				kindBadge(c, colorSkyTile, colorSkyInk, "支持图片翻译")
			}
			ui.Spacer(c)
			switch {
			case isFreeTranslator(card.id):
				kindBadge(c, colorSkyTile, colorSkyInk, "免费")
			case card.configured:
				statusNote(c, "已配置")
			default:
				ui.Text(c, "未配置").FontSize(sizeTiny).TextColor(colorTextMuted).SingleLine()
			}
		})
	})
	if row.Clicked() && card.configured && !selected {
		s.setTranslationProvider(card.id)
	}
}

// chooseColor picks between two colors for the dot that says which service
// is in use.
func chooseColor(chosen bool, fallback ui.Color) ui.Color {
	if chosen {
		return colorBrand
	}
	return fallback
}

// chooseTextColor is what a service's name is written in: the body color
// when it can be used, and the muted one when it cannot.
func chooseTextColor(configured bool) ui.Color {
	if configured {
		return colorTextSoft
	}
	return colorTextMuted
}

// annotationSwitches are the Japanese enhancements: what the translator is
// asked to say about each word.
func (s *State) annotationSwitches(c *ui.Context, settings translate.Settings) {
	stacked(c, float32(10), func() {
		sectionLabel(c, "日语增强（仅日语原文）")
		furigana := settings.Furigana
		ui.Checkbox(c, &furigana, "汉字标注片假名读音").
			FontSize(sizeBody).
			OnClick(func() { s.setTranslationFurigana(furigana) })
		notice(c, notePlain, "例：機械語 → キカイゴ")

		gloss := settings.LoanwordGloss
		ui.Checkbox(c, &gloss, "片假名标注对应英文原词").
			FontSize(sizeBody).
			OnClick(func() { s.setTranslationLoanwordGloss(gloss) })
		notice(c, notePlain, "例：コンパイラ → compiler")
	})
}

// windowSwitches are the card's own behavior: whether it stays, and how long
// it waits before closing itself.
func (s *State) windowSwitches(c *ui.Context, settings translate.Settings) {
	stacked(c, float32(10), func() {
		sectionLabel(c, "浮窗行为")
		pinned := settings.Pinned
		ui.Checkbox(c, &pinned, "默认固定（Pin）浮窗").
			FontSize(sizeBody).
			OnClick(func() { s.setTranslationPinned(pinned) })

		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
			sectionLabel(c, "关闭延迟（毫秒）")
			ui.Spacer(c)
			s.closeDelayField(c, settings)
		})
		notice(c, notePlain, "浮窗失去焦点后等待这段时间再关闭；固定后不再自动关闭。")
	})
}

// closeDelayField is the delay as a number field: a duration in
// milliseconds, the unit a person thinks in.
//
// The value is written when the user asks for it rather than on every
// keystroke: a half-typed number is not a setting, and writing one would make
// the window close itself the moment a user selected the old value to
// replace it.
func (s *State) closeDelayField(c *ui.Context, settings translate.Settings) {
	field := ui.TextInput(c, &s.delayInput)
	field.Placeholder(strconv.FormatInt(settings.CloseDelayOr().Milliseconds(), 10)).SingleLine()
	field.Disabled(s.form.busy != "")
	inputFrame(c, field).Width(float32(88)).Font("monospace")

	text := strings.TrimSpace(s.delayInput)
	milliseconds := settings.CloseDelayOr().Milliseconds()
	switch {
	case text == "":
	case !isDigits(text):
		notice(c, noteDanger, "关闭延迟需要一个非负的毫秒数。")
		return
	default:
		if value, err := strconv.ParseInt(text, 10, 64); err == nil {
			milliseconds = value
		}
	}
	if button := designButton(c, "应用", buttonSecondary); button.Clicked() {
		s.setTranslationCloseDelay(time.Duration(milliseconds) * time.Millisecond)
	}
}

// isDigits reports whether a string is a run of decimal digits, which is what
// a millisecond count has to be.
func isDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// shortcutSwitches is the key that opens the window: one binding, because the
// tool has one action.
//
// The button is the recorder. Pressing it waits for the next combination; the
// combination is bound and remembered, and one the system or another
// application already holds leaves the binding that was there in place, with
// a line saying so. The design's other keys — 朗读原文、固定浮窗、关闭浮窗 —
// are not implemented, and are not drawn as buttons that would do nothing.
func (s *State) shortcutSwitches(c *ui.Context) {
	stacked(c, float32(8), func() {
		sectionLabel(c, "快捷键")

		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(12)).Children(func() {
			ui.Column(c).Grow(1).Children(func() {
				ui.Text(c, "划词后弹出翻译").FontSize(sizeBody).TextColor(colorTextSoft).SingleLine()
				ui.Text(c, "选中文字后按此键显示浮窗").FontSize(sizeTiny).TextColor(colorTextMuted).SingleLine()
			})
			s.shortcutButton(c, shortcutText)
		})
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(12)).Children(func() {
			ui.Column(c).Grow(1).Children(func() {
				ui.Text(c, "截图翻译").FontSize(sizeBody).TextColor(colorTextSoft).SingleLine()
				label := friendlyAccelerator(s.translateSettingsOr().ScreenshotAcceleratorOr())
				if label == "" {
					label = "已关闭"
				}
				ui.Text(c, "框选屏幕后直接使用 AI 翻译 · "+label).FontSize(sizeTiny).TextColor(colorTextMuted).SingleLine()
			})
			s.shortcutButton(c, shortcutScreenshot)
		})

		// What is bound now, or why nothing is. A registration that failed
		// has to be visible rather than silent, which is what
		// §6 of the tool design asks for.
		text, kind := s.shortcutLine()
		notice(c, kind, text)
		if note, noteKind := s.shortcutNote(); note != "" {
			notice(c, noteKind, note)
		}
		if platform := shortcutPlatformNote(); platform != "" {
			notice(c, notePlain, platform)
		}
		if note := selectionPlatformNote(); note != "" && !s.selectionGranted {
			notice(c, notePlain, note)
			// One button, named for what the user is trying to get. It asks
			// the system to put Boku in the Accessibility list; the system
			// prompt is what shows the switch, and it offers System Settings
			// itself. Opening the pane for a release build would cover that
			// prompt; a development build is the exception, below.
			if button := designButton(c, "获取辅助功能权限", buttonSecondary); button.Clicked() {
				if !s.requestSelectionAccess() && os.Getenv("MYGO_DEV") != "" {
					// mygo dev starts the app as a child of the terminal, and
					// TCC attributes the request to the terminal: the system
					// suppresses the prompt for a development build. Open the
					// pane instead, where the developer can add Boku Dev.
					c.OpenURL(accessibilitySettingsURL)
				}
			}
		}
		notice(c, notePlain, "点击快捷键可重新录制；与系统或其他软件冲突时请更换。")

		// An inline action at the end of the block, not another full-width
		// control: resetting is a correction to the binding above it, and a
		// block button would read as the section's main action.
		ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
			if button := designButton(c, "恢复默认快捷键", buttonWarning).Shrink(0); button.Clicked() {
				s.resetShortcut()
			}
		})
	})
}

// accessibilitySettingsURL is the System Settings page for Accessibility.
const accessibilitySettingsURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"

// requestSelectionAccess asks macOS to add Boku to its Accessibility list.
// The system owns the switch: the application can only ask, and the user then
// turns it on. The note says so rather than pretending the click granted it.
func (s *State) requestSelectionAccess() bool {
	granted := s.RequestSelectionAccess()
	slog.Info("selection access requested", "granted", granted)
	if granted {
		s.translateNotice = ""
	} else {
		s.translateNotice = "已请求辅助功能权限：请在「系统设置 > 隐私与安全性 > 辅助功能」中打开 Boku 的开关。"
	}
	// The request may have been granted before the call returned (an app
	// that already has the permission), or the system prompt may still be
	// up; the focus/activation refresh catches the latter.
	s.RefreshSelectionAccess()
	return granted
}

// RequestSelectionAccess asks the platform to show its permission prompt when
// the translation tool is on. The settings button is the caller: the settings
// window makes the app regular, which is what macOS needs before it shows the
// Accessibility prompt. The app does not ask at launch or from the shortcut:
// those run while it is accessory, and the system records such a request
// without showing a prompt.
func (s *State) RequestSelectionAccess() bool {
	if s.translate == nil {
		return false
	}
	settings, err := s.translate.Settings()
	if err != nil || !settings.Enabled {
		return false
	}
	return s.translate.RequestSelectionAccess()
}

// shortcutButton is the recording control: it shows the binding, and pressing
// it waits for the next combination.
//
// While it is recording it keeps the keyboard — the framework sends every key
// of the focused element to HandleInput — so the combination the user presses
// is read rather than acted on by the window.
func (s *State) shortcutButton(c *ui.Context, kind shortcutKind) ui.Element {
	state := s.shortcutStateFor(kind)
	accelerator := state.accelerator
	if accelerator == "" && kind == shortcutText {
		accelerator = s.translateSettingsOr().AcceleratorOr()
	}
	if accelerator == "" && kind == shortcutScreenshot {
		accelerator = s.translateSettingsOr().ScreenshotAcceleratorOr()
	}
	// The field shows the binding without the spaces the sentence under the
	// row uses: the two keys of a macOS binding plus their plus signs are
	// wider than the field once the clear icon has its room.
	label := compactAccelerator(accelerator)
	if label == "" {
		label = "点击录制"
	}
	if state.recording {
		label = "按下组合键…"
	}
	// The recorder always owns the same 140-point column. Screenshot's clear
	// affordance is layered inside that column, so its presence never shifts the
	// two shortcut fields off their shared right edge.
	control := ui.Box(c).
		Width(float32(140)).
		Height(heightControl - 4)
	// The clear icon's 22-point target sits 2 points in from the field's right
	// edge, and its mark starts 4 points further in. Reserve that column in the
	// label's padding: without it the last letter of "Shift+Command+T" is drawn
	// under the X, and the widest ordinary three-key binding
	// ("Command+Control+T") would lose its last letter to the ellipsis.
	labelPad := float32(12)
	if kind == shortcutScreenshot && accelerator != "" {
		labelPad = 24
	}
	control.Children(func() {
		button := ui.ButtonBase(c).Label("快捷键 "+friendlyAccelerator(accelerator)).
			Absolute().Fill().
			Padding(0, labelPad, 0, 12).
			Radius(radiusField).
			Border(1, colorBorder).
			Background(colorSurface).
			Focusable().
			Cursor(ui.CursorPointer)
		switch {
		case state.recording:
			button.Background(colorBrandTintStrong).Border(1, colorBrand)
		case button.Hovered() || button.Focused():
			button.Background(colorHover)
		}
		button.Children(func() {
			text := ui.Text(c, label).
				// A tight 10-point monospace label, written without spaces,
				// keeps the widest ordinary three-key combination (for example
				// Command+Control+T) inside the same field on every platform.
				FontSize(sizeTiny).
				Font("monospace").
				LetterSpacing(float32(-0.25)).
				SingleLine()
			if state.recording {
				text.TextColor(colorBrand)
			} else {
				text.TextColor(colorTextSoft)
			}
		})
		// The keys arrive here only while recording, and only this button takes
		// them: what is pressed elsewhere in the window is not a binding.
		button.HandleInput(s.captureShortcut)
		if state.recording {
			// The keyboard has to be here for the keys to arrive at all.
			button.AutoFocus()
		}
		if button.Clicked() {
			if state.recording {
				s.cancelShortcutCapture()
			} else {
				s.beginShortcutCapture(kind)
			}
		}
		if kind == shortcutScreenshot && accelerator != "" {
			// This is intentionally a child of the same field rather than a sibling
			// in the settings row: both controls stay aligned, and the X has a clear
			// association with only this binding.
			if clear := cardIconButton(c, s.closeMark, "清除截图翻译快捷键").Absolute().Right(float32(2)).Top(shortcutClearTop()); clear.Clicked() {
				s.clearScreenshotShortcut()
			}
		}
	})
	return control
}

func shortcutClearTop() float32 {
	fieldHeight := heightControl - 4
	iconTarget := sizeCardMark + 2*cardMarkPad
	return float32((fieldHeight - iconTarget) / 2)
}

// clipboardSection is the clipboard tool: the two switches, the record range
// and the way to the history.
//
// Both switches start off, and the section says why: the clipboard is the most
// sensitive thing Boku can read. Turning monitoring off stops the reading
// rather than hiding it, and the form says that too, because "off" that
// still reads would be a lie.
func (s *State) clipboardSection(c *ui.Context, head *ui.Handle) {
	if s.clipboard == nil {
		return
	}
	settings := s.clipboard.Settings()
	inset(c, func() {
		stacked(c, float32(16), func() {
			sectionHead(c, head, s.clipboardMark, colorStoneInk, "剪贴板", "监听与预览设置")

			stacked(c, float32(12), func() {
				notice(c, notePlain, "剪贴板内容可能包含密码、验证码与私人对话；默认不读取。")

				monitor := settings.Monitoring
				ui.Checkbox(c, &monitor, "监听剪贴板（每 "+intervalLabel(settings)+" 读取一次）").
					FontSize(sizeBody).
					OnClick(func() { s.setClipboardMonitoring(monitor) })

				visible := settings.RegionVisible
				ui.Checkbox(c, &visible, "在快捷窗口底部显示剪贴板内容").
					FontSize(sizeBody).
					OnClick(func() { s.setClipboardRegionVisible(visible) })

				// The record range: how much of what was copied is kept. It
				// is a choice among three rather than a number, and the line
				// under it says what shortening it costs, because the entries
				// that no longer fit are dropped rather than hidden.
				sizes := clipboard.HistorySizes()
				selected := 0
				for index, size := range sizes {
					if size == settings.KeepsHistory() {
						selected = index
					}
				}
				stacked(c, float32(8), func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
						sectionLabel(c, "记录范围")
						ui.Spacer(c)
						picker := segmented(c, &selected, historySizeLabels(sizes)...)
						if picker.Changed() {
							s.setClipboardHistorySize(sizes[selected])
						}
					})
					notice(c, notePlain, "只保留最近 "+strconv.Itoa(settings.KeepsHistory())+
						" 条；缩短范围会立刻丢弃更早的记录。")
					if !settings.Monitoring {
						notice(c, notePlain, "关闭监听后不再读取剪贴板，历史里已有的记录仍然保留。")
					}
				})

				ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
					if button := s.historyButton(c); button.Clicked() {
						s.openHistory()
					}
				})

				if text := s.clipboardStatusLine(); text != "" {
					notice(c, notePlain, text)
				}
			})
		})
	})
}

// historyButton is the compact trailing action that opens clipboard history.
func (s *State) historyButton(c *ui.Context) ui.Element {
	b := ui.ButtonBase(c).Label("打开剪贴板历史").
		Height(heightControl).
		Padding(0, float32(12)).
		Radius(radiusField).
		Background(colorSurface).
		Border(1, colorBorder).
		Cursor(ui.CursorPointer)
	switch {
	case b.Pressed():
		b = b.Background(colorActive)
	case b.Hovered() || b.Focused():
		b = b.Background(colorHover)
	}
	b.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(float32(6)).Children(func() {
			ui.Icon(c, s.clipboardMark).
				Size(float32(14), float32(14)).
				Shrink(0).
				TextColor(colorTextMuted)
			ui.Text(c, "打开剪贴板历史").FontSize(sizeFine).FontWeight(500).SingleLine()
		})
	})
	return b
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
	inset(c, func() {
		stacked(c, float32(16), func() {
			sectionHead(c, head, s.settingsMark, colorStoneInk, "通用", "启动与外观")

			stacked(c, float32(10), func() {
				// The appearance is the section's first choice because it is
				// the one the user sees before reading anything: three words
				// in the same segmented control the clipboard's record range
				// uses, so a choice between a few fixed things looks the same
				// wherever it appears.
				stacked(c, float32(8), func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
						sectionLabel(c, "主题")
						ui.Spacer(c)
						selected := appearanceIndex(s.appearance.Mode())
						picker := segmented(c, &selected, appearanceLabels...)
						if picker.Changed() && selected >= 0 && selected < len(appearanceModes) {
							s.setAppearance(appearanceModes[selected])
						}
					})
					notice(c, notePlain, "自动跟随系统外观；浅色与深色固定使用。")
					if s.appearanceStatus != "" {
						notice(c, notePlain, s.appearanceStatus)
					}
				})

				// The panels that read something of the user's own — today
				// the Codex quota, which comes from the login the command
				// line already made — are switches here. What the switch
				// says is the application's words; which panels exist is the
				// assembly's.
				for _, panel := range s.panelService.OptionalPanels() {
					on := panel.On
					label, note := panelSwitchCopy(panel)
					ui.Checkbox(c, &on, label).
						FontSize(sizeBody).
						OnClick(func() { s.setPanelOn(panel.Panel.ID, on) })
					notice(c, notePlain, note)
				}
				if s.panelStatus != "" {
					notice(c, notePlain, s.panelStatus)
				}

				s.autostartSwitch(c)
			})

			stacked(c, float32(8), func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					sectionLabel(c, "数据目录")
					ui.Spacer(c)
					if button := designButton(c, "在文件管理器中显示", buttonSecondary).Shrink(0); button.Clicked() {
						s.revealDataPath()
					}
				})
				ui.Text(c, s.dataPath()).
					FontSize(sizeSmall).
					TextColor(colorInputValue).
					Font("monospace").
					Wrap()
			})
		})
	})
}

// aboutSection is what the application is: the version, what it was built
// with, where its files are, and a button to check whether there is a newer
// version.
func (s *State) aboutSection(c *ui.Context, head *ui.Handle) {
	inset(c, func() {
		stacked(c, float32(16), func() {
			sectionHead(c, head, s.infoMark, colorStoneInk, "关于", "版本与构建")

			ui.Column(c).FillWidth().
				Background(colorField).
				Border(1, colorBorder).
				Radius(radiusField).
				Padding(float32(12)).
				Children(func() {
					stacked(c, float32(6), func() {
						aboutLine(c, "版本", s.versionLine())
						aboutLine(c, "平台", runtime.GOOS+" / "+runtime.GOARCH)
						aboutLine(c, "构建", runtime.Version())
						aboutLine(c, "凭据", s.credentialPath())
					})
				})

			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
				if button := designButton(c, "复制版本信息", buttonSecondary); button.Clicked() {
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
		ui.Text(c, label).FontSize(sizeSmall).TextColor(colorTextMuted).SingleLine().Shrink(0)
		ui.Spacer(c)
		ui.Text(c, value).
			FontSize(sizeSmall).
			TextColor(colorText).
			Font("monospace").
			SingleLine()
	})
}

// intervalLabel names the poll interval the way the switch reads.
func intervalLabel(settings clipboard.Settings) string {
	interval := settings.Interval()
	if interval%time.Second == 0 {
		return strconv.Itoa(int(interval/time.Second)) + " 秒"
	}
	return strconv.Itoa(int(interval/time.Millisecond)) + " 毫秒"
}

// setClipboardMonitoring flips the switch and says what happened, so the
// user is not left guessing whether it worked.
func (s *State) setClipboardMonitoring(enabled bool) {
	err := s.clipboard.SetMonitoring(backgroundContext(), enabled)
	settings := s.clipboard.Settings()
	switch {
	case err != nil:
		s.clipboardStatus = "开关没能写进设置文件：" + friendlyError(err) + "（本次运行内已生效）"
	case settings.Monitoring:
		s.clipboardStatus = "已开始读取剪贴板。"
	default:
		s.clipboardStatus = "已停止读取剪贴板。"
	}
	s.Refresh()
}

// setClipboardRegionVisible flips the visibility of the quick window's
// clipboard line.
func (s *State) setClipboardRegionVisible(visible bool) {
	err := s.clipboard.SetRegionVisible(backgroundContext(), visible)
	settings := s.clipboard.Settings()
	switch {
	case err != nil:
		s.clipboardStatus = "开关没能写进设置文件：" + friendlyError(err) + "（本次运行内已生效）"
	case settings.RegionVisible:
		s.clipboardStatus = "快捷窗口底部会显示剪贴板内容。"
	default:
		s.clipboardStatus = "快捷窗口底部不再显示剪贴板内容。"
	}
	s.Refresh()
}

// historySizeLabels names the record ranges the way the segments read.
func historySizeLabels(sizes []int) []string {
	labels := make([]string, 0, len(sizes))
	for _, size := range sizes {
		labels = append(labels, strconv.Itoa(size)+" 条")
	}
	return labels
}

// setClipboardHistorySize chooses the record range: what no longer fits is
// dropped here, not at the next copy.
func (s *State) setClipboardHistorySize(size int) {
	if s.clipboard == nil {
		return
	}
	err := s.clipboard.SetHistorySize(backgroundContext(), size)
	if err != nil {
		s.clipboardStatus = "没能写进设置文件：" + friendlyError(err) + "（本次运行内已生效）"
	}
	s.Refresh()
}

// clipboardStatusLine is the last thing the clipboard switches said.
func (s *State) clipboardStatusLine() string { return s.clipboardStatus }

// setTranslationEnabled turns the tool on or off — and, with it, the global
// shortcut.
//
// The shortcut follows the switch rather than the switch merely being read when
// the shortcut fires: a key this process holds is a key no other application
// can use, so a tool that is off must let it go. Turning it back on takes the
// key again, which is why this is one action rather than two: the two states a
// user can be in are "off, and the key is free" and "on, and the key opens it".
func (s *State) setTranslationEnabled(enabled bool) {
	slog.Info("translation enabled setting changed", "event", "translation_enabled_changed", "enabled", enabled)
	s.saveTranslationSetting("划词翻译已打开。", func(ctx context.Context) error {
		return s.translate.SetEnabled(ctx, enabled)
	})
	s.bindShortcutForSwitch()
}

// setTranslationFurigana turns the reading of Japanese kanji on or off.
func (s *State) setTranslationFurigana(on bool) {
	slog.Info("furigana setting changed", "event", "translation_furigana_changed", "enabled", on)
	s.saveTranslationSetting("已更新汉字读音的标注。", func(ctx context.Context) error {
		return s.translate.SetFurigana(ctx, on)
	})
}

// setTranslationLoanwordGloss turns the English original of a katakana
// loanword on or off.
func (s *State) setTranslationLoanwordGloss(on bool) {
	slog.Info("loanword gloss setting changed", "event", "translation_loanword_gloss_changed", "enabled", on)
	s.saveTranslationSetting("已更新外来语原词的标注。", func(ctx context.Context) error {
		return s.translate.SetLoanwordGloss(ctx, on)
	})
}

// setTranslationPinned decides whether the window starts pinned.
func (s *State) setTranslationPinned(pinned bool) {
	slog.Info("translation default pin setting changed", "event", "translation_default_pin_changed", "pinned", pinned)
	s.saveTranslationSetting("浮窗行为已保存。", func(ctx context.Context) error {
		return s.translate.SetPinned(ctx, pinned)
	})
}

// setTranslationCloseDelay decides how long the window waits before closing
// itself.
func (s *State) setTranslationCloseDelay(delay time.Duration) {
	slog.Info("translation close delay changed", "event", "translation_close_delay_changed", "delay", delay)
	s.saveTranslationSetting("关闭延迟已保存。", func(ctx context.Context) error {
		return s.translate.SetCloseDelay(ctx, delay)
	})
	s.delayInput = ""
}

// setTranslationProvider chooses the translator to use.
func (s *State) setTranslationProvider(id string) {
	slog.Info("translation provider changed", "event", "translation_provider_changed", "provider", id)
	s.saveTranslationSetting("已改用 "+id+"。", func(ctx context.Context) error {
		return s.translate.SetProvider(ctx, id)
	})
}

// saveTranslationSetting writes one switch and says what happened. Every
// translation switch goes through here, so that a failure to write the file
// is answered the same way everywhere: the switch did not change, and the
// window says so instead of showing a setting that is not stored.
func (s *State) saveTranslationSetting(ok string, save func(context.Context) error) {
	if s.translate == nil {
		return
	}
	if err := save(backgroundContext()); err != nil {
		s.translateNotice = "没能写进设置文件：" + friendlyError(err)
	} else {
		s.translateNotice = ok
	}
	s.Refresh()
}

// translateCards is the translators the window offers, in the order it
// shows them, with whether each can be used now. It is what the API key
// section draws a card for and what the picker draws a row for.
func (s *State) translateCards() []translateCard {
	if s.translate == nil {
		return nil
	}
	views, err := s.translate.Providers(backgroundContext())
	if err != nil {
		return nil
	}
	cards := make([]translateCard, 0, len(views))
	for _, view := range views {
		cards = append(cards, translateCard{
			id:              view.ID,
			displayName:     view.DisplayName,
			docsURL:         view.DocsURL,
			endpoint:        view.Endpoint,
			credentialKey:   view.CredentialKey,
			needsCredential: view.NeedsCredential,
			configured:      view.Configured,
			supportsImage:   view.SupportsImage,
		})
	}
	return cards
}

// translateSettings is the translation configuration, for the card's view.
func (s *State) translateSettings() (translate.Settings, error) {
	if s.translate == nil {
		return translate.Defaults(), nil
	}
	return s.translate.Settings()
}

// translateSettingsOr is the same configuration, or its defaults when it
// cannot be read: a window that cannot read a settings file shows what a
// fresh installation has rather than nothing.
func (s *State) translateSettingsOr() translate.Settings {
	settings, err := s.translateSettings()
	if err != nil {
		return translate.Defaults()
	}
	return settings
}

// cardEndpointNote is a translator's endpoint written the way one line can
// carry it: the addresses themselves, without the parenthetical the
// descriptor adds for the card's own sake.
//
// It is a pure function rather than part of the line, because what an endpoint
// reads as is the one thing about it a test can decide.
func cardEndpointNote(endpoint string) string {
	if index := strings.Index(endpoint, "（"); index >= 0 {
		return strings.TrimSpace(endpoint[:index])
	}
	return strings.TrimSpace(endpoint)
}

// credentialIsShown reports whether a card is holding a credential the user
// has not typed over: something is stored and the field is empty, so there is
// nothing new to save or to test. Typing into the field is what leaves this
// state.
func (s *State) credentialIsShown(card translateCard) bool {
	return s.form.hasCredential[card.id] && strings.TrimSpace(s.cardInput(card.id)) == ""
}

// cardInput is what a card's field holds, whether or not that card is the
// one the field is currently showing: what was typed into another card is
// filed under it until the frame comes back to it.
func (s *State) cardInput(id string) string {
	if s.form.inputCard == id {
		return strings.TrimSpace(s.form.input)
	}
	if s.form.keys == nil {
		return ""
	}
	return strings.TrimSpace(s.form.keys[id])
}

// loadCredentials reads which credentials are already stored. It runs on the
// main thread, over local files, when the window opens.
//
// Nothing of what is stored is read into the window. The field is where a new
// key is typed, and the only thing a card says about a stored one is that it is
// there: its badge, and the 删除 that can take it away. Even the last four
// characters stay in the store — what the window asks for is a name.
func (s *State) loadCredentials() {
	s.form = connectForm{
		keys:          map[string]string{},
		hasCredential: map[string]bool{},
		status:        map[string]cardStatus{},
	}
	for _, card := range s.translateCards() {
		names, err := s.secretService.Names(backgroundContext(), card.id)
		if err != nil {
			s.form.status[card.id] = cardStatus{status: statusError, text: friendlyError(err)}
			continue
		}
		s.form.hasCredential[card.id] = credentialStored(names, card)
	}
}

// credentialStored reports whether what a provider has stored includes the
// credential its card is about. The names are the store's own, provider id and
// all, so a provider that holds some other credential is not read as
// configured.
func credentialStored(names []string, card translateCard) bool {
	want := domain.CredentialKey(card.id, card.credentialKey)
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

// reloadCredentialsAfterSave refreshes the cards' state without clearing the
// message that was just set, nor what the user has typed in the other cards.
func (s *State) reloadCredentialsAfterSave() {
	status, keys := s.form.status, s.form.keys
	name, url, model := s.form.newName, s.form.newURL, s.form.newModel
	s.loadCredentials()
	s.form.status = status
	if keys != nil {
		s.form.keys = keys
	}
	s.form.newName, s.form.newURL, s.form.newModel = name, url, model
}

// testCredential asks a service whether a key works, without storing it. The
// request runs off the main thread; the answer comes back through
// Window.Update, which is the only way a goroutine may touch this state.
//
// The address is already stored by the time this runs: a service of the user's
// own is added first and tested after, because the translator that asks it
// reads its address from the settings and does not need the window to translate
// every keystroke into a save.
func (s *State) testCredential(card translateCard) {
	if s.form.busy != "" || s.translate == nil {
		return
	}
	if s.credentialIsShown(card) {
		s.setCardStatus(card.id, statusError, "要先填入新的 "+
			credentialLabel(card.credentialKey)+"，再测试连接。")
		return
	}
	if card.needsCredential && strings.TrimSpace(s.keyValue(card.id)) == "" {
		s.setCardStatus(card.id, statusError, "请先填入 "+credentialLabel(card.credentialKey)+"。")
		return
	}
	window := s.settingsWindow
	service := s.translate
	override := map[string]string{}
	if value := strings.TrimSpace(s.keyValue(card.id)); value != "" {
		override[card.credentialKey] = value
	}
	s.form.busy = card.id
	slog.Info("translator connection test requested", "event", "translator_connection_test_requested", "provider", card.id, "hasCredentialOverride", len(override) > 0)
	s.form.busyNote = "正在连接 " + card.displayName + "…"
	s.setCardStatus(card.id, statusNone, "")

	go func() {
		startedAt := time.Now()
		err := service.TestTranslator(context.Background(), card.id, override)
		if err != nil {
			slog.Warn("translator connection test failed", "event", "translator_connection_test_failed", "provider", card.id, "duration", time.Since(startedAt), "error", err)
		} else {
			slog.Info("translator connection test completed", "event", "translator_connection_test_completed", "provider", card.id, "duration", time.Since(startedAt))
		}
		window.Update(func() {
			s.form.busy = ""
			if err != nil {
				s.setCardStatus(card.id, statusError, friendlyError(err))
				return
			}
			s.setCardStatus(card.id, statusOK, "连接成功，可以保存了。")
		})
	}()
}

// addCustomService is the blank card's action: it stores the service the user
// described, registers the translator that asks it, and tests it once so that
// the answer is right there rather than on the next translation.
//
// The order matters. The service is written first, because the translator reads
// its address from the settings; the translator is registered second, because
// the test reaches it through the registry; and the test runs last, because
// there is nothing to test until both are done.
//
// The card is cleared whether or not the test passed: a service that could not
// be reached is still a service the user configured, and making them retype the
// address to try again would be punishing them for their own typo.
func (s *State) addCustomService() {
	if s.form.busy != "" || s.translate == nil {
		return
	}
	name := strings.TrimSpace(s.form.newName)
	configured := translate.CustomSettings{BaseURL: s.form.newURL, Model: s.form.newModel}.Normalize()
	address, model := configured.BaseURL, configured.Model
	if address == "" {
		s.setCardStatus(newCustomServiceID, statusError, "请先填写 Base URL。")
		s.Refresh()
		return
	}
	if model == "" {
		s.setCardStatus(newCustomServiceID, statusError, "请填写模型名称。")
		s.Refresh()
		return
	}
	key := strings.TrimSpace(s.cardInput(newCustomServiceID))
	window := s.settingsWindow
	service := s.translate
	secrets := s.secretService

	s.form.busy = newCustomServiceID
	slog.Info("custom translator creation requested", "event", "custom_translator_creation_requested", "hasAPIKey", key != "")
	s.form.busyNote = "正在添加…"
	s.setCardStatus(newCustomServiceID, statusNone, "")

	go func() {
		startedAt := time.Now()
		ctx := context.Background()
		id, err := service.AddCustomTranslator(ctx, translate.CustomSettings{
			BaseURL:     address,
			DisplayName: name,
			Model:       model,
		})
		if err == nil && key != "" && secrets != nil {
			err = secrets.Set(ctx, id, "apiKey", key)
		}
		if err == nil {
			// The new service needs a translator before it can be asked.
			if err = s.registerOwnServices(); err != nil {
				slog.Warn("the new service could not be registered", "error", err)
			}
		}
		// A custom service can point at any OpenAI-compatible gateway. Its model
		// catalogue does not reliably advertise vision support, so test the
		// exact endpoint with a tiny image after the key and translator exist.
		// Failure does not undo the service: it can still be a useful text
		// translator, and the stored capability remains false.
		imageSupported := false
		if err == nil {
			probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			imageSupported, _ = service.DetectCustomImageCapability(probeCtx, id)
			cancel()
		}
		if err != nil {
			slog.Warn("custom translator creation failed", "event", "custom_translator_creation_failed", "duration", time.Since(startedAt), "error", err)
		} else {
			slog.Info("custom translator created", "event", "custom_translator_created", "provider", id, "imageSupported", imageSupported, "duration", time.Since(startedAt))
		}
		window.Update(func() {
			s.form.busy = ""
			if err != nil {
				s.setCardStatus(newCustomServiceID, statusError, friendlyError(err))
				return
			}
			s.form.newName, s.form.newURL, s.form.newModel = "", "", ""
			s.clearCardInput(newCustomServiceID)
			s.reloadCredentialsAfterSave()
			if imageSupported {
				s.setCardStatus(id, statusOK, "已添加，支持图片翻译。")
			} else if key != "" {
				s.setCardStatus(id, statusOK, "已添加，仅支持文本翻译。")
			} else {
				s.setCardStatus(id, statusOK, "已添加。")
			}
			s.setCardStatus(newCustomServiceID, statusNone, "")
			if key == "" {
				s.testCredential(cardOfID(s, id))
			}
		})
	}()
}

// registerOwnServices puts a translator in the registry for every service the
// settings hold, and takes out the ones they no longer hold.
//
// It is one call for both because it does one thing: make the registry match
// the settings. The cards the window draws come from the registry, and the
// services that exist come from the settings, so two directions of change —
// adding a service and removing one — are the same sync seen from either end.
//
// It is a field on the state rather than a call to the assembly so that a test
// can see what a window does without a registry to reach into.
func (s *State) registerOwnServices() error {
	if s.ownServices == nil {
		return nil
	}
	return s.ownServices()
}

// unregisterOwnServices is the same sync, for the removal path. It is named
// separately because what it is for is not obvious at the call site: a removed
// service has to leave the registry, or its card stays on the page.
func (s *State) unregisterOwnServices() error { return s.registerOwnServices() }

// cardOfID is one card of the window, by id, without failing: the answer is
// empty when there is no such card, which a caller in a goroutine needs.
func cardOfID(s *State, id string) translateCard {
	for _, card := range s.translateCards() {
		if card.id == id {
			return card
		}
	}
	return translateCard{id: id}
}

// customValues is the user's own service as typed, cleaned the way the
// translation tool cleans it.
// saveCredential stores the key of a configured card. A card of the user's own
// service saves its key like any other: its address is set when it is added and
// does not change here.
func (s *State) saveCredential(card translateCard) {
	if s.form.busy != "" || s.translate == nil {
		return
	}
	if s.credentialIsShown(card) {
		s.setCardStatus(card.id, statusError, "要先填入新的 "+
			credentialLabel(card.credentialKey)+"，再保存。")
		return
	}
	key := strings.TrimSpace(s.keyValue(card.id))
	if card.needsCredential && key == "" {
		s.setCardStatus(card.id, statusError, "请先填入 "+credentialLabel(card.credentialKey)+"。")
		return
	}
	if !card.needsCredential && key == "" {
		// Nothing to store: an optional key that is not there means the
		// service needs none, and it is already configured.
		s.setCardStatus(card.id, statusOK, "这个服务不需要密钥。")
		s.Refresh()
		return
	}
	window := s.settingsWindow
	secrets := s.secretService
	id, credentialKey := card.id, card.credentialKey
	s.form.busy = card.id
	slog.Info("translator credential save requested", "event", "translator_credential_save_requested", "provider", card.id, "credentialKey", card.credentialKey)
	s.form.busyNote = "正在保存…"
	s.setCardStatus(card.id, statusNone, "")

	go func() {
		err := secrets.Set(context.Background(), id, credentialKey, key)
		if err != nil {
			slog.Warn("translator credential save failed", "event", "translator_credential_save_failed", "provider", id, "credentialKey", credentialKey, "error", err)
		} else {
			slog.Info("translator credential saved", "event", "translator_credential_saved", "provider", id, "credentialKey", credentialKey)
		}
		window.Update(func() {
			s.form.busy = ""
			if err != nil {
				s.setCardStatus(id, statusError, friendlyError(err))
				return
			}
			s.clearCardInput(id)
			s.reloadCredentialsAfterSave()
			s.setCardStatus(id, statusOK, "已保存。")
			// A panel now has what it was missing: fetch with it.
			s.refreshAsync(true)
		})
	}()
}

// setCardStatus records what one card's last attempt said.
func (s *State) setCardStatus(id string, status int, text string) {
	if s.form.status == nil {
		s.form.status = map[string]cardStatus{}
	}
	s.form.status[id] = cardStatus{status: status, text: text}
}

// credentialPath names the file the credentials are in, for the warning the
// form shows.
func (s *State) credentialPath() string {
	if path := s.secretService.Path(); path != "" {
		return path
	}
	return "本机文件"
}

// credentialFile is that file as the warning names it: the file alone, since
// the line it sits on is a hint, and the about box spells the whole path out
// for whoever wants to find it.
func (s *State) credentialFile() string { return fileOf(s.credentialPath()) }

// fileOf is the last name of a path, or the path itself when it has none. It
// is a pure function rather than part of the warning, because what a path ends
// in is the one thing about it a test can decide.
func fileOf(path string) string {
	path = strings.TrimSpace(path)
	if index := strings.LastIndexAny(path, `/\`); index >= 0 {
		if name := strings.TrimSpace(path[index+1:]); name != "" {
			return name
		}
	}
	return path
}

// credentialLabel names a credential the way a person would.
func credentialLabel(key string) string {
	switch key {
	case "apiKey":
		return "API Key"
	case "adminKey":
		return "Admin Key"
	case "steamId":
		return "Steam ID"
	case "":
		return "凭据"
	default:
		return key
	}
}

// credentialHint is what an empty credential field shows: the shape of the
// key the service hands out, so that a user who pastes the wrong thing can
// see that it is the wrong thing. A service that can do without one says so,
// because the field is the only place its name would be read.
func credentialHint(card translateCard) string {
	if !card.needsCredential {
		return "API Key（可选）"
	}
	if card.credentialKey == "apiKey" {
		return "sk-......"
	}
	return credentialLabel(card.credentialKey)
}

// docsAddress is a documentation link written the way a person reads it: the
// host and path, without the scheme, because the line it sits on is a hint
// rather than a link.
func docsAddress(url string) string {
	trimmed := strings.TrimSpace(url)
	trimmed = strings.TrimPrefix(trimmed, "https://")
	trimmed = strings.TrimPrefix(trimmed, "http://")
	return trimmed
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

// setPanelOn flips an optional panel's switch and says what happened, so the
// user is not left guessing whether it worked.
//
// Turning a panel off takes it out of the quick window and out of every fetch:
// nothing is drawn for it, and nothing asks its provider — which, for a panel
// whose credential is a local login, is also the promise that the login file
// is not read while the switch is off.
func (s *State) setPanelOn(panelID string, on bool) {
	err := s.panelService.SetPanelOn(backgroundContext(), panelID, on)
	switch {
	case err != nil:
		s.panelStatus = "开关没能写进设置文件：" + friendlyError(err) + "（本次运行内已生效）"
	case on:
		s.panelStatus = "已开启：悬浮窗会重新显示它。"
	default:
		s.panelStatus = "已关闭：悬浮窗不再显示它，也不再读取它的数据。"
	}
	s.Refresh()
}

// panelSwitchCopy is what the switch of one optional panel says: the label on
// the switch, and the line under it that explains what turning it off does.
//
// The words are the application's, which is why they are here and not in the
// panel's definition: the definition says which panels exist, and the window
// says how they are described to a person. A panel with no copy of its own
// gets the panel's own name, so that adding one never leaves a nameless
// switch.
func panelSwitchCopy(panel panel.OptionalPanel) (label, note string) {
	switch panel.Panel.ProviderID {
	case "codex":
		return "本地 Codex 额度监控",
			"读取本机 Codex 命令行的登录状态，在悬浮窗显示额度；关闭后不再读取。"
	default:
		return panel.Panel.Title, "关闭后悬浮窗不再显示它，也不再读取它的数据。"
	}
}

// backgroundContext is what a background job with nothing to cancel is
// given: the work carries its own timeouts, and a job that outlives the
// window that asked for it is still worth finishing.
func backgroundContext() context.Context { return context.Background() }
