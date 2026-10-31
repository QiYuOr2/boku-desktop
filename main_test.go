package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	contractdeepseek "boku/internal/contract/deepseek"
	"boku/internal/credential"
	"boku/internal/domain"
	"boku/internal/logging"
	panelcodex "boku/internal/panel/codex"
	paneldeepseek "boku/internal/panel/deepseek"
	panelgrok "boku/internal/panel/grok"
	"boku/internal/preferences"
	"boku/internal/translate"
)

func TestServicesLoadAndPersistQuickWindowLayout(t *testing.T) {
	userData := t.TempDir()
	path := filepath.Join(userData, "settings.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"tools":{"quickWindowLayout":{"order":["anime","codex","deepseek","bilibili"],"hidden":["bilibili"]},"customKey":"keep"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	layout := preferences.NewQuickWindowLayout()
	_, _, _, _, _, _, _, appearance, prefs, recorder, _, err := services(userData, "test", layout)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(recorder.Stop)
	if layout.Order()[0] != "playtime" || layout.Order()[1] != "anime" || layout.Visible("bilibili") {
		t.Fatalf("loaded layout=%v", layout.Order())
	}
	layout.MoveToEnd("anime")
	layout.SetVisible("codex", false)
	if err := layout.Save(context.Background(), prefs); err != nil {
		t.Fatal(err)
	}
	if err := appearance.Save(context.Background(), prefs, preferences.AppearanceDark); err != nil {
		t.Fatal(err)
	}
	reloaded := preferences.NewQuickWindowLayout()
	_, _, _, _, _, _, _, _, _, again, _, err := services(userData, "test", reloaded)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(again.Stop)
	if reloaded.Order()[len(reloaded.Order())-1] != "anime" || reloaded.Visible("codex") || reloaded.Visible("bilibili") {
		t.Fatalf("saved layout=%v", reloaded.Order())
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), `"customKey": "keep"`) {
		t.Fatalf("unowned setting lost: %s", contents)
	}
}

