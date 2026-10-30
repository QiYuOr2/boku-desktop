package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"boku/internal/domain"
	"boku/internal/translate"
)

func TestOwnServicesFollowStoredSettings(t *testing.T) {
	_, _, _, _, _, translation, own, _, _, recorder, _, err := services(t.TempDir(), "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(recorder.Stop)
	ctx := t.Context()
	if err := own.registerAll(); err != nil {
		t.Fatal(err)
	}
	if got := own.registry.Len(); got != 3 {
		t.Fatalf("empty custom settings changed the built-in registry: %d translators", got)
	}

	for _, id := range []string{"custom", "custom-2"} {
		got, err := translation.AddCustomTranslator(ctx, translate.CustomSettings{
			BaseURL: "http://127.0.0.1:11434/v1", DisplayName: id,
		})
		if err != nil || got != id {
			t.Fatalf("AddCustomTranslator = %q, %v; want %q", got, err, id)
		}
	}
	if err := own.registerAll(); err != nil {
		t.Fatal(err)
	}
	original, err := own.registry.Translator("custom")
	if err != nil {
		t.Fatal(err)
	}
	if err := own.registerAll(); err != nil {
		t.Fatal(err)
	}
	again, err := own.registry.Translator("custom")
	if err != nil || again != original || own.registry.Len() != 5 {
		t.Fatalf("repeated registration replaced a translator or changed the registry: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("request path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"你好"}}]}`))
	}))
	defer server.Close()
	if err := translation.SetCustomTranslator(ctx, "custom", translate.CustomSettings{
		BaseURL: server.URL + "/v1", DisplayName: "改过的服务", Model: "test-model",
	}); err != nil {
		t.Fatal(err)
	}
	// Updating settings must reach the already registered backend, without registering again.
	if got := original.Descriptor(); got.Endpoint != server.URL+"/v1/chat/completions" || got.DisplayName != "改过的服务" {
		t.Fatalf("descriptor after editing settings = %+v", got)
	}
	if err := translation.SetProvider(ctx, "custom"); err != nil {
		t.Fatal(err)
	}
	result, err := translation.Translate(ctx, translate.Request{Text: "hello"})
	if err != nil || result.TranslatedText != "你好" {
		t.Fatalf("translation through the saved address = %+v, %v", result, err)
	}

	if err := translation.RemoveCustomTranslator(ctx, "custom"); err != nil {
		t.Fatal(err)
	}
	if err := own.registerAll(); err != nil {
		t.Fatal(err)
	}
	if _, err := own.registry.Translator("custom"); !errors.Is(err, domain.ErrProviderNotFound) {
		t.Fatalf("removed translator lookup = %v", err)
	}
	for _, id := range []string{"custom-2", "free.microsoft", "free.bilibili", "deepseek"} {
		if !own.registry.Has(id) {
			t.Errorf("removing custom also removed %s", id)
		}
	}
}
