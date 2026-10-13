package main

import (
	"context"
	"errors"
	"testing"

	"boku/internal/domain"
	paneldeepseek "boku/internal/domain/deepseek"
)

// The assembly is the one place a provider and its adapter, or a panel and
// its provider, can be paired wrongly. That is a startup error, and it can
// be checked without a window.
func TestServicesAssembleTheDeepSeekPanel(t *testing.T) {
	panels, secrets, err := services(t.TempDir(), "0.1.0")
	if err != nil {
		t.Fatalf("services failed: %v", err)
	}
	if panels == nil || secrets == nil {
		t.Fatal("services returned no services")
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
