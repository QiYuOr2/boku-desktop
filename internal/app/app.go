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

	animeui "boku/internal/anime/ui"
	"boku/internal/app/assets"
	bilibiliui "boku/internal/bilibili/ui"
	"boku/internal/clipboard"
	clipboardui "boku/internal/clipboard/ui"
	"boku/internal/credential"
	credentialui "boku/internal/credential/ui"
	"boku/internal/domain"
	"boku/internal/flash"
	flashui "boku/internal/flash/ui"
	"boku/internal/panel"
	panelui "boku/internal/panel/ui"
	"boku/internal/platform/autostart"
	"boku/internal/playtime"
	playtimeui "boku/internal/playtime/ui"
	"boku/internal/preferences"
	preferencesui "boku/internal/preferences/ui"
	"boku/internal/presentation"
	"boku/internal/translate"
	translateui "boku/internal/translate/ui"
	updateui "boku/internal/update/ui"
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
	// quickTaskRowHeight is the reserved vertical room for one image task in
	// the tray flyout. The actual rows are content-driven; this conservative
	// estimate lets the flyout grow before its scroll body needs to scroll.
	quickTaskRowHeight = 52
	// quickEnabledHeightShare is how tall the flyout opens when a reading block
	// — today's animation releases or the followed UPs — is switched on.
	//
	// The window is not resized to its content: measuring the content from the
	// view turned out to follow the scroll offset, so dragging the scrollbar
	// made the window change height under the pointer. A share of the work
	// area is decided before anything is drawn, so it cannot move while the
	// user is reading.
	quickEnabledHeightShare = 0.30
	// trayIconSize is the icon in device pixels: 16 points on a Retina
	// display, which MyGo scales to the screen's density on Windows and
	// Linux.
	trayIconSize = 32
)

// Deps is what the presentation layer is assembled from, by main.
type Deps struct {
	// Panels reads and refreshes the panels.
	Panels *panel.Panels
	// Anime reads today's releases from the configured public watching list.
	Anime AnimeService
	// Bilibili reads public activity and live state for explicitly followed UPs.
	Bilibili BilibiliService
	// Secrets stores the credentials those panels need.
	Secrets *credential.Secrets
	// Clipboard reads and records the system clipboard. It is what the
	// quick window's clipboard line and the history window show; a State
	// without one draws neither.
	Clipboard *clipboard.Clipboard
	// Translate is the selection-translation tool. A State without one
	// draws no translation window and binds no shortcut for it.
	Translate *translate.Translate
	// Flash is the text-flash document. A State without one draws no capture
	// line and no history.
	Flash *flash.Service
	// Screenshot opens the platform region selector and returns the chosen PNG.
	Screenshot domain.ScreenshotDevice
	// RegionCapture reads physical desktop pixels. When present, the app
	// supplies its own cross-platform selection overlay and does not invoke a
	// system screenshot UI or use the clipboard.
	RegionCapture RegionCapture
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
	AppearanceStore        AppearanceStore
	QuickWindowLayout      *preferences.QuickWindowLayout
	QuickWindowLayoutStore preferences.LayoutStore
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
	// Playtime is the tray readout of recorded sessions. Nil draws nothing,
	// which is every test that does not care about it.
	Playtime *playtime.Recorder
	// Updates is the update check. A State without one draws no update row,
	// starts no check and adds nothing to the tray menu: a build without an
	// updater is every test and every assembly that wired none.
	//
	// The interface is the presentation's own, so that nothing here imports
	// internal/update or MyGo's updater. main injects the real service; a
	// test injects a fake.
	Updates UpdateService
}

// AnimeService is the small presentation-facing contract of the animation
// feature. Defining it here keeps app independent of the feature package.
type AnimeService interface {
	Enabled() bool
	Username() string
	RefreshMinutes() int
	Configure(context.Context, bool, string, int) error
	View() ([]domain.EpisodeRelease, time.Time, bool, error)
	Refresh(context.Context) error
	RefreshIfNeeded(context.Context) error
	Cover(subjectID int64) []byte
}

