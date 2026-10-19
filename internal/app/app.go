// Package app is the presentation layer: view state, the views MyGo draws,
// and the windows that show them.
//
// It owns the tray icon and the flyout, because MyGo's callbacks and the
// views run on the main thread and share the same state. Everything is
// created and changed on that thread; no field of State may be touched from
// a goroutine, and results of background work are handed back with
// Window.Update.
//
// The panels themselves are not state here: the use case keeps them, so a
// refresh that finishes while the window is closed cannot lose its answer,
// and the view reads what is known without doing any work of its own.
package app

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/app/assets"
	"boku/internal/clipboard"
	"boku/internal/credential"
	"boku/internal/domain"
	"boku/internal/panel"
	"boku/internal/platform/autostart"
	"boku/internal/translate"
)

const (
	appName = "Boku"
	// toolTip is what the system shows when the pointer rests on the tray
	// icon: the application's name, and nothing after it.
	//
	// It used to add "— 我的面板". The tray is a five-millimetre square, and
	// the line that appears over it is read while the pointer is already
	// there to click it: the extra words were a sentence nobody was reading.
	// The name is what tells the user whose icon this is, which is the only
	// question the tooltip has to answer.
	toolTip    = appName
	panelWidth = 320
	// panelHeight leaves room for the page's margin, the panel's section, the
	// clipboard line under it and the footer. A section with more in it
	// scrolls rather than pushing the lines off the window.
	panelHeight = 380
	// trayIconSize is the icon in device pixels: 16 points on a Retina
	// display, which MyGo scales to the screen's density on Windows and
	// Linux.
	trayIconSize = 32
)

// Deps is what the presentation layer is assembled from, by main.
type Deps struct {
	// Panels reads and refreshes the panels.
	Panels *panel.Panels
	// Secrets stores the credentials those panels need.
	Secrets *credential.Secrets
	// Clipboard reads and records the system clipboard. It is what the
	// quick window's clipboard line and the history window show; a State
	// without one draws neither.
	Clipboard *clipboard.Clipboard
	// Translate is the selection-translation tool. A State without one
	// draws no translation window and binds no shortcut for it.
	Translate *translate.Translate
	// RegisterOwnServices registers a translator for every service the user
	// runs themselves. The window calls it after the user adds one: a service
	// the settings hold but the registry does not know is a row that cannot
	// answer. It is nil in a test that never adds a service.
	RegisterOwnServices func() error
	// Shortcuts binds the global shortcuts. A State without one still runs:
	// a shortcut that cannot be bound is a tray application with one way in
	// fewer, not a failure to start.
	Shortcuts ShortcutRegistrar
	// Version is the application version, shown in the footer.
	Version string
	// Quit is how the tray application leaves: it has no main window to
	// close.
	Quit func()
	// Appearance is the theme the user chose. It is the presentation's own
	// setting, stored beside the features' keys; nil means "follow the
	// desktop", which is also the default.
	Appearance *Appearance
	// AppearanceStore is the settings store the choice is written through.
	// A state without one still changes the appearance for this run.
	AppearanceStore AppearanceStore
	// ApplyAppearance gives the desktop the chosen appearance, so that the
	// title bars, menus and system-drawn window chrome around Boku's own
	// drawing match it. It is nil in a test that has no desktop.
	ApplyAppearance func(AppearanceMode)
	// PopoverAppearance gives a native popover the dark appearance a forced
	// dark mode needs. It is nil where the platform has no way to do it, in
	// which case forced dark uses the application's card. main injects the
	// platform implementation; replacing it with one backed by MyGo's own
	// API (or nil) is the only change that future API needs.
	PopoverAppearance PopoverAppearance
	// Autostart is the platform's login start: what "开机自动启动" reads and
	// writes. It is nil where nothing was injected, and a State without one
	// draws no switch — a switch that cannot do anything is worse than none.
	// main injects internal/platform/autostart's platform implementation; a
	// test injects its own.
	Autostart autostart.Manager
	// Updates is the update check. A State without one draws no update row,
	// starts no check and adds nothing to the tray menu: a build without an
	// updater is every test and every assembly that wired none.
	//
	// The interface is the presentation's own, so that nothing here imports
	// internal/update or MyGo's updater. main injects the real service; a
	// test injects a fake.
	Updates UpdateService
}

