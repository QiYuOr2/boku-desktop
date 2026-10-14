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
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"boku/internal/app"
	adapterdeepseek "boku/internal/data/adapters/deepseek"
	clipboarddevice "boku/internal/data/clipboard"
	"boku/internal/data/httpx"
	"boku/internal/data/provider"
	providerdeepseek "boku/internal/data/providers/deepseek"
	translatedefault "boku/internal/data/providers/translate/custom"
	translatedeepseek "boku/internal/data/providers/translate/deepseek"
	"boku/internal/data/secret"
	"boku/internal/data/selection"
	"boku/internal/data/settings"
	"boku/internal/data/translator"
	"boku/internal/domain"
	paneldeepseek "boku/internal/domain/deepseek"
	"boku/internal/usecase"
)

// Name the application before Run: MyGo reads the name it registers with
// the system when the event loop starts, which is after init.
func init() {
	mygo.App.SetName("Boku")
}

func main() {
	userData, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Fatalf("boku: locate the user data directory: %v", err)
	}
	version := mygo.App.Version()

	panels, secrets, clipboard, translate, err := services(userData, version)
	if err != nil {
		log.Fatalf("boku: %v", err)
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
		Shortcuts: app.MyGoShortcuts(),
		Version:   version,
		Quit:      mygo.App.Quit,
	})
	if err != nil {
		log.Fatal(err)
	}

	// macOS: the tray icon is the entry point, so keep the app out of the
	// Dock. Windows and Linux ignore this.
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	// The quick window is the only window, and closing it must not quit the
	// application: MyGo quits on the last window closed unless something
	// listens.
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
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
		if err := state.Setup(); err != nil {
			log.Fatal(err)
		}
		// The shortcut needs the running application, and monitoring starts
		// only if the user turned it on.
		state.RegisterShortcuts()
		clipboard.Start(nil)
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
func services(userData, version string) (*usecase.Panels, *usecase.Secrets, *usecase.Clipboard, *usecase.Translate, error) {
	// Credentials: a file of their own, never the database. The store is
	// named here and nowhere else, so moving to the encrypted vault of
	// Task 008 is a change to this function.
	secretStore, err := secret.Open(filepath.Join(userData, "secrets.json"))
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("open the credential store: %w", err)
	}
	secrets, err := usecase.NewSecrets(secretStore)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("prepare the credential store: %w", err)
	}

	// The application's own settings and the clipboard history, next to
	// the credentials. See internal/data/settings for why the history is a
	// file rather than the table docs/database.md §3 describes.
	store, err := settings.New(
		filepath.Join(userData, "settings.json"),
		filepath.Join(userData, "clipboard_history.json"),
	)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("open the settings store: %w", err)
	}

	client := httpx.New(httpx.Options{UserAgent: "Boku/" + applicationVersion(version)})

	// Providers and their adapters are registered in pairs: a provider
	// without an adapter can only fail later, in front of the user.
	registry := provider.NewRegistry()
	if err := registry.Register(
		providerdeepseek.New(client),
		adapterdeepseek.NewAdapter(),
	); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("register the deepseek provider: %w", err)
	}
	if err := registry.Validate(); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("the provider registry is incomplete: %w", err)
	}

	// The panels the application shows. Adding one means adding a definition
	// here; the use case checks it against the provider's own contract.
	panels, err := usecase.NewPanels(registry, secretStore, []usecase.PanelDef{
		{
			Panel:   paneldeepseek.Panel(),
			Metrics: paneldeepseek.Metrics(),
		},
	})
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("prepare the panels: %w", err)
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
	clipboard, err := usecase.NewClipboard(usecase.ClipboardOptions{
		Device: device,
		Store:  store,
		Tools:  store,
		Clock:  usecase.SystemClock{},
	})
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("prepare the clipboard: %w", err)
	}

	// The translators the tool may ask. Like the panels, adding one means
	// adding a registration here; unlike a provider, a translator stands
	// alone: it answers in the domain's own words, so there is no adapter to
	// pair it with.
	translators := translator.NewRegistry()
	if err := translators.Register(translatedeepseek.New(client)); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("register the deepseek translator: %w", err)
	}
	if err := translators.Register(translatedefault.New(client, func() domain.CustomTranslatorSettings {
		// The address is read from the settings each time it is asked for:
		// the user may change it while the application runs, and the next
		// translation should use the new one.
		settings, err := store.Load(context.Background())
		if err != nil {
			return domain.CustomTranslatorSettings{}
		}
		return settings.Normalize().Translation.Custom
	})); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("register the custom translator: %w", err)
	}
	translate, err := usecase.NewTranslate(usecase.TranslateOptions{
		Translators: translators,
		Secrets:     secretStore,
		Tools:       store,
		// The selection is asked first and the clipboard second: what the
		// user has selected is what they mean, and what is on the clipboard
		// is what they copied last.
		Selection: selection.New(),
		Clipboard: device,
		Clock:     usecase.SystemClock{},
	})
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("prepare the translation tool: %w", err)
	}
	return panels, secrets, clipboard, translate, nil
}

// applicationVersion is the version to send as a user agent: a plain `go
// run` has none, and an empty "Boku/" would say nothing.
func applicationVersion(version string) string {
	if version == "" {
		return "dev"
	}
	return version
}