// BilibiliService is the small presentation-facing contract of the UP
// monitoring feature. It keeps app independent of the feature package.
type BilibiliService interface {
	Enabled() bool
	UIDs() []int64
	RefreshMinutes() int
	DynamicCount() int
	Notifications() (live, dynamic bool)
	Configure(context.Context, bool, []int64, int, int, bool, bool) error
	View() ([]domain.FollowedUpdate, time.Time, bool, error)
	Refresh(context.Context) error
	RefreshIfNeeded(context.Context) error
	// Avatar returns the fetched avatar bytes of one creator, or nil.
	Avatar(int64) []byte
}

// State is the whole presentation state. It is read and written on the main
// thread only.
type State struct {
	app               *mygo.Application
	panelService      *panel.Panels
	panelUI           *panelui.State
	animeService      AnimeService
	bilibiliService   BilibiliService
	animeUI           *animeui.ViewModel
	bilibiliUI        *bilibiliui.ViewModel
	secretService     *credential.Secrets
	credentialUI      *credentialui.State
	clipboard         *clipboard.Clipboard
	playtime          *playtime.Recorder
	playtimePanel     *playtimeui.Panel
	clipboardUI       *clipboardui.State
	translate         *translate.Translate
	translationUI     *translateui.State
	flashUI           *flashui.State
	version           string
	quit              func()
	appearance        *Appearance
	appearanceStore   AppearanceStore
	applyAppearance   func(AppearanceMode)
	popoverAppearance PopoverAppearance
	preferencesUI     *preferencesui.State
	layoutUI          *preferencesui.LayoutState
	quickWindowLayout *preferences.QuickWindowLayout
	layoutMark        *ui.SVG
	updates           UpdateService
	notifier          desktopNotifier
	updateUI          *updateui.State
	dark              bool
	systemDark        bool
	trayIcon          []byte
	deepseekMark      *ui.SVG
	gaugeMark         *ui.SVG
	tvMark            *ui.SVG
	bilibiliMark      *ui.SVG
	chevronRightMark  *ui.SVG
	clipboardMark     *ui.SVG
	refreshMark       *ui.SVG
	historyText       *ui.SVG
	historyImage      *ui.SVG
	historyLink       *ui.SVG
	historyCode       *ui.SVG
	settingsMark      *ui.SVG
	quitMark          *ui.SVG
	flashMark         *ui.SVG
	screenshotMark    *ui.SVG
	qrMark            *ui.SVG
	copyMark          *ui.SVG
	pinMark           *ui.SVG
	closeMark         *ui.SVG
	translationMark   *ui.SVG
	keyMark           *ui.SVG
	infoMark          *ui.SVG
	tray              *mygo.Tray
	panel             *mygo.Window
	panelPopover      bool
	panelInjectedDark bool
	settingsWindow    *mygo.Window
	historyWindow     *mygo.Window
	flashCapture      *mygo.Window
	flashHistory      *mygo.Window
	ownServices       func() error
	panelStatus       string
	settingsScroll    ui.ScrollState
	settingsSection   settingsSectionKind
	settingsReveal    *settingsSectionKind
	settingsAiming    bool
	settingsAimAt     settingsSectionKind
	settingsRoom      float32
}

