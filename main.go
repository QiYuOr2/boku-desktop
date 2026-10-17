// Command Boku is the desktop application: a tray icon whose left click
// opens the quick window next to it.
//
// This file is the only place that assembles the application: the HTTP
// client, the secret store, the provider registry, the use cases, the views
// and the two windows. Everything it wires up is a package of its own, and
// nothing below knows about anything above it.
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"boku/internal/app"
	"boku/internal/clipboard"
	"boku/internal/credential"
	"boku/internal/domain"
	"boku/internal/logging"
	"boku/internal/panel"
	codexpanel "boku/internal/panel/codex"
	deepseekpanel "boku/internal/panel/deepseek"
	"boku/internal/platform/httpc"
	"boku/internal/platform/popoverappearance"
	"boku/internal/platform/selection"
	clipboarddevice "boku/internal/platform/sysclip"
	"boku/internal/settings"
	"boku/internal/translate"
	deepseektranslator "boku/internal/translate/deepseek"
	freetranslator "boku/internal/translate/free"
)

// Name the application before Run: MyGo reads the name it registers with
// the system when the event loop starts, which is after init.
func init() {
	mygo.App.SetName("Boku")
}

func main() {
	// One process, one tray icon. A second launch hands its command line to
	// the instance that is already running and leaves; that instance brings
	// its quick window forward instead (§2 of the desktop design). The lock is
	// taken before anything is opened, so a second launch costs no more than
	// the hand-over itself.
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}

	userData, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Fatalf("boku: locate the user data directory: %v", err)
	}
	version := mygo.App.Version()

	// The log comes first, before anything that can fail: it is where a
	// failure that happens before a window exists is found afterwards.
	// the known-issues notes ask for facts before a guess, and this is the only
	// place they are written down.
	logFile, err := startLogging(userData, version)
	if err != nil {
		// A log that cannot be written is one way fewer to diagnose this
		// application, not a reason for it not to start: the records go to
		// stderr, which is where they went before there was a file.
		log.Printf("boku: %v; this run is not being recorded", err)
	} else {
		defer func() { _ = logFile.Close() }()
		// A tray application has no console: a panic that is only printed
		// is a crash nobody can explain. It is recorded, and then raised
		// again unchanged — how the process ends is not this defer's
		// business.
		defer recordPanics()
	}

	panels, secrets, clipboard, translate, own, appearance, prefs, err := services(userData, version)
	if err != nil {
		log.Fatalf("boku: %v", err)
	}

	// The native popover appearance is a temporary macOS bridge for forced
	// dark mode. It is injected as a small interface so that replacing it
	// with a MyGo API later touches this assembly and nothing in the views.
	var popoverAppearance app.PopoverAppearance
	if popoverappearance.Available() {
		popoverAppearance = popoverappearance.New()
	}

	// A clipboard reading happening on the poll's goroutine comes back
	// here, on the main thread. The state does not exist yet when the
	// service is built, so the callback reads it through a variable that
	// is set a few lines below: a repaint before then is one there is
	// nothing to repaint.
	var state *app.State
	clipboard.SetEmit(func() {
		if state != nil {
			state.Refresh()
		}
	})

	state, err = app.New(mygo.App, app.Deps{
		Panels:    panels,
		Secrets:   secrets,
		Clipboard: clipboard,
		Translate: translate,
		// What the settings window calls after the user adds a service of
		// their own: the translator for it has to exist before the next
		// translation asks one.
		RegisterOwnServices: own.registerAll,
		Shortcuts:           app.MyGoShortcuts(),
		Version:             version,
		Quit:                mygo.App.Quit,
		// The theme is the presentation's own setting: it is stored with the
		// features' keys, but only this window knows what the modes mean.
		Appearance:      appearance,
		AppearanceStore: prefs,
		ApplyAppearance: func(mode app.AppearanceMode) {
			mygo.Theme.SetSource(app.ThemeSource(mode))
			// Forcing an appearance hides the desktop's own from Theme.IsDark,
			// and lifting the force (自动) uncovers it again. Re-read it now,
			// while the new source is in force, so that choosing 自动 shows
			// the desktop's appearance rather than the one it had last been
			// told about.
			state.SyncAppearance(mygo.Theme.IsDark())
		},
		PopoverAppearance: popoverAppearance,
	})
	if err != nil {
		log.Fatal(err)
	}

	// A launch that reached the running instance is the user asking for the
	// window, not for a second application: the tray is already there, so
	// the handler only brings the quick window forward.
	mygo.App.OnSecondInstance(func([]string, string) {
		state.Activate()
	})

	// macOS: reopening the app (Finder, `open -a Boku`) is an activation
	// event, not a second process: LaunchServices gives the running instance
	// applicationShouldHandleReopen, which MyGo reports as OnActivate. The
	// second-instance path above is the Windows and Linux one.
	if runtime.GOOS == "darwin" {
		mygo.App.OnActivate(func(hasVisibleWindows bool) {
			if !hasVisibleWindows {
				state.Activate()
			}
		})
		// The user may have just flipped the Accessibility switch in System
		// Settings; coming back is the moment to stop showing the request.
		mygo.App.OnDidBecomeActive(func() {
			state.RefreshSelectionAccess()
		})
	}

	// macOS: the tray icon is the entry point, so keep the app out of the
	// Dock. Windows and Linux ignore this.
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	// The saved appearance goes to the desktop before Run, as MyGo asks: the
	// title bars, menus and the macOS popover's material are drawn before the
	// first frame, and a Boku that starts light and switches a moment later
	// is a window that flashes. In the automatic mode this is the system
	// source; follows it while the application runs, too.
	mygo.Theme.SetSource(app.ThemeSource(appearance.Mode()))
	mygo.Theme.OnUpdated(func() {
		state.SyncAppearance(mygo.Theme.IsDark())
	})
	// The quick window is the only window, and closing it must not quit the
	// application: MyGo quits on the last window closed unless something
	// listens.
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		slog.Info("boku is quitting")
		// The clipboard poll is a goroutine, and it ends before the
		// process does: a read that arrives while the backend is going
		// away is a read nobody asked for.
		clipboard.Stop()
		// A shortcut left bound belongs to a process that no longer exists:
		// the next launch would find it taken by itself.
		state.UnregisterShortcuts()
		state.Destroy()
	})
	mygo.App.WhenReady(func() {
		// The system's answer is only available once the application is
		// running; in the automatic mode this is what the first frame uses.
		state.SyncAppearance(mygo.Theme.IsDark())
		if err := state.Setup(); err != nil {
			log.Fatal(err)
		}
		// The shortcut needs the running application, and monitoring starts
		// only if the user turned it on.
		state.RegisterShortcuts()
		clipboard.Start(nil)
		slog.Info("boku is ready")
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// services assembles everything that exists before a window does: the
// credential store, the settings, the system clipboard, the HTTP client, the
// provider registry, the panels and the translation tool.
//
// It is a function of its own so that a test can check the wiring — a
// mismatched provider and adapter, or a panel that breaks its provider's
// contract, is a startup error and worth catching without a graphical
// session.
func services(userData, version string) (*panel.Panels, *credential.Secrets, *clipboard.Clipboard, *translate.Translate, *ownServices, *app.Appearance, *settings.Store, error) {
	// Credentials: a file of their own, never the database. The store is
	// named here and nowhere else.
	//
	// The values are plain text. That is a deliberate choice rather than an
	// unfinished one: with no passphrase for the user to type there is
	// nowhere to keep a key that somebody reading the directory cannot also
	// read, and a file encrypted with a key stored beside it would look like
	// protection without being any. §5 of the security design says what
	// protects them instead — a 0600 file, an ACL on Windows, never a value
	// in a log or an error, and a window that says plainly what the file is.
	secretStore, err := credential.Open(filepath.Join(userData, credential.FileName))
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("open the credential store: %w", err)
	}
	secrets, err := credential.NewSecrets(secretStore)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("prepare the credential store: %w", err)
	}

	// The application's own settings and the clipboard history, next to the
	// credentials: two files, each owned by the feature that reads it. See
	// internal/clipboard/store.go for why the history is a file rather than
	// the table §3 of the storage design describes.
	//
	// The settings file is shared, so each feature that has settings is
	// registered with the store: the store itself knows nothing about what is
	// in it, and a feature is handed only the keys it owns.
	clipboardSettings := clipboard.NewSection()
	translateSettings := translate.NewSection()
	panelSettings := panel.NewSection()
	// The appearance is the presentation's choice rather than a feature's,
	// but it is stored the same way: a section of its own in the same file,
	// written through the same store.
	appearance := app.NewAppearanceSection()
	prefs, err := settings.New(
		filepath.Join(userData, "settings.json"),
		clipboardSettings,
		translateSettings,
		panelSettings,
		appearance,
	)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("open the settings store: %w", err)
	}
	// The file is read once, here: a frame must not read it, and neither
	// must a service that already has its settings in memory.
	if err := prefs.Load(context.Background()); err != nil {
		return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("read the settings: %w", err)
	}
	history, err := clipboard.NewHistory(filepath.Join(userData, "clipboard_history.json"))
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("open the clipboard history: %w", err)
	}

	client := httpc.New(httpc.Options{UserAgent: "Boku/" + applicationVersion(version)})

	// Providers and their adapters are registered in pairs: a provider
	// without an adapter can only fail later, in front of the user.
	registry := panel.NewRegistry()
	if err := registry.Register(
		deepseekpanel.New(client),
		deepseekpanel.NewAdapter(),
	); err != nil {
		return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("register the deepseek provider: %w", err)
	}
	if err := registry.Register(
		codexpanel.New(client),
		codexpanel.NewAdapter(),
	); err != nil {
		return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("register the codex provider: %w", err)
	}
	if err := registry.Validate(); err != nil {
		return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("the provider registry is incomplete: %w", err)
	}

	// The Codex quota's credential is not one of Boku's: it is the ChatGPT
	// login the Codex CLI already made, read where it lies and refreshed in
	// memory (§9 of the security design).
	codexLogin := codexpanel.NewLogin(codexpanel.LoginOptions{Client: client})

	// The panels the application shows. Adding one means adding a definition
	// here; the panel service checks it against the provider's own contract. A
	// panel marked optional is one the user can turn off in the settings
	// window, and its switch is stored in the same settings file as the rest
	// of the application's choices.
	panels, err := panel.NewPanels(registry, secretStore, panelSettings, prefs, []panel.PanelDef{
		{
			Panel:   deepseekpanel.Panel(),
			Metrics: deepseekpanel.Metrics(),
		},
		{
			Panel:   codexpanel.Panel(),
			Metrics: codexpanel.Metrics(),
			// The quota comes from the Codex CLI's own login on this
			// machine, so reading it is the user's to allow.
			Optional: true,
		},
	}, codexLogin)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("prepare the panels: %w", err)
	}
	// The switches are read once, here: a frame must not read a file.
	if err := panels.LoadChoices(context.Background()); err != nil {
		return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("read the panel switches: %w", err)
	}

	// The clipboard reads and writes on the thread the platform owns, so
	// every call goes through MyGo's dispatcher.
	device := clipboarddevice.New(func(fn func()) bool {
		done := make(chan struct{})
		go func() {
			defer close(done)
			mygo.RunOnMain(fn)
		}()
		select {
		case <-done:
			return true
		case <-time.After(clipboarddevice.DefaultTimeout):
			return false
		}
	})
	clip, err := clipboard.NewClipboard(clipboard.ClipboardOptions{
		Device:  device,
		Store:   history,
		Section: clipboardSettings,
		// The store the switches are written through. The file was read
		// once above, so this only writes.
		Settings: prefs,
		Clock:    domain.SystemClock{},
	})
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("prepare the clipboard: %w", err)
	}

	// The translators the tool may ask. Like the panels, adding one means
	// adding a registration here; unlike a provider, a translator stands
	// alone: it answers in the domain's own words, so there is no adapter to
	// pair it with.
	translators := translate.NewRegistry()
	if err := translators.Register(deepseektranslator.New(client)); err != nil {
		return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("register the deepseek translator: %w", err)
	}
	// The free engines: no key, no account, and the reason a fresh
	// installation can press the shortcut and get an answer instead of a page
	// explaining where to buy one. They are two translators rather than one,
	// because the settings window offers them as two choices — the user picks
	// the engine, and that engine is the one asked.
	if err := translators.Register(freetranslator.NewMicrosoft(client)); err != nil {
		return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("register the free microsoft translator: %w", err)
	}
	if err := translators.Register(freetranslator.NewBilibili(client)); err != nil {
		return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("register the free bilibili translator: %w", err)
	}
	// The services the user runs themselves: one translator per stored service,
	// because each has its own id, its own address and its own name. Which ones
	// exist is the settings file's answer, and the user adds one from the
	// settings window — so the registry is given a way to catch up with what
	// they added, rather than a set written down here.
	own := newOwnServices(client, translators, translateSettings)
	if err := own.registerAll(); err != nil {
		return nil, nil, nil, nil, nil, nil, nil, err
	}
	translation, err := translate.NewTranslate(translate.TranslateOptions{
		Translators: translators,
		Secrets:     secretStore,
		Section:     translateSettings,
		Settings:    prefs,
		// The selection is asked first and the clipboard second: what the
		// user has selected is what they mean, and what is on the clipboard
		// is what they copied last. The clipboard is also what the selection
		// device borrows when an application publishes nothing — it asks the
		// application to copy, then puts the clipboard back — so the tool is
		// told to keep that borrowed value out of the history.
		Selection: selection.New(selection.Options{
			Clipboard:  device,
			BeforeCopy: clip.IgnoreTheNextCopy,
			// The clipboard's write counter is the platform's own, and the
			// selection tool uses the same one Windows uses. On macOS the
			// copy fallback cannot tell "nothing selected" from "copied the
			// same text again" without it.
			Sequence: device.ChangeCount,
		}),
		Clipboard: device,
		Clock:     domain.SystemClock{},
	})
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("prepare the translation tool: %w", err)
	}
	return panels, secrets, clip, translation, own, appearance, prefs, nil
}