// The assembly is the one place a provider and its adapter, or a panel and
// its provider, can be paired wrongly. That is a startup error, and it can
// be checked without a window.
func TestServicesAssembleThePanels(t *testing.T) {
	userData := t.TempDir()
	panels, anime, bilibili, secrets, clip, translation, _, appearance, prefs, recorder, _, err := services(userData, "0.1.0")
	if err != nil {
		t.Fatalf("services failed: %v", err)
	}
	t.Cleanup(recorder.Stop)
	if panels == nil || anime == nil || bilibili == nil || secrets == nil || clip == nil || translation == nil {
		t.Fatal("services returned no services")
	}
	// The appearance is a section of the same settings file, registered with
	// the store: a change to it has to be one the store accepts and writes.
	if appearance.Mode() != "system" {
		t.Errorf("the appearance starts at %q, want system", appearance.Mode())
	}
	if err := prefs.Update(context.Background(), "appearance", func() error {
		appearance.Set("dark")
		return nil
	}); err != nil {
		t.Fatalf("writing the appearance: %v", err)
	}
	if appearance.Mode() != "dark" {
		t.Errorf("the appearance after a save = %q, want dark", appearance.Mode())
	}
	// The clipboard starts off, and nothing here reads the system
	// clipboard: assembling a service is not using it.
	if settings := clip.Settings(); settings.Monitoring || settings.RegionVisible {
		t.Errorf("the clipboard starts enabled: %+v", settings)
	}
	if err := bilibili.Configure(context.Background(), true, []int64{672328094}, 5, 2, true, true); err != nil {
		t.Fatalf("saving Bilibili settings: %v", err)
	}
	if got := bilibili.Settings(); len(got.UIDs) != 1 || got.UIDs[0] != 672328094 || got.DynamicCount != 2 || !got.LiveNotifications || !got.DynamicNotifications {
		t.Errorf("Bilibili settings were not retained: %+v", got)
	}

	listed := panels.List()
	if len(listed) != 3 {
		t.Fatalf("the application has %d panels, want the balance, Codex and Grok: %+v", len(listed), listed)
	}
	if listed[0].ProviderID != contractdeepseek.ProviderID {
		t.Errorf("the first panel's provider = %q, want %q", listed[0].ProviderID, contractdeepseek.ProviderID)
	}
	if listed[1].ProviderID != panelcodex.ProviderID {
		t.Errorf("the second panel's provider = %q, want %q", listed[1].ProviderID, panelcodex.ProviderID)
	}
	if listed[2].ProviderID != panelgrok.ProviderID {
		t.Errorf("the third panel's provider = %q, want %q", listed[2].ProviderID, panelgrok.ProviderID)
	}

	descriptor, err := panels.Descriptor(contractdeepseek.ProviderID)
	if err != nil {
		t.Fatalf("Descriptor failed: %v", err)
	}
	if descriptor.DisplayName == "" || descriptor.DocsURL == "" || descriptor.Endpoint == "" {
		t.Errorf("descriptor = %+v, want a display name, a docs link and an endpoint", descriptor)
	}
	if descriptor.CredentialKeys[0] != contractdeepseek.APIKeyCredential {
		t.Errorf("credential keys = %v", descriptor.CredentialKeys)
	}
	if descriptor.UsageURL != paneldeepseek.UsageURL {
		t.Errorf("descriptor usage link = %q, want %q", descriptor.UsageURL, paneldeepseek.UsageURL)
	}
	// The page the panel's headline opens comes from the provider's own
	// descriptor, through the use case, to the view: the assembly is where
	// that link can go missing without anything failing to compile.
	if view := panels.View(listed[0].ID); view.UsageURL != paneldeepseek.UsageURL {
		t.Errorf("the panel opens %q, want the provider's page %q", view.UsageURL, paneldeepseek.UsageURL)
	}

	// The quota panel is assembled the same way, and its credential is the
	// one the Codex CLI keeps rather than a key of Boku's: what the
	// descriptor asks for is the name the source answers to.
	quota, err := panels.Descriptor(panelcodex.ProviderID)
	if err != nil {
		t.Fatalf("Descriptor failed for the quota: %v", err)
	}
	if quota.DisplayName == "" || quota.DocsURL == "" || quota.Endpoint == "" {
		t.Errorf("the quota descriptor = %+v, want a display name, a docs link and an endpoint", quota)
	}
	if len(quota.CredentialKeys) != 1 || quota.CredentialKeys[0] != panelcodex.AccessTokenCredential {
		t.Errorf("the quota asks for %v, want the CLI's token", quota.CredentialKeys)
	}
	if quota.UsageURL != panelcodex.UsageURL {
		t.Errorf("the quota's usage link = %q, want %q", quota.UsageURL, panelcodex.UsageURL)
	}
	if view := panels.View(listed[1].ID); len(view.Metrics) != len(panelcodex.Metrics()) {
		t.Errorf("the quota panel shows %d metrics, want %d", len(view.Metrics), len(panelcodex.Metrics()))
	}

	// With no credential stored, the balance panel says so instead of asking
	// the network: nothing here reaches out. (The quota panel's credential is
	// the Codex CLI's own login, which this machine may well have: reading it
	// is not this test's business.)
	view := panels.View(listed[0].ID)
	if view.HasSnapshot {
		t.Error("a panel with no credential claims a snapshot")
	}
	if _, err := panels.Refresh(context.Background(), listed[0].ID); !errors.Is(err, domain.ErrCredentialMissing) {
		t.Errorf("Refresh error = %v, want ErrCredentialMissing", err)
	}
	if !panels.View(listed[0].ID).NeedsCredentials() {
		t.Error("the view does not report the missing credential")
	}

	// The credential file lives under the user data directory the caller
	// named, and is not created until something is stored in it.
	if path := secrets.Path(); path == "" {
		t.Error("the credential store has no path")
	} else if filepath.Base(path) != credential.FileName {
		t.Errorf("the credential file is %q, want %q", filepath.Base(path), credential.FileName)
	}
}