// State is the whole presentation state. It is read and written on the main
// thread only.
type State struct {
	app           *mygo.Application
	panelService  *panel.Panels
	secretService *credential.Secrets
	clipboard     *clipboard.Clipboard
	translate     *translate.Translate
	shortcuts     ShortcutRegistrar
	version       string
	quit          func()

	// appearance is the theme choice, and dark is what it resolves to right
	// now: the views read the resolved answer rather than asking the desktop
	// on every frame. systemDark is the desktop's last known answer, which is
	// what the automatic mode follows.
	appearance        *Appearance
	appearanceStore   AppearanceStore
	applyAppearance   func(AppearanceMode)
	popoverAppearance PopoverAppearance
	// autostart is the platform's login start, or nil when there is none to
	// offer. The settings page's switch is drawn from it every frame, and it
	// is the only writer: the system's own record is the one truth about
	// whether Boku starts at login.
	autostart autostart.Manager
	// updates is the update check, or nil in a build without one. The state
	// it reports is read from it every frame rather than kept here, so that
	// an answer arriving while no window is open is not lost.
	updates UpdateService
	// updateNotifier puts a notice about a ready update on the desktop. It
	// is nil until an update service is assembled, which is why the row can
	// be drawn without one.
	updateNotifier updateNotifier
	// updateNotified is the version whose notice has already been shown in
	// this run, so that a state polled once a frame is announced once.
	updateNotified string
	// autostartStatus is what the last press of that switch said, shown under
	// it. The state itself is not kept here, only the answer to the press.
	autostartStatus autostartStatus
	// appearanceStatus is what the last attempt to save the theme said,
	// shown under the control in the settings window.
	appearanceStatus string
	dark             bool
	systemDark       bool

	trayIcon []byte
	// deepseekMark is the DeepSeek whale: the mark its block of the quick
	// window carries. It is parsed once at startup, because a view is built
	// several times per frame and parsing a document is not a view's work.
	deepseekMark *ui.SVG
	// gaugeMark is the placeholder a block carries when the application has
	// no drawing of its provider yet. It is not a brand: it says "a measure"
	// until the real mark arrives.
	gaugeMark *ui.SVG
	// clipboardMark labels the quick window's clipboard line, parsed for the
	// same reason.
	clipboardMark *ui.SVG
	// refreshMark is the panel's refresh action. It is drawn instead of a
	// word, and turned while a fetch runs, so it is parsed once at startup
	// like the other marks.
	refreshMark *ui.SVG
	// The history's kind marks: what a recorded copy is drawn as, in the
	// tile at the head of its row. They are parsed once for the same reason
	// the panel's marks are.
	historyText  *ui.SVG
	historyImage *ui.SVG
	historyLink  *ui.SVG
	historyCode  *ui.SVG
	// settingsMark and quitMark are the quick window footer's two actions,
	// parsed once like the rest: they are marks rather than words, and what
	// they mean is their label.
	settingsMark *ui.SVG
	quitMark     *ui.SVG
	// copyMark, pinMark and closeMark are the translation window's actions.
	// They are parsed once for the same reason as the rest: a view is built
	// several times per frame, and parsing a document is not a view's work.
	copyMark  *ui.SVG
	pinMark   *ui.SVG
	closeMark *ui.SVG
	// translationMark heads the settings window's translation section: the
	// design gives it a mark of its own, and it is the application's own
	// drawing rather than a brand's.
	translationMark *ui.SVG
	// keyMark and infoMark are the settings window's sidebar's other two
	// marks. Every entry draws the mark its own section carries — the key,
	// the translation mark, the clipboard, the gear, the info circle
	// (§2.1 of the design system) — and these are the two the page did not have
	// before the sidebar named every section at once. They are parsed once
	// at startup like every other mark.
	keyMark  *ui.SVG
	infoMark *ui.SVG

	tray *mygo.Tray
	// panel is the open quick window; panelPopover records whether it was
	// created as the system popover (自动) or as the application's own card
	// (a fixed appearance), which is what tells a theme change whether the
	// open window can be repainted or has to be closed.
	panel             *mygo.Window
	panelPopover      bool
	panelInjectedDark bool
	settingsWindow    *mygo.Window // the open settings window, or nil
	historyWindow     *mygo.Window // the open clipboard history window, or nil
	// translateWindow is the translation card, created the first time the
	// shortcut is pressed and hidden afterwards.
	translateWindow cardWindow
	// cardWindows builds the translation card. A test supplies its own, so
	// that what the card does can be exercised without a window.
	cardWindows func(onClosed, onBlur, onFocus func()) cardWindow

	// form is the settings window's state, on the main thread like the
	// rest.
	form connectForm
	// clipboardStatus is the last thing the clipboard switches said, shown
	// under them.
	clipboardStatus string
	// translateNotice is the last thing the translation switches said,
	// shown under them.
	translateNotice string
	// ownServices registers a translator for every service the user runs
	// themselves, so that one they just added from this window is one the
	// translation tool can ask. It is nil in a build with no assembly behind
	// it, and a window that has nothing to register says so rather than
	// panicking.
	ownServices func() error
	// panelStatus is the last thing a panel switch said, shown under the
	// switches in the general section.
	panelStatus string
	// loginNotice is what the last attempt to start a credential's own login
	// said. It belongs to the panel whose credential comes from a tool: the
	// button is there, and failing to start the login has to be visible.
	loginNotice string
	// delayInput is the close-delay field as typed. It is a string rather
	// than a number because a field is built several times per frame, and
	// what the user has typed is what it has to keep.
	delayInput string
	// settingsScroll is how far the settings page is scrolled. The sidebar
	// owns it: clicking an entry moves it to that section, and reading it is
	// what tells the sidebar which chapter the user is looking at. It is a
	// ScrollState rather than a number because MyGo's scroll container keeps
	// its place there.
	settingsScroll ui.ScrollState
	// settingsSection is the section the sidebar says is open: the last one
	// an entry was clicked for, or the one the page is showing, whichever
	// the frame can tell.
	settingsSection settingsSectionKind
	// settingsReveal is the section the sidebar asked for and the page has
	// not reached yet, or nil when the sidebar is not the one moving the
	// page. The entry the user clicked is the open one for the whole time the
	// page is travelling, so that it does not flicker back to a section the
	// page is passing on the way.
	settingsReveal *settingsSectionKind
	// settingsAiming says that the sidebar has sent the page somewhere and the
	// scroll it was given has not stopped yet, which is what keeps the entry
	// the user clicked the open one for the whole travel.
	settingsAiming bool
	// settingsAimAt is the section that travel is aimed at, so that a second
	// click while the page is moving is a new destination rather than another
	// aim at the same one.
	settingsAimAt settingsSectionKind
	// settingsRoom is how tall the room the page scrolls in is, as the last
	// frame laid it out. The window is taller than that: the footer is under
	// the page, and a section's heading is read against the middle of the
	// room rather than the middle of the window.
	settingsRoom float32
	// translation is the translation card's state.
	translation translationState
	// translationHeight is how tall the card is, which is what it measured
	// the last time it drew itself rather than a number chosen here.
	translationHeight int
	// cardHeight is what the card measured as it drew, in points.
	cardHeight int
	// cardMeasuring keeps the measurement from resizing the window from
	// inside the frame that is being measured.
	cardMeasuring bool
	// cardSettle is how many more frames the card owes its own measurement.
	// What a frame reads from the card is what the frame before it laid out
	// (MyGo's Element.Bounds is the previous frame's box), so a card whose
	// content has just changed needs a frame after the change to be measured
	// as it now is, and one more after the window takes the size that
	// measurement asked for. Without them the window keeps the height it had
	// — the stale measurement is exactly that height — and never asks for
	// another frame. See measureCard.
	cardSettle int
	// cursor and cursorSet are where the pointer was when the shortcut was
	// pressed. A point given in advance stands in for the platform's own,
	// which a test has no way to ask for: the screen module needs a running
	// application.
	cursor    mygo.Point
	cursorSet bool
	// translateCloseTimer is the close the card asked for and has not
	// reached yet, or nil.
	translateCloseTimer *time.Timer
	// shortcut is what became of the translation shortcut.
	shortcut shortcutState
	// selectionGranted says macOS' Accessibility permission is already
	// granted. The settings page hides the request note and button while it
	// is true; it is refreshed when the app becomes active, when the settings
	// window is focused or opened, and after a request.
	selectionGranted bool
	// copiedEntry is the row that was last clicked in the history, and
	// copiedAt is when: the row says so for a moment.
	copiedEntry string
	copiedAt    time.Time
	// detailsOpen is whether the quick window shows the panel's detail rows
	// — the smaller numbers under the headline — which stay folded away
	// until they are asked for.
	detailsOpen bool
}

