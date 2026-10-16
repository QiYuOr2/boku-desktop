package app

import (
	"testing"

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
	state, _, _ := translateState(t)

	// The user typed into DeepSeek's card, then moved to another card: the
	// value is filed under DeepSeek and the field is bound to the other one.
	state.form.keys = map[string]string{"deepseek": "sk-typed-0001"}
	state.form.inputCard = freeMicrosoftID
	state.form.input = ""

	state.clearCardInput("deepseek")

	if _, still := state.form.keys["deepseek"]; still {
		t.Error("the saved value is still filed under its card")
	}
	// The field was bound to another card, so what is in it is untouched: the
	// user is typing there.
	if state.form.inputCard != freeMicrosoftID {
		t.Errorf("the field moved to %q", state.form.inputCard)
	}
}

// When the card being saved is the one the field is showing, the field is
// emptied as well: the key is stored, and the box it was typed into does not
// keep it.
func TestSavingTheDisplayedCardEmptiesTheField(t *testing.T) {
	state, _, _ := translateState(t)

	state.form.keys = map[string]string{"deepseek": "sk-typed-0001"}
	state.form.inputCard = "deepseek"
	state.form.input = "sk-typed-0001"

	state.clearCardInput("deepseek")

	if state.form.input != "" {
		t.Errorf("the field still holds %q", state.form.input)
	}
	if _, still := state.form.keys["deepseek"]; still {
		t.Error("the value is still filed under the card it was typed into")
	}
}

// A card that was never typed into is an empty answer rather than a panic: the
// actions read the field whether or not the user has been there.
func TestClearingACardWithNothingTypedIsHarmless(t *testing.T) {
	state, _, _ := translateState(t)
	state.form.keys = map[string]string{}
	state.form.inputCard = ""
	state.form.input = ""

	state.clearCardInput("deepseek")
	state.clearCardInput("")

	if len(state.form.keys) != 0 {
		t.Errorf("keys = %+v, want none", state.form.keys)
	}
}

// The whole path a user takes to add a service of their own: type a name and an
// address into the blank card, press 添加, and the field is clear afterwards
// while the settings hold what they typed.
//
// It is the sequence that crashed when the field cleared itself in a loop. It
// runs without a window — the write runs on a goroutine and hands its answer to
// Window.Update, and a state with no window never receives it — so what is
// checked here is the part that ran on the main thread before the goroutine,
// which is where the crash was.
func TestAddingAnOwnServiceDoesNotCrashTheField(t *testing.T) {
	state, _, _ := translateState(t)

	// What the user typed, in the two fields and the key box.
	state.form.newName = "测试"
	state.form.newURL = "127.0.0.1:11434/v1"
	state.form.keys = map[string]string{newCustomServiceID: ""}
	state.form.inputCard = newCustomServiceID
	state.form.input = ""

	// The guard clause the add runs before it writes anything: a service with
	// no address is not a service.
	address := translate.CustomSettings{BaseURL: state.form.newURL}.Normalize().BaseURL
	if address == "" {
		t.Fatal("the address the user typed was cleaned away")
	}
	if address != "http://127.0.0.1:11434/v1" {
		t.Errorf("address = %q, want the local service to be http", address)
	}
	// What the add does on the main thread once the write is done.
	state.clearCardInput(newCustomServiceID)
	if state.form.input != "" {
		t.Errorf("the field still holds %q", state.form.input)
	}
}

// The settings the card wrote are the ones the translator reads, so a saved
// address is the address asked.
func TestASavedAddressIsTheAddressAsked(t *testing.T) {
	_, fixture, _ := translateState(t)

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
