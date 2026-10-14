package main

import (
	"context"
	"errors"
	"testing"

	"boku/internal/domain"
	paneldeepseek "boku/internal/domain/deepseek"
	"boku/internal/usecase"
)

// The assembly is the one place a provider and its adapter, or a panel and
// its provider, can be paired wrongly. That is a startup error, and it can
// be checked without a window.
func TestServicesAssembleTheDeepSeekPanel(t *testing.T) {
	panels, secrets, clipboard, translate, err := services(t.TempDir(), "0.1.0")
	if err != nil {
		t.Fatalf("services failed: %v", err)
	}
	if panels == nil || secrets == nil || clipboard == nil || translate == nil {
		t.Fatal("services returned no services")
	}
	// The clipboard starts off, and nothing here reads the system
	// clipboard: assembling a service is not using it.
	if settings := clipboard.Settings(); settings.ClipboardMonitoring || settings.ClipboardRegionVisible {
		t.Errorf("the clipboard starts enabled: %+v", settings)
	}

	listed := panels.List()
	if len(listed) != 1 {
		t.Fatalf("the application has %d panels, want 1", len(listed))
	}
	if listed[0].ProviderID != paneldeepseek.ProviderID {
		t.Errorf("panel provider = %q, want %q", listed[0].ProviderID, paneldeepseek.ProviderID)
	}

	descriptor, err := panels.Descriptor(paneldeepseek.ProviderID)
	if err != nil {
		t.Fatalf("Descriptor failed: %v", err)
	}
	if descriptor.DisplayName == "" || descriptor.DocsURL == "" || descriptor.Endpoint == "" {
		t.Errorf("descriptor = %+v, want a display name, a docs link and an endpoint", descriptor)
	}
	if descriptor.CredentialKeys[0] != paneldeepseek.APIKeyCredential {
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

	// With no credential stored, the panel says so instead of asking the
	// network: nothing here reaches out.
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

// The assembly is also where the translators are registered, and where they
// are paired with the credentials and the settings their descriptors name:
// a translator without a settings store, or one whose settings it cannot
// read, is a startup mistake worth catching here.
func TestServicesAssembleTheTranslators(t *testing.T) {
	_, _, _, translate, err := services(t.TempDir(), "0.1.0")
	if err != nil {
		t.Fatalf("services failed: %v", err)
	}
	views, err := translate.Providers(context.Background())
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
	if _, err := translate.Translate(context.Background(), usecase.Request{Text: "hello"}); !errors.Is(err, domain.ErrCredentialMissing) {
		t.Errorf("Translate error = %v, want ErrCredentialMissing before anything is stored", err)
	}
}