// New builds the tray icon and the state of the application. The caller
// must still run Setup from mygo.App.WhenReady, once MyGo's event loop is
// up: no window or tray can exist before that.
func New(app *mygo.Application, deps Deps) (*State, error) {
	if app == nil {
		return nil, fmt.Errorf("app: no application")
	}
	if deps.Panels == nil {
		return nil, fmt.Errorf("app: no panel service")
	}
	if deps.Secrets == nil {
		return nil, fmt.Errorf("app: no secret service")
	}
	// A state built by a test that does not care about the theme still has
	// one: following the desktop is what a user who never chose gets.
	appearance := deps.Appearance
	if appearance == nil {
		appearance = NewAppearanceSection()
	}
	trayIcon, err := assets.Icon(trayIconSize)
	if err != nil {
		return nil, err
	}
	// The blocks of the quick window carry their provider's mark, so the two
	// the repository has are parsed here with the rest.
	deepseekMark, err := ui.ParseSVG(assets.DeepSeek())
	if err != nil {
		return nil, fmt.Errorf("app: parse the DeepSeek mark: %w", err)
	}
	gaugeMark, err := ui.ParseSVG(assets.Gauge())
	if err != nil {
		return nil, fmt.Errorf("app: parse the placeholder mark: %w", err)
	}
	clipMark, err := ui.ParseSVG(assets.Clipboard())
	if err != nil {
		return nil, fmt.Errorf("app: parse the clipboard mark: %w", err)
	}
	refreshMark, err := ui.ParseSVG(assets.Refresh())
	if err != nil {
		return nil, fmt.Errorf("app: parse the refresh mark: %w", err)
	}
	historyText, err := ui.ParseSVG(assets.HistoryText())
	if err != nil {
		return nil, fmt.Errorf("app: parse the history text mark: %w", err)
	}
	historyImage, err := ui.ParseSVG(assets.HistoryImage())
	if err != nil {
		return nil, fmt.Errorf("app: parse the history image mark: %w", err)
	}
	historyLink, err := ui.ParseSVG(assets.HistoryLink())
	if err != nil {
		return nil, fmt.Errorf("app: parse the history link mark: %w", err)
	}
	historyCode, err := ui.ParseSVG(assets.HistoryCode())
	if err != nil {
		return nil, fmt.Errorf("app: parse the history code mark: %w", err)
	}
	settingsMark, err := ui.ParseSVG(assets.Settings())
	if err != nil {
		return nil, fmt.Errorf("app: parse the settings mark: %w", err)
	}
	quitMark, err := ui.ParseSVG(assets.Quit())
	if err != nil {
		return nil, fmt.Errorf("app: parse the quit mark: %w", err)
	}
	copyMark, err := ui.ParseSVG(assets.Copy())
	if err != nil {
		return nil, fmt.Errorf("app: parse the copy mark: %w", err)
	}
	pinMark, err := ui.ParseSVG(assets.Pin())
	if err != nil {
		return nil, fmt.Errorf("app: parse the pin mark: %w", err)
	}
	closeMark, err := ui.ParseSVG(assets.Close())
	if err != nil {
		return nil, fmt.Errorf("app: parse the close mark: %w", err)
	}
	// The translation section's mark is the application's own drawing, not a
	// brand's: it heads a section of Boku's settings rather than standing for
	// a service.
	translationMark, err := ui.ParseSVG(assets.Translate())
	if err != nil {
		return nil, fmt.Errorf("app: parse the translation mark: %w", err)
	}
	// The sidebar reaches every section, including the two that had no mark
	// of their own: the key section and the about box. The key is the
	// design's own drawing for the page that configures services, and the
	// info mark is the one it draws beside the other one.
	keyMark, err := ui.ParseSVG(assets.Key())
	if err != nil {
		return nil, fmt.Errorf("app: parse the key mark: %w", err)
	}
	infoMark, err := ui.ParseSVG(assets.Info())
	if err != nil {
		return nil, fmt.Errorf("app: parse the info mark: %w", err)
	}
	state := &State{
		app:               app,
		panelService:      deps.Panels,
		secretService:     deps.Secrets,
		clipboard:         deps.Clipboard,
		translate:         deps.Translate,
		ownServices:       deps.RegisterOwnServices,
		shortcuts:         deps.Shortcuts,
		version:           deps.Version,
		quit:              deps.Quit,
		appearance:        appearance,
		appearanceStore:   deps.AppearanceStore,
		applyAppearance:   deps.ApplyAppearance,
		popoverAppearance: deps.PopoverAppearance,
		autostart:         deps.Autostart,
		updates:           deps.Updates,
		dark:              appearance.Dark(false),
		selectionGranted:  true,
		trayIcon:          trayIcon,
		deepseekMark:      deepseekMark,
		gaugeMark:         gaugeMark,
		clipboardMark:     clipMark,
		refreshMark:       refreshMark,
		historyText:       historyText,
		historyImage:      historyImage,
		historyLink:       historyLink,
		historyCode:       historyCode,
		settingsMark:      settingsMark,
		quitMark:          quitMark,
		copyMark:          copyMark,
		pinMark:           pinMark,
		closeMark:         closeMark,
		translationMark:   translationMark,
		keyMark:           keyMark,
		infoMark:          infoMark,
	}
	// The update service was built before the state, so its change listener
	// is installed now, once, before anything can ask it for a check: a
	// service without one is a row that never redraws.
	state.prepareUpdates(deps.Updates)
	return state, nil
}

