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
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/assets"
	"boku/internal/usecase"
)

const (
	appName    = "Boku"
	toolTip    = "Boku — 我的面板"
	panelWidth = 320
	// panelHeight leaves room for the panel, the two lines that close the
	// bottom, the clipboard line between them and the footer: the content
	// then fits without scrolling, and a panel with more in it scrolls
	// rather than pushing the lines off the window.
	panelHeight = 380
	// trayIconSize is the icon in device pixels: 16 points on a Retina
	// display, which MyGo scales to the screen's density on Windows and
	// Linux.
	trayIconSize = 32
)

// Deps is what the presentation layer is assembled from, by main.
type Deps struct {
	// Panels reads and refreshes the panels.
	Panels *usecase.Panels
	// Secrets stores the credentials those panels need.
	Secrets *usecase.Secrets
	// Clipboard reads and records the system clipboard. It is what the
	// quick window's clipboard line and the history window show; a State
	// without one draws neither.
	Clipboard *usecase.Clipboard
	// Version is the application version, shown in the footer.
	Version string
	// Quit is how the tray application leaves: it has no main window to
	// close.
	Quit func()
}

// State is the whole presentation state. It is read and written on the main
// thread only.
type State struct {
	app           *mygo.Application
	panelService  *usecase.Panels
	secretService *usecase.Secrets
	clipboard     *usecase.Clipboard
	version       string
	quit          func()

	trayIcon []byte
	// deepseekMark is the DeepSeek logo, parsed once at startup: a view is
	// built several times per frame, and parsing a document is not a view's
	// work.
	deepseekMark *ui.SVG
	// clipboardMark labels the quick window's clipboard line, parsed for the
	// same reason.
	clipboardMark *ui.SVG
	// refreshMark is the panel's refresh action. It is drawn instead of a
	// word, and turned while a fetch runs, so it is parsed once at startup
	// like the other marks.
	refreshMark *ui.SVG

	tray           *mygo.Tray
	panel          *mygo.Window // the open quick window, or nil when closed
	settingsWindow *mygo.Window // the open settings window, or nil
	historyWindow  *mygo.Window // the open clipboard history window, or nil

	// form is the settings window's state, on the main thread like the
	// rest.
	form connectForm
	// clipboardStatus is the last thing the clipboard switches said, shown
	// under them.
	clipboardStatus string
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
	trayIcon, err := assets.Icon(trayIconSize)
	if err != nil {
		return nil, err
	}
	// The providers' marks are parsed here for the same reason: the
	// settings window shows one beside the name of the provider it is
	// configuring.
	mark, err := ui.ParseSVG(assets.DeepSeek())
	if err != nil {
		return nil, fmt.Errorf("app: parse the DeepSeek mark: %w", err)
	}
	clipMark, err := ui.ParseSVG(assets.Clipboard())
	if err != nil {
		return nil, fmt.Errorf("app: parse the clipboard mark: %w", err)
	}
	refreshMark, err := ui.ParseSVG(assets.Refresh())
	if err != nil {
		return nil, fmt.Errorf("app: parse the refresh mark: %w", err)
	}
	return &State{
		app:           app,
		panelService:  deps.Panels,
		secretService: deps.Secrets,
		clipboard:     deps.Clipboard,
		version:       deps.Version,
		quit:          deps.Quit,
		trayIcon:      trayIcon,
		deepseekMark:  mark,
		clipboardMark: clipMark,
		refreshMark:   refreshMark,
	}, nil
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
	if s.panel != nil {
		s.panel.Update(func() {})
	}
	if s.settingsWindow != nil {
		s.settingsWindow.Update(func() {})
	}
	if s.historyWindow != nil {
		s.historyWindow.Update(func() {})
	}
}

// menu is the tray menu: the only way out of a tray application, which has
// no main window to close.
func (s *State) menu() *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: "打开 " + appName, Click: func(*mygo.MenuItem, *mygo.Window) { s.Toggle() }},
		{Label: "设置…", Click: func(*mygo.MenuItem, *mygo.Window) { s.openSettings() }},
		mygo.Separator(),
		{Label: "退出 " + appName, Click: func(*mygo.MenuItem, *mygo.Window) { s.quit() }},
	})
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