// The logs live beside the rest of the application's files, in a directory of
// their own: one thing to look at, and one thing to delete.
func TestLogDirectoryIsUnderTheUserData(t *testing.T) {
	t.Setenv(logging.DirEnv, "")
	userData := t.TempDir()
	want := filepath.Join(userData, "logs")
	if got := logDirectory(userData); got != want {
		t.Errorf("logDirectory(%q) = %q, want %q", userData, got, want)
	}
}

// BOKU_LOG_DIR moves the logs, which is what a support run wants: the file is
// then somewhere that can be handed over whole.
func TestLogDirectoryFollowsTheEnvironment(t *testing.T) {
	elsewhere := filepath.Join(t.TempDir(), "somewhere else")
	t.Setenv(logging.DirEnv, "  "+elsewhere+"  ")
	if got := logDirectory(t.TempDir()); got != elsewhere {
		t.Errorf("logDirectory() = %q, want the environment's %q", got, elsewhere)
	}
}

// The first record of a run is where a failure that happens before any window
// exists is found afterwards, so the launch has to open the log, install it
// process-wide, and say what this run is — under the user data directory the
// caller named, and nowhere else.
func TestStartLoggingRecordsTheLaunchUnderTheUserData(t *testing.T) {
	keepStandardLoggers(t)
	t.Setenv(logging.DirEnv, "")
	userData := t.TempDir()

	logFile, err := startLogging(userData, "")
	if err != nil {
		t.Fatalf("startLogging: %v", err)
	}
	lives := filepath.Join(userData, "logs")
	if !strings.HasPrefix(logFile.File(), lives+string(filepath.Separator)) {
		t.Errorf("the log is at %q, want it under %q", logFile.File(), lives)
	}
	// Install is what puts the standard logger and slog's default logger
	// here; both are process-wide, so this is the only place to check it.
	slog.Info("through the default logger")
	if err := logFile.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(logFile.File())
	if err != nil {
		t.Fatalf("read the log: %v", err)
	}
	text := string(data)
	for _, want := range []string{`msg="boku started"`, "version=dev", "retentionDays=30", "through the default logger"} {
		if !strings.Contains(text, want) {
			t.Errorf("the log does not have %q in it:\n%s", want, text)
		}
	}
}

// keepStandardLoggers puts the process's loggers back the way they were: a
// test that installs a log owns the whole process while it runs.
func keepStandardLoggers(t *testing.T) {
	t.Helper()
	previousLogger := slog.Default()
	previousOut := log.Writer()
	previousFlags := log.Flags()
	previousPrefix := log.Prefix()
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
		log.SetOutput(previousOut)
		log.SetFlags(previousFlags)
		log.SetPrefix(previousPrefix)
	})
}