// RegionCapture is the narrow platform boundary behind the app-owned region
// picker. The rectangle is in the coordinate space the platform's reader
// takes: screen points on macOS, physical virtual-desktop pixels on Windows.
type RegionCapture = translateui.RegionCapture

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
	codexMark, err := ui.ParseSVG(assets.Codex())
	if err != nil {
		return nil, fmt.Errorf("app: parse the Codex mark: %w", err)
	}
	tvMark, err := ui.ParseSVG(assets.TV())
	if err != nil {
		return nil, fmt.Errorf("app: parse the TV mark: %w", err)
	}
	bilibiliMark, err := ui.ParseSVG(assets.TVBilibili())
	if err != nil {
		return nil, fmt.Errorf("app: parse the Bilibili mark: %w", err)
	}
	chevronRightMark, err := ui.ParseSVG(assets.ChevronRight())
	if err != nil {
		return nil, fmt.Errorf("app: parse the chevron-right mark: %w", err)
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
	flashMark, err := ui.ParseSVG(assets.Lightbulb())
	if err != nil {
		return nil, fmt.Errorf("app: parse the flash mark: %w", err)
	}
	quitMark, err := ui.ParseSVG(assets.Quit())
	if err != nil {
		return nil, fmt.Errorf("app: parse the quit mark: %w", err)
	}
	screenshotMark, err := ui.ParseSVG(assets.Screenshot())
	if err != nil {
		return nil, fmt.Errorf("app: parse the screenshot mark: %w", err)
	}
	qrMark, err := ui.ParseSVG(assets.QR())
	if err != nil {
		return nil, fmt.Errorf("app: parse the QR mark: %w", err)
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
	commandMark, err := ui.ParseSVG(assets.Command())
	if err != nil {
		return nil, fmt.Errorf("app: parse the command mark: %w", err)
	}
	returnMark, err := ui.ParseSVG(assets.Return())
	if err != nil {
		return nil, fmt.Errorf("app: parse the return mark: %w", err)
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
	layoutMark, err := ui.ParseSVG(assets.Layout())
	if err != nil {
		return nil, fmt.Errorf("app: parse the layout mark: %w", err)
	}
	eyeMark, err := ui.ParseSVG(assets.Eye())
	if err != nil {
		return nil, fmt.Errorf("app: parse the eye mark: %w", err)
	}
	eyeOffMark, err := ui.ParseSVG(assets.EyeOff())
	if err != nil {
		return nil, fmt.Errorf("app: parse the hidden eye mark: %w", err)
	}
	gripMark, err := ui.ParseSVG(assets.Grip())
	if err != nil {
		return nil, fmt.Errorf("app: parse the drag mark: %w", err)
	}
	steamMark, err := ui.ParseSVG(assets.Steam())
	if err != nil {
		return nil, fmt.Errorf("app: parse the playtime mark: %w", err)
	}
	dualSenseMark, err := ui.ParseSVG(assets.DualSense())
	if err != nil {
		return nil, fmt.Errorf("app: parse the DualSense mark: %w", err)
	}
	switchProMark, err := ui.ParseSVG(assets.SwitchPro())
	if err != nil {
		return nil, fmt.Errorf("app: parse the Switch Pro mark: %w", err)
	}
	xboxMark, err := ui.ParseSVG(assets.Xbox())
	if err != nil {
		return nil, fmt.Errorf("app: parse the Xbox mark: %w", err)
	}
	quickLayout := deps.QuickWindowLayout
	if quickLayout == nil {
		quickLayout = preferences.NewQuickWindowLayout()
	}
	state := &State{
		app:               app,
		panelService:      deps.Panels,
		animeService:      deps.Anime,
		bilibiliService:   deps.Bilibili,
		animeUI:           animeui.New(deps.Anime),
		bilibiliUI:        bilibiliui.New(deps.Bilibili),
		secretService:     deps.Secrets,
		clipboard:         deps.Clipboard,
		playtime:          deps.Playtime,
		playtimePanel:     &playtimeui.Panel{},
		translate:         deps.Translate,
		ownServices:       deps.RegisterOwnServices,
		version:           deps.Version,
		quit:              deps.Quit,
		appearance:        appearance,
		appearanceStore:   deps.AppearanceStore,
		applyAppearance:   deps.ApplyAppearance,
		popoverAppearance: deps.PopoverAppearance,
		updates:           deps.Updates,
		notifier:          mygoNotifier{},
		dark:              appearance.Dark(false),
		trayIcon:          trayIcon,
		deepseekMark:      deepseekMark,
		gaugeMark:         gaugeMark,
		tvMark:            tvMark,
		bilibiliMark:      bilibiliMark,
		chevronRightMark:  chevronRightMark,
		clipboardMark:     clipMark,
		refreshMark:       refreshMark,
		historyText:       historyText,
		historyImage:      historyImage,
		historyLink:       historyLink,
		historyCode:       historyCode,
		settingsMark:      settingsMark,
		quitMark:          quitMark,
		flashMark:         flashMark,
		screenshotMark:    screenshotMark,
		qrMark:            qrMark,
		copyMark:          copyMark,
		pinMark:           pinMark,
		closeMark:         closeMark,
		translationMark:   translationMark,
		keyMark:           keyMark,
		infoMark:          infoMark,
		layoutMark:        layoutMark,
		quickWindowLayout: quickLayout,
	}
	// The update service was built before the state, so its change listener
	// is installed now, once, before anything can ask it for a check: a
	// service without one is a row that never redraws.
	var connections credentialui.Connections
	if deps.Translate != nil {
		connections = deps.Translate
	}
	state.credentialUI = credentialui.New(connections, deps.Secrets, state.featureHost(), func() error {
		if state.ownServices != nil {
			return state.ownServices()
		}
		return nil
	}, func() { state.refreshAsync(true) }, keyMark)
	state.translationUI = translateui.New(translateui.Config{Service: deps.Translate, Clipboard: deps.Clipboard, Screenshot: deps.Screenshot, RegionCapture: deps.RegionCapture, Shortcuts: deps.Shortcuts, Host: state.featureHost(), Dark: func() bool { return state.dark }, Notifier: state.notifier, ConnectionsBusy: func() bool { return state.credentialUI.Form.Busy != "" }, CopyMark: copyMark, PinMark: pinMark, CloseMark: closeMark, ScreenshotMark: screenshotMark, TranslationMark: translationMark})
	state.panelUI = panelui.New(deps.Panels, state.featureHost(), state.openSettings, deepseekMark, codexMark, gaugeMark, refreshMark)
	state.preferencesUI = preferencesui.New(deps.Autostart, state.featureHost())
	state.layoutUI = preferencesui.NewLayout(quickLayout, deps.QuickWindowLayoutStore, state.featureHost(), layoutMark, eyeMark, eyeOffMark, gripMark, map[string]presentation.PanelMark{
		"deepseek": state.panelUI.ProviderMark("deepseek"),
		"codex":    state.panelUI.ProviderMark("codex"),
		"anime":    {SVG: tvMark},
		"bilibili": {SVG: bilibiliMark},
		"playtime": {SVG: steamMark},
	})
	state.layoutUI.SetOffer(func(id string) bool {
		if id != "playtime" {
			return true
		}
		return state.playtime != nil && state.playtime.Available()
	})
	state.clipboardUI = clipboardui.New(deps.Clipboard, state.featureHost(), state.openHistory)
	if deps.Flash != nil {
		state.flashUI = flashui.New(flashui.Config{
			Service:     deps.Flash,
			Host:        state.featureHost(),
			CopyText:    state.copyFlashText,
			RebindAll:   state.RegisterShortcuts,
			ShowCapture: state.openFlashCapture,
			HideCapture: state.hideFlashCapture,
			Mark:        state.flashMark,
			CloseMark:   state.closeMark,
			CommandMark: commandMark,
			ReturnMark:  returnMark,
		})
		state.translationUI.SetExtraRebind(state.flashUI.Rebind)
	}
	state.bilibiliUI.AttachHost(state.featureHost())
	state.animeUI.AttachHost(state.featureHost(), deps.Secrets)
	state.prepareUpdates(deps.Updates)
	state.playtimePanel.SetControllerMarks(dualSenseMark, switchProMark, xboxMark)
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
	requestScreenRecording()
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
	s.featureHost().Refresh()
}

// featureHost is the Shell adapter used by feature UI packages. It is kept at
// the edge of app so no feature needs to import app.State or MyGo windows.
func (s *State) featureHost() presentation.Host {
	return presentation.Host{
		RequestRefresh: func() { s.refresh(nil) },
		RunOnMain: func(fn func()) {
			go mygo.RunOnMain(func() { fn(); s.Refresh() })
		},
		FitQuickWindow: s.fitQuickWindow,
		SetStatus: func(text string) {
			s.panelStatus = text
		},
	}
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
	if s.flashCapture != nil {
		s.flashCapture.Update(refreshCallback(after))
		after = nil
	}
	if s.flashHistory != nil {
		s.flashHistory.Update(refreshCallback(after))
		after = nil
	}
	if s.translationUI != nil && s.translationUI.Repaint(refreshCallback(after)) {
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
	if s.flashUI != nil {
		items = append(items,
			&mygo.MenuItem{Label: "记一条闪念", Click: func(*mygo.MenuItem, *mygo.Window) { s.openFlashCapture() }},
			&mygo.MenuItem{Label: "闪念历史", Click: func(*mygo.MenuItem, *mygo.Window) { s.openFlashHistory() }},
		)
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
		Height:    s.quickWindowHeight(),
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
	//
	// The animation service may already hold today's disk cache. Turn its
	// cover bytes into UI bitmaps before the first frame, so a cache-backed
	// opening does not flash placeholder marks while the network refresh is
	// still in flight. This is presentation-thread image decoding, not I/O.
	s.cacheAnimeCovers()
	s.CachePlaytimeCovers()
	// The followed-UP block may already hold rows from the last refresh, and
	// its avatars may already have been fetched. Turn them into bitmaps and
	// size the flyout for them before the first frame, so an opening does not
	// show placeholders that resolve a moment later.
	s.cacheBilibiliAvatars()
	s.fitQuickWindow()
	s.refreshAsync(true)
}

// quickWindowHeight sizes the flyout from what is switched on, within the
// half-screen ceiling. The fallback keeps tests without a screen deterministic.
func (s *State) quickWindowHeight() int {
	work := s.screenNear(s.cursorPoint())
	return quickWindowHeightForContent(s.translationUI.ImageTaskCount(), s.readingBlocksOn(), work.Height)
}

// readingBlocksOn reports whether a block the user reads — today's animation
// releases, or the followed UPs — is switched on. Either one opens the flyout
// at quickEnabledHeightShare of the work area, tall enough to show it without
// scrolling and without filling the screen.
func (s *State) readingBlocksOn() bool {
	if s.quickWindowLayout.Visible("anime") && s.animeService != nil && s.animeService.Enabled() && s.animeService.Username() != "" {
		return true
	}
	return s.quickWindowLayout.Visible("bilibili") && s.bilibiliService != nil && s.bilibiliService.Enabled() && len(s.bilibiliService.UIDs()) > 0
}

// quickWindowHeightForTasks is the image-task-only sizing rule. Keeping it
// independent of the desktop makes the half-screen guarantee testable without
// a live tray or display.
func quickWindowHeightForTasks(tasks, workHeight int) int {
	return quickWindowHeightForContent(tasks, false, workHeight)
}

// quickWindowHeightForContent is the whole sizing rule:
//
//   - a reading block is switched on: 30% of the work area, which shows a
//     couple of rows under the panels;
//   - otherwise: the base height, one row per image task;
//
// and never more than half the work area.
//
// The height is decided from settings and the task count alone — never from
// measured content. Measuring was the previous attempt, and it went wrong in
// the one way that matters: the boxes it read follow the scroll offset, so
// dragging the scrollbar resized the window under the user's pointer.
//
// The price is that a flyout with a reading block on and little in it can be
// taller than its rows. That is the trade the user asked for after seeing it:
// a predictable height over a moving one.
func quickWindowHeightForContent(tasks int, readingBlocks bool, workHeight int) int {
	maxHeight := workHeight / 2
	if maxHeight <= 0 {
		maxHeight = panelHeight
	}
	wanted := panelHeight + max(tasks, 0)*quickTaskRowHeight
	if readingBlocks {
		// The share is a floor, not a target: on a short display 30% can be
		// less than the base height, and a reading block must never make the
		// flyout smaller than a flyout without one.
		wanted = max(wanted, int(float64(workHeight)*quickEnabledHeightShare))
	}
	return clampInt(wanted, min(panelHeight, maxHeight), maxHeight)
}

// fitQuickWindow applies the height rule after a task is added, completed,
// retried or removed, or after a block is switched on or off. SetSize is the
// MyGo flyout API that also re-places the window next to its tray anchor.
func (s *State) fitQuickWindow() {
	if s.panel != nil {
		s.panel.SetSize(panelWidth, s.quickWindowHeight())
	}
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
		box.Background(presentation.ColorSurface)
	case runtime.GOOS == "darwin":
		// A fixed appearance draws the application's card on macOS: the
		// system draws the shadow and outlines the card with a hairline, so
		// it draws the face and its corner and no second border.
		box.Background(presentation.ColorSurface).Radius(presentation.RadiusCard)
	default:
		box.Background(presentation.ColorSurface).Radius(presentation.RadiusCard).Border(1, presentation.ColorBorder)
	}
	box.Children(func() {
		body := ui.Scroll(c).FillWidth().Grow(1)
		body.Children(func() {
			// Every block brings its own room with it, and the line between
			// two of them is the window's: the design's blocks are the
			// window, not rows inside one margin.
			s.quickLayoutBlocks(c)
			s.translationUI.QuickView(c)
		})
		// The clipboard row closes the content: it is about the desktop
		// rather than about a panel, and it belongs at the far end of the
		// window from the numbers. The line under it is the window's own —
		// it runs from edge to edge — and the footer keeps its own room
		// inside that.
		s.clipboardBottom(c)
		presentation.QuickEdgeLine(c)
		presentation.QuickWindowFooter(c, s.versionLine(), s.flyoutActions)
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
// do from here. The actions are marks rather than words — the footer is one
// line at the bottom of a card, and a word there reads as another piece of
// content while a mark reads as what it is. Each keeps its label for the
// pointer that rests on it and for assistive technology.
func (s *State) flyoutActions(c *ui.Context) {
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(float32(4)).Children(func() {
		if s.flashUI != nil {
			if button := presentation.QuietIconButton(c, s.flashMark, "闪念"); button.Clicked() {
				slog.Info("flash button clicked", "event", "flash_history_opened", "source", "quick_window")
				s.openFlashHistory()
			}
		}
		if s.translationUI.HasScreenshot() {
			if button := presentation.QuietIconButton(c, s.qrMark, "识别二维码"); button.Clicked() {
				slog.Info("QR recognition button clicked", "event", "qr_recognition_requested", "source", "quick_window")
				s.translationUI.StartQRRecognition()
			}
			if button := presentation.QuietIconButton(c, s.screenshotMark, "截图翻译"); button.Clicked() {
				slog.Info("screenshot translation button clicked", "event", "screenshot_translation_requested", "source", "quick_window")
				s.translationUI.StartScreenshotTranslation()
			}
		}
		if button := presentation.QuietIconButton(c, s.settingsMark, "设置"); button.Clicked() {
			slog.Info("settings button clicked", "event", "settings_opened", "source", "quick_window")
			s.openSettings()
		}
		if button := presentation.QuietIconButton(c, s.quitMark, "退出"); button.Clicked() {
			slog.Info("quit button clicked", "event", "quit_requested", "source", "quick_window")
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
		s.panelUI.RefreshPanelAsync(panel.ID, force)
	}
	s.animeUI.RefreshAsync(false)
	s.bilibiliUI.RefreshAsync(false)
}
