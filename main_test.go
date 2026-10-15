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
	"boku/internal/domain"
	"boku/internal/logging"
	panelcodex "boku/internal/panel/codex"
	paneldeepseek "boku/internal/panel/deepseek"
	"boku/internal/translate"
)

// The assembly is the one place a provider and its adapter, or a panel and
// its provider, can be paired wrongly. That is a startup error, and it can
// be checked without a window.
func TestServicesAssembleThePanels(t *testing.T) {
	panels, secrets, clip, translation, err := services(t.TempDir(), "0.1.0")
	if err != nil {
		t.Fatalf("services failed: %v", err)
	}
	if panels == nil || secrets == nil || clip == nil || translation == nil {
		t.Fatal("services returned no services")
	}
	// The clipboard starts off, and nothing here reads the system
	// clipboard: assembling a service is not using it.
	if settings := clip.Settings(); settings.ClipboardMonitoring || settings.ClipboardRegionVisible {
		t.Errorf("the clipboard starts enabled: %+v", settings)
	}

	listed := panels.List()
	if len(listed) != 2 {
		t.Fatalf("the application has %d panels, want the balance and the quota: %+v", len(listed), listed)
	}
	if listed[0].ProviderID != contractdeepseek.ProviderID {
		t.Errorf("the first panel's provider = %q, want %q", listed[0].ProviderID, contractdeepseek.ProviderID)
	}
	if listed[1].ProviderID != panelcodex.ProviderID {
		t.Errorf("the second panel's provider = %q, want %q", listed[1].ProviderID, panelcodex.ProviderID)
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
	_, _, _, translation, err := services(t.TempDir(), "0.1.0")
	if err != nil {
		t.Fatalf("services failed: %v", err)
	}
	views, err := translation.Providers(context.Background())
	if err != nil {
		t.Fatalf("Providers failed: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("the application has %d translators, want 2: %+v", len(views), views)
	}
	byID := map[string]bool{}
	for _, view := range views {
		byID[view.ID] = true
		if view.DisplayName == "" || view.DocsURL == "" {
			t.Errorf("translator %s is missing its name or its documentation link: %+v", view.ID, view)
		}
	}
	for _, want := range []string{"deepseek", "custom"} {
		if !byID[want] {
			t.Errorf("translator %s is not registered: %+v", want, views)
		}
	}
	// The tool starts with the built-in translator and no credential: it
	// says where to configure one rather than reaching for the network.
	if _, err := translation.Translate(context.Background(), translate.Request{Text: "hello"}); !errors.Is(err, domain.ErrCredentialMissing) {
		t.Errorf("Translate error = %v, want ErrCredentialMissing before anything is stored", err)
	}
}
