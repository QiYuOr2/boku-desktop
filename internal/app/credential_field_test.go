package app

import (
	credentialui "boku/internal/credential/ui"
	"testing"
	"time"

	"boku/internal/translate"
)

// The credential field: one card at a time, one string the framework can hold
// on to, and what a save does to both.
//
// It is tested here because the field is where a saved key would come back
// from: a value left filed under its card is put back on screen the next time
// the frame draws that card, and the user would be looking at the key they just
// saved.

// Saving a card forgets what was typed into it, whether or not that card is
// the one the field is currently bound to.
//
// This is the crash the field had: clearing the value called itself, and a save
// ran the stack out. It is also the behaviour the field needs, so it is checked
// rather than only not-crashing.
func TestSavingACardForgetsWhatWasTypedIntoIt(t *testing.T) {
	state, _ := translateState(t)

	// The user typed into DeepSeek's card, then moved to another card: the
	// value is filed under DeepSeek and the field is bound to the other one.
	state.credentialUI.Form.Keys = map[string]string{"deepseek": "sk-typed-0001"}
	state.credentialUI.Form.InputCard = freeMicrosoftID
	state.credentialUI.Form.Input = ""

	state.credentialUI.ClearCardInput("deepseek")

	if _, still := state.credentialUI.Form.Keys["deepseek"]; still {
		t.Error("the saved value is still filed under its card")
	}
	// The field was bound to another card, so what is in it is untouched: the
	// user is typing there.
	if state.credentialUI.Form.InputCard != freeMicrosoftID {
		t.Errorf("the field moved to %q", state.credentialUI.Form.InputCard)
	}
}

// When the card being saved is the one the field is showing, the field is
// emptied as well: the key is stored, and the box it was typed into does not
// keep it.
func TestSavingTheDisplayedCardEmptiesTheField(t *testing.T) {
	state, _ := translateState(t)

	state.credentialUI.Form.Keys = map[string]string{"deepseek": "sk-typed-0001"}
	state.credentialUI.Form.InputCard = "deepseek"
	state.credentialUI.Form.Input = "sk-typed-0001"

	state.credentialUI.ClearCardInput("deepseek")

	if state.credentialUI.Form.Input != "" {
		t.Errorf("the field still holds %q", state.credentialUI.Form.Input)
	}
	if _, still := state.credentialUI.Form.Keys["deepseek"]; still {
		t.Error("the value is still filed under the card it was typed into")
	}
}

// A card that was never typed into is an empty answer rather than a panic: the
// actions read the field whether or not the user has been there.
func TestClearingACardWithNothingTypedIsHarmless(t *testing.T) {
	state, _ := translateState(t)
	state.credentialUI.Form.Keys = map[string]string{}
	state.credentialUI.Form.InputCard = ""
	state.credentialUI.Form.Input = ""

	state.credentialUI.ClearCardInput("deepseek")
	state.credentialUI.ClearCardInput("")

	if len(state.credentialUI.Form.Keys) != 0 {
		t.Errorf("keys = %+v, want none", state.credentialUI.Form.Keys)
	}
}

func TestNewCustomServiceInputIsSeparateFromDeepSeekInput(t *testing.T) {
	state, _ := translateState(t)
	state.credentialUI.Form.InputCard = "deepseek"
	state.credentialUI.Form.Input = "sk-deepseek-only"

	state.credentialUI.ShowCardInput(credentialui.NewCustomServiceID)

	if got := state.credentialUI.Form.Input; got != "" {
		t.Errorf("custom API Key input = %q, want empty", got)
	}
	if got := state.credentialUI.Form.Keys["deepseek"]; got != "sk-deepseek-only" {
		t.Errorf("DeepSeek API Key input = %q, want its own value", got)
	}
}

func TestDeletingSelectedDeepSeekUsesTheFreeProvider(t *testing.T) {
	state, fixture := translateState(t)
	if err := fixture.service.SetProvider(t.Context(), "deepseek"); err != nil {
		t.Fatalf("SetProvider(deepseek) failed: %v", err)
	}
	state.credentialUI.DeleteCredential(cardOf(t, state, "deepseek"))

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		settings, err := fixture.service.Settings()
		if err != nil {
			t.Fatalf("Settings failed: %v", err)
		}
		if settings.ProviderOr() == freeMicrosoftID {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Error("deleting the selected DeepSeek key did not select the free provider")
}

func TestSavedCustomSettingsConfigureTheProvider(t *testing.T) {
	_, fixture := translateState(t)

	id := fixture.addService(translate.CustomSettings{
		BaseURL:     "http://127.0.0.1:11434/v1",
		DisplayName: "测试",
	})
	settings, err := fixture.service.Settings()
	if err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	service, ok := settings.CustomService(id)
	if !ok {
		t.Fatalf("the added service is not in the settings: %+v", settings.Custom)
	}
	if service.BaseURL != "http://127.0.0.1:11434/v1" {
		t.Errorf("base URL = %q", service.BaseURL)
	}
	if service.DisplayName != "测试" {
		t.Errorf("display name = %q", service.DisplayName)
	}
	// The translator that will be asked resolves to the saved address, and it
	// is called what the user called it.
	views, err := fixture.service.Providers(t.Context())
	if err != nil {
		t.Fatalf("Providers failed: %v", err)
	}
	found := false
	for _, view := range views {
		if view.ID != id {
			continue
		}
		found = true
		if view.Endpoint != "http://127.0.0.1:11434/v1/chat/completions" {
			t.Errorf("endpoint = %q, want the saved address", view.Endpoint)
		}
		if !view.Configured {
			t.Error("a service with an address is reported as unconfigured")
		}
	}
	if !found {
		t.Errorf("the translator for the added service is not among the views: %+v", views)
	}
}