// AttachUpdates gives the state its update service, or replaces the one it
// has. It is what an assembly calls when the service is built after — or on
// — the state: the service's change listener may fire from another goroutine,
// and installing it once the state exists means every report reaches a state
// that can be repainted.
//
// A service passed to New installs the same listener there, so an assembly
// only needs one of the two. A State with neither simply has no updates, and
// everything about updating is then absent rather than broken.
func (s *State) AttachUpdates(updates UpdateService) {
	s.prepareUpdates(updates)
}

// Setup adds the tray icon and wires its clicks. It must be called on the
// main thread, once the application is ready.
func (s *State) Setup() error {
	if s.tray != nil {
		return fmt.Errorf("app: the tray already exists")
	}
	tray, err := mygo.NewTray(mygo.TrayOptions{
		Icon: s.trayIcon,
		// A black-and-transparent image lets macOS tint its menu-bar icon.
		// Windows and Linux ignore this flag and retain the PNG's colors.
		IconIsTemplate: runtime.GOOS == "darwin",
		ToolTip:        toolTip,
		// No Menu: MyGo shows a menu on a left click whenever the tray has
		// one, which would make the left click unable to open the panel.
		// The menu is set for the duration of a right click only.
	})
	if err != nil {
		return fmt.Errorf("app: create the tray: %w", err)
	}
	s.tray = tray
	s.logTrayBounds()
	s.RefreshSelectionAccess()
	if runtime.GOOS == "linux" {
		// An AppIndicator can only show a menu and reports no clicks, so
		// the menu is what opens the panel there.
		tray.SetMenu(s.menu())
		return nil
	}
	tray.OnClick(s.Toggle)
	tray.OnRightClick(s.popUpMenu)
	return nil
}