// applicationVersion is the version to send as a user agent: a plain `go
// run` has none, and an empty "Boku/" would say nothing.
func applicationVersion(version string) string {
	if version == "" {
		return "dev"
	}
	return version
}

// logDirectory is where the daily logs live: a directory of their own under
// the user data directory, so that the logs are one thing to look at and one
// thing to delete. BOKU_LOG_DIR moves them, which is what a support run
// wants — the file is then somewhere that can be handed over whole.
func logDirectory(userData string) string {
	if dir := strings.TrimSpace(os.Getenv(logging.DirEnv)); dir != "" {
		return dir
	}
	return filepath.Join(userData, "logs")
}

// startLogging opens the day's log, makes it the process's own, and records
// the launch.
//
// It is a function of its own, like services, so that a test can check it
// without a window: the first record of a run is where a failure that happens
// before anything else exists is found afterwards.
func startLogging(userData, version string) (*logging.Log, error) {
	logFile, err := logging.Open(logging.Options{
		Dir:   logDirectory(userData),
		Level: logging.LevelFromEnv(),
	})
	if err != nil {
		return nil, err
	}
	logFile.Install()
	slog.Info("boku started",
		"version", applicationVersion(version),
		"os", runtime.GOOS,
		"arch", runtime.GOARCH,
		"data", userData,
		"log", logFile.File(),
		"retentionDays", logging.RetentionDays,
	)
	return logFile, nil
}

// recordPanics writes down why the process is dying, then lets it die the way
// it was going to: the reason is in the file for the next launch to find, and
// nothing about how the process ends changes.
func recordPanics() {
	reason := recover()
	if reason == nil {
		return
	}
	slog.Error("boku panicked", "reason", reason, "stack", string(debug.Stack()))
	panic(reason)
}