// A tray application has no console, so a panic that is only printed is a
// crash nobody can explain. It is written down — and then raised again, so
// that how the process ends is unchanged.
func TestRecordPanicsWritesItDownAndRaisesItAgain(t *testing.T) {
	keepStandardLoggers(t)
	dir := t.TempDir()
	logFile, err := logging.Open(logging.Options{Dir: dir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	slog.SetDefault(logFile.Logger())
	t.Cleanup(func() { _ = logFile.Close() })

	func() {
		defer func() {
			reason := recover()
			if reason == nil {
				t.Fatal("recordPanics swallowed the panic")
			}
			if reason != "the tray could not be created" {
				t.Errorf("the panic came back as %v, want it unchanged", reason)
			}
		}()
		func() {
			defer recordPanics()
			panic("the tray could not be created")
		}()
	}()

	_ = logFile.Close()
	data, err := os.ReadFile(logFile.File())
	if err != nil {
		t.Fatalf("read the log: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "level=ERROR") || !strings.Contains(text, "the tray could not be created") {
		t.Errorf("the panic is not in the log:\n%s", text)
	}
	if !strings.Contains(text, "goroutine") {
		t.Errorf("the log has no stack:\n%s", text)
	}
}

// The assembly is also where the translators are registered, and where they
// are paired with the credentials and the settings their descriptors name:
// a translator without a settings store, or one whose settings it cannot
// read, is a startup mistake worth catching here.
func TestServicesAssembleTheTranslators(t *testing.T) {
	_, _, _, _, _, translation, own, _, _, recorder, _, err := services(t.TempDir(), "0.1.0")
	if err != nil {
		t.Fatalf("services failed: %v", err)
	}
	t.Cleanup(recorder.Stop)
	views, err := translation.Providers(context.Background())
	if err != nil {
		t.Fatalf("Providers failed: %v", err)
	}
	// The three the application ships with. The user's own services are not
	// among them: there are none until the user adds one, and a translator for
	// a service nobody configured would be a row that cannot answer.
	if len(views) != 3 {
		t.Fatalf("the application has %d translators, want 3: %+v", len(views), views)
	}
	byID := map[string]bool{}
	for _, view := range views {
		byID[view.ID] = true
		if view.DisplayName == "" || view.DocsURL == "" {
			t.Errorf("translator %s is missing its name or its documentation link: %+v", view.ID, view)
		}
	}
	for _, want := range []string{"free.microsoft", "free.bilibili", "deepseek"} {
		if !byID[want] {
			t.Errorf("translator %s is not registered: %+v", want, views)
		}
	}
	// The free engines are the two choices a fresh installation has, and each
	// is usable with nothing configured at all: that is the whole point of
	// them.
	seen := 0
	for _, view := range views {
		if !strings.HasPrefix(view.ID, "free.") {
			continue
		}
		seen++
		if !view.Configured || view.NeedsCredential {
			t.Errorf("the free engine %s is not usable out of the box: %+v", view.ID, view)
		}
	}
	if seen != 2 {
		t.Errorf("the application has %d free engines, want two", seen)
	}
	if settings := translationSettingsOf(t, translation); settings.ProviderOr() != "free.microsoft" {
		t.Errorf("a fresh installation translates with %q, want the Microsoft engine", settings.ProviderOr())
	}

	// A service of the user's own is added the whole way: stored, registered,
	// and then a translator the tool can actually ask.
	id, err := translation.AddCustomTranslator(context.Background(), translate.CustomSettings{
		BaseURL:     "http://127.0.0.1:11434/v1",
		DisplayName: "本地测试服务",
	})
	if err != nil {
		t.Fatalf("AddCustomTranslator failed: %v", err)
	}
	if err := own.registerAll(); err != nil {
		t.Fatalf("registering the user's own services failed: %v", err)
	}
	views, err = translation.Providers(context.Background())
	if err != nil {
		t.Fatalf("Providers failed: %v", err)
	}
	found := false
	for _, view := range views {
		if view.ID != id {
			continue
		}
		found = true
		if view.DisplayName != "本地测试服务" {
			t.Errorf("the service is called %q, want the name the user gave it", view.DisplayName)
		}
		if view.Endpoint != "http://127.0.0.1:11434/v1/chat/completions" {
			t.Errorf("endpoint = %q", view.Endpoint)
		}
		if !view.Configured {
			t.Error("the service the user just added is not configured")
		}
	}
	if !found {
		t.Errorf("the added service has no translator: %+v", views)
	}
	// The first service takes the bare id, which is what a file written before
	// there could be several services holds.
	if id != "custom" {
		t.Errorf("the first service was stored as %q, want custom", id)
	}
}

// translationSettingsOf reads the tool's configuration, failing the test when
// it cannot be read: an assembly that cannot be read back is a mistake of its
// own.
func translationSettingsOf(t *testing.T, translation *translate.Translate) translate.Settings {
	t.Helper()
	settings, err := translation.Settings()
	if err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	return settings
}