// trayDebugEnv makes Setup report where the system thinks the tray icon is.
//
// Do not read a zero rectangle as "the icon was never added". MyGo asks
// Shell_NotifyIconGetRect, which returns E_FAIL for every tray icon on
// Windows 11 — measured on 2025-11 hardware against Chrome, Steam, PowerToys,
// Raycast and the app window alike. The rectangle is worth having when it is
// non-zero; zeros mean "no answer", not "no icon". The system's own record of
// a registered icon is the NotifyIconSettings key under HKCU\Control Panel,
// which gets one entry per icon that registers, including hidden ones.
const trayDebugEnv = "BOKU_TRAY_DEBUG"

func (s *State) logTrayBounds() {
	if os.Getenv(trayDebugEnv) == "" {
		return
	}
	r := s.tray.Bounds()
	log.Printf("tray: bounds x=%d y=%d w=%d h=%d (zeros mean the shell did not "+
		"answer; see the comment on trayDebugEnv)", r.X, r.Y, r.Width, r.Height)
}

// Destroy takes the tray icon away. MyGo calls it on the main thread while
// the application is quitting; leaving the icon behind makes the system
// show a dead one until the pointer moves over it.
func (s *State) Destroy() {
	if s.tray != nil {
		s.tray.Destroy()
		s.tray = nil
	}
}