// Open shows a new quick window next to the tray icon.
//
// The window is created again on every open instead of being kept hidden:
// a flyout is placed against the tray when it is created, so a reused one
// would stay where it was first shown.
func (s *State) Open() {
	panel := mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:      s.tray,
		Placement: mygo.PlacementTopEnd,
		Gap:       4,
		Width:     panelWidth,
		Height:    panelHeight,
		// Focusable: a click outside closes it, as the system's menus do.
		Focusable: true,
		Shadow:    true,
		// Popover: on macOS it is an NSPopover, which draws the material,
		// the corners and the arrow pointing at the icon. Elsewhere the
		// view draws its own background.
		Popover: true,
		Content: ui.View(s.View),
	})
	s.panel = panel
	panel.OnClosed(func() {
		if s.panel == panel {
			s.panel = nil
		}
	})
	// Opening the window is the moment to refresh, and every tray click asks
	// again whatever the cache says: the numbers a click shows should be the
	// numbers now, not the ones the previous click left behind. The request
	// runs off the main thread; the view never starts work of its own,
	// because MyGo builds it several times per frame.
	s.refreshAsync(true)
}

// View builds one frame of the quick window: the panels, the divider and the
// footer. It has no header: what the window is about is said by the panel's
// own title, and a flyout is too small to spend its first line on repeating
// the application's name.
func (s *State) View(c *ui.Context) {
	t := c.Theme()
	c.Root().Background(ui.Transparent)
	box := ui.Column(c).Fill()
	switch {
	case c.Vibrancy(): // over the popover's own material on macOS
	case runtime.GOOS == "windows": // Windows 11 rounds and outlines the flyout
		box.Background(t.Surface)
	default:
		box.Background(t.Surface).Radius(10).Border(1, t.Border)
	}
	box.Children(func() {
		s.page(c, page{
			body: s.card,
			// The clipboard line closes the content: it is about the
			// desktop rather than about a panel, and it belongs at the far
			// end of the window from the numbers.
			bottom:  s.clipboardBottom,
			actions: s.flyoutActions,
		})
	})
}

// clipboardBottom draws the quick window's clipboard region, when the user
// asked for it. Without the switch the region is not there at all — no
// divider, no empty line — so a window that never wanted it is unchanged.
func (s *State) clipboardBottom(c *ui.Context) {
	if s.clipboard == nil {
		return
	}
	if !s.clipboard.Settings().ClipboardRegionVisible {
		return
	}
	s.clipboardRegion(c)
}

// flyoutActions is the quick window's footer: everything the user can always
// do from here.
func (s *State) flyoutActions(c *ui.Context) {
	if button := textButton(c, "设置"); button.Clicked() {
		s.openSettings()
	}
	if button := textButton(c, "退出"); button.Clicked() {
		s.quit()
	}
}

// versionLine is what the footer shows on the left.
func (s *State) versionLine() string {
	version := strings.TrimSpace(s.version)
	if version == "" {
		version = "dev"
	}
	return appName + " " + version
}

// refreshAsync fetches the first panel away from the main thread.
//
// force asks for a refresh whatever the cache says; otherwise the panel is
// only asked when its snapshot is missing, stale or from a failed attempt.
// The answer is not carried back here: the use case keeps it, and the
// repaint below makes the view read it. That is why a refresh that finishes
// while the window is closed loses nothing.
func (s *State) refreshAsync(force bool) {
	listed := s.panelService.List()
	if len(listed) == 0 {
		return
	}
	panelID := listed[0].ID
	panels := s.panelService
	window := s.panel
	if !force && !panels.NeedsRefresh(panelID) {
		return
	}
	go func() {
		ctx := context.Background()
		if force {
			_, _ = panels.Refresh(ctx, panelID)
		} else {
			_, _, _ = panels.RefreshIfNeeded(ctx, panelID)
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