// Refresh asks the open windows for a frame. It is what a background answer
// calls — a new clipboard reading, a switch that was flipped — and it is
// safe to call from another goroutine: Window.Update carries it to the main
// thread, which is the only thread that may touch this state.
func (s *State) Refresh() {
	s.refresh(nil)
}

// refresh is Refresh with work of the caller's to run on the main thread
// after the repaint has been asked for: a goroutine that has an answer to
// hand over gives it here as a closure rather than touching the state itself.
//
// The windows are MyGo's own way to the main thread (Window.Update posts to
// it and returns), so the first window present carries the work and the
// others get no repaint. With no window open there is nothing to carry it,
// and dropping it would lose what the caller could not do on its own — a
// notice about an update that finished while the quick window was closed, for
// instance. It is posted on a goroutine instead, which is safe because the
// work does nothing but ask MyGo to show a notice: that request is itself
// carried to the main thread by the framework, and nothing in it touches the
// state.
func (s *State) refresh(after func()) {
	if s.panel != nil {
		s.panel.Update(refreshCallback(after))
		after = nil
	}
	if s.settingsWindow != nil {
		s.settingsWindow.Update(refreshCallback(after))
		after = nil
	}
	if s.historyWindow != nil {
		s.historyWindow.Update(refreshCallback(after))
		after = nil
	}
	if s.translateWindow != nil {
		s.translateWindow.Update(refreshCallback(after))
		after = nil
	}
	if after != nil {
		go after()
	}
}

// refreshCallback turns a repaint-only request into a callable no-op. MyGo's
// Window.Update always invokes its argument, so nil is not a valid way to ask
// for just a redraw. The first open window receives after; every later window
// receives a no-op, preserving the rule that extra work runs exactly once.
func refreshCallback(after func()) func() {
	if after != nil {
		return after
	}
	return func() {}
}

// menu is the tray menu: the only way out of a tray application, which has
// no main window to close.
func (s *State) menu() *mygo.Menu {
	items := []*mygo.MenuItem{
		{Label: "打开 " + appName, Click: func(*mygo.MenuItem, *mygo.Window) { s.Toggle() }},
		{Label: "设置…", Click: func(*mygo.MenuItem, *mygo.Window) { s.openSettings() }},
	}
	// A ready update earns a line here, between the settings and the way
	// out: the menu is the one place reachable while the application is
	// behind another window. Every other status earns nothing — an item that
	// can never do anything is noise in a four-line menu.
	items = append(items, s.updateMenuItems()...)
	items = append(items,
		mygo.Separator(),
		&mygo.MenuItem{Label: "退出 " + appName, Click: func(*mygo.MenuItem, *mygo.Window) { s.quit() }},
	)
	return mygo.NewMenu(items)
}

// popUpMenu shows the menu next to the icon for this right click only, as
// the framework's menubar example does: a menu left set would turn every
// left click into a menu, and the left click is what opens the panel.
func (s *State) popUpMenu() {
	if s.panel != nil {
		s.panel.Close()
	}
	s.tray.SetMenu(s.menu())
	defer s.tray.SetMenu(nil)
	s.tray.PopUpMenu()
}

// Toggle opens the quick window next to the tray icon, or closes it when it
// is already open. It is what the tray click, the menu item and the global
// shortcut call, so it must return at once and never wait on anything.
func (s *State) Toggle() {
	if s.panel != nil {
		s.panel.Close()
		return
	}
	s.Open()
}

// Activate brings the quick window forward: it is what a second launch of
// the application asks for, since the running instance owns the tray and has
// no window of its own to raise. The window already open is focused rather
// than opened again, so a second launch never leaves two of them behind.
//
// A tray that does not exist yet is one nothing can be anchored to: MyGo's
// NewFlyout refuses a flyout without a tray or a parent, and a second launch
// arriving while the application is still starting has nothing to show.
func (s *State) Activate() {
	if s.tray == nil {
		return
	}
	if s.panel != nil {
		s.panel.Focus()
		return
	}
	s.Open()
}

// quickWindowUsesPopover reports whether the quick window can be the system
// popover. It can in the automatic appearance, whose material follows the
// desktop and therefore matches Boku's page. A fixed dark appearance can use
// it only with the injected dark material; a fixed light one draws the
// application's own card, because a native popover on a dark desktop would be
// dark behind a light page.
func (s *State) quickWindowUsesPopover() bool {
	switch s.appearance.Mode() {
	case AppearanceSystem:
		// The material follows the desktop, and so does the page.
		return true
	case AppearanceDark:
		// The material does not follow a forced appearance; the injected
		// appearance is what makes the native popover usable here.
		return s.popoverAppearance != nil
	default:
		// A forced light appearance keeps the application's card: the
		// platform bridge is deliberately only for dark, and a native
		// popover on a dark desktop would be dark behind a light page.
		return false
	}
}

// quickWindowPlacement is where the quick window goes from the tray icon. A
// popover centres its arrow on the icon; the application's card centres itself
// on macOS for the same reason, and keeps the established end alignment where
// it is the normal shape.
func quickWindowPlacement(goos string, popover bool) mygo.Placement {
	if goos == "darwin" && !popover {
		return mygo.PlacementTop
	}
	return mygo.PlacementTopEnd
}

// Open shows a new quick window next to the tray icon.
//
// The window is created again on every open instead of being kept hidden:
// a flyout is placed against the tray when it is created, so a reused one
// would stay where it was first shown.
func (s *State) Open() {
	popover := s.quickWindowUsesPopover()
	panel := mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:      s.tray,
		Placement: quickWindowPlacement(runtime.GOOS, popover),
		Gap:       4,
		Width:     panelWidth,
		Height:    panelHeight,
		// Focusable: a click outside closes it, as the system's menus do.
		Focusable: true,
		Shadow:    true,
		// Popover: on macOS it is an NSPopover, which draws the material,
		// the corners and the arrow pointing at the icon. It is used in the
		// automatic appearance and, with the injected dark material, in a
		// fixed dark one; a fixed light one draws the application's card.
		// Elsewhere the option is ignored and the view draws its own
		// background either way.
		Popover: popover,
		Content: ui.View(s.View),
	})
	s.panel = panel
	s.panelPopover = popover
	s.syncPopoverAppearance()
	panel.OnClosed(func() {
		if s.panel == panel {
			s.panel = nil
			s.panelPopover = false
			s.panelInjectedDark = false
		}
	})
	// Opening the window is the moment to refresh, and every tray click asks
	// again whatever the cache says: the numbers a click shows should be the
	// numbers now, not the ones the previous click left behind. The request
	// runs off the main thread; the view never starts work of its own,
	// because MyGo builds it several times per frame.
	s.refreshAsync(true)
}

// View builds one frame of the quick window: the panel, the clipboard line
// under it and the footer. It has no title bar of its own — a flyout hangs
// from the tray icon, and the window it opens is what names the application —
// so the content starts at the top of the window.
//
// The window is drawn on the application's design (design.go) rather than on
// the framework's default theme: the panel reads as the first section of a
// page, which is what it is.
func (s *State) View(c *ui.Context) {
	s.useDesign(c)
	c.Root().Background(ui.Transparent)
	// A frame is where a ready update is noticed too, on the same decision
	// the service's own change reports go through (see refreshReadyUpdate):
	// the notice is shown once per version, so whichever of the two sees it
	// first, the other finds nothing left to do.
	s.refreshReadyUpdate()
	box := ui.Column(c).Fill()
	switch {
	case c.Vibrancy(): // over the popover's own material on macOS
	case runtime.GOOS == "windows": // Windows 11 rounds and outlines the flyout
		box.Background(colorSurface)
	case runtime.GOOS == "darwin":
		// A fixed appearance draws the application's card on macOS: the
		// system draws the shadow and outlines the card with a hairline, so
		// it draws the face and its corner and no second border.
		box.Background(colorSurface).Radius(radiusCard)
	default:
		box.Background(colorSurface).Radius(radiusCard).Border(1, colorBorder)
	}
	box.Children(func() {
		body := ui.Scroll(c).FillWidth().Grow(1)
		body.Children(func() {
			// Every block brings its own room with it, and the line between
			// two of them is the window's: the design's blocks are the
			// window, not rows inside one margin.
			s.card(c)
		})
		// The clipboard row closes the content: it is about the desktop
		// rather than about a panel, and it belongs at the far end of the
		// window from the numbers. The line under it is the window's own —
		// it runs from edge to edge — and the footer keeps its own room
		// inside that.
		s.clipboardBottom(c)
		quickEdgeLine(c)
		quickWindowFooter(c, s.versionLine(), s.flyoutActions)
	})
}

// clipboardBottom draws the quick window's clipboard region, when the user
// asked for it. Without the switch the region is not there at all — no
// divider, no empty line — so a window that never wanted it is unchanged.
func (s *State) clipboardBottom(c *ui.Context) {
	if s.clipboard == nil {
		return
	}
	if !s.clipboard.Settings().RegionVisible {
		return
	}
	s.clipboardRegion(c)
}

// flyoutActions is the quick window's footer: everything the user can always
// do from here. The two actions are marks rather than words — the footer is one
// line at the bottom of a card, and a word there reads as another piece of
// content while a mark reads as what it is. Each keeps its label for the
// pointer that rests on it and for assistive technology.
func (s *State) flyoutActions(c *ui.Context) {
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(float32(4)).Children(func() {
		if button := quietIconButton(c, s.settingsMark, "设置"); button.Clicked() {
			s.openSettings()
		}
		if button := quietIconButton(c, s.quitMark, "退出"); button.Clicked() {
			s.quit()
		}
	})
}

// versionLine is what the footer shows on the left.
func (s *State) versionLine() string {
	version := strings.TrimSpace(s.version)
	if version == "" {
		version = "dev"
	}
	return appName + " " + version
}

// refreshAsync fetches the listed panels away from the main thread.
//
// force asks for a refresh whatever the cache says; otherwise a panel is only
// asked when its snapshot is missing, stale or from a failed attempt. Each
// panel runs in its own goroutine, so one slow provider cannot hold up
// another's numbers. The answers are not carried back here: the use case
// keeps them, and the repaint below makes the view read them. That is why a
// refresh that finishes while the window is closed loses nothing.
func (s *State) refreshAsync(force bool) {
	for _, panel := range s.panelService.List() {
		s.refreshPanelAsync(panel.ID, force)
	}
}

// refreshPanelAsync fetches one panel away from the main thread: the panel
// whose own refresh action was pressed, and the panel whose block is showing
// a failure.
func (s *State) refreshPanelAsync(panelID string, force bool) {
	panels := s.panelService
	window := s.panel
	if !force && !panels.NeedsRefresh(panelID) {
		return
	}
	go func() {
		ctx := context.Background()
		// What a refresh found, and what it failed with, is written down
		// here: a card can only say a little about a wrong key, a timeout or
		// a service that answered something else, and it is gone when the
		// window closes. The log keeps it.
		var (
			snapshot domain.Snapshot
			asked    = true
			err      error
		)
		if force {
			snapshot, err = panels.Refresh(ctx, panelID)
		} else {
			snapshot, asked, err = panels.RefreshIfNeeded(ctx, panelID)
		}
		switch {
		case err != nil:
			slog.Warn("the panel could not be refreshed", "panel", panelID, "error", err)
		case asked:
			slog.Info("the panel was refreshed", "panel", panelID, "status", string(snapshot.Status))
		}
		if window != nil {
			// Window.Update runs on the main thread; an empty one only
			// asks for the frame that shows the new answer.
			window.Update(func() {})
		}
	}()
	// The frame the request needs while it is in flight, rather than only the
	// one its answer lands in: the mark by the title turns for as long as a
	// fetch runs, and nothing else would ask for the frames that turn it.
	if window != nil {
		window.Update(func() {})
	}
}
