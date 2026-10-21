package app

import (
	credentialui "boku/internal/credential/ui"
	"fmt"
	"strconv"
	"testing"

	contractfree "boku/internal/contract/freetranslate"
	"boku/internal/domain"
	"boku/internal/translate"
)

// okTranslation is an answer a free engine gives when it works.
func okTranslation() domain.TranslationResult {
	return domain.TranslationResult{
		TranslatedText: "你好",
		SourceLang:     domain.LangEnglish,
		TargetLang:     domain.LangChinese,
	}
}

// errTranslationUnavailable is a free engine that could not answer: the
// endpoint refused, which is the failure the user reads.
func errTranslationUnavailable() error {
	return fmt.Errorf("%w: the endpoint answered 429", domain.ErrProviderRateLimited)
}

// aTranslationSettings reads the settings a state is running on.
func aTranslationSettings(t *testing.T, state *State) translate.Settings {
	t.Helper()
	settings, err := state.translate.Settings()
	if err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	return settings
}

// cardOf is one card of the window, by id.
func cardOf(t *testing.T, state *State, id string) credentialui.TranslateCard {
	t.Helper()
	for _, card := range state.credentialUI.TranslateCards() {
		if card.ID == id {
			return card
		}
	}
	t.Fatalf("the window has no card for %s", id)
	return credentialui.TranslateCard{}
}

// The free engines as the settings window drives them: the two options the
// translation section draws, the group they are drawn in, and the fact that
// the API Key section leaves them out.
//
// These are behavior tests, not view tests: what the window decides — which
// cards it draws where, which group a card lands in — is checked, and what it
// looks like is not. The view itself is accepted at runtime, as the testing
// rules ask.

// The free engines are two independent options, and they are what a fresh
// installation starts on.
func TestTheFreeEnginesAreTwoSeparateChoices(t *testing.T) {
	state, _, _ := translateState(t)
	cards := state.credentialUI.TranslateCards()

	byID := map[string]credentialui.TranslateCard{}
	for _, card := range cards {
		byID[card.ID] = card
	}
	for _, id := range []string{freeMicrosoftID, freeBilibiliID} {
		card, ok := byID[id]
		if !ok {
			t.Fatalf("the picker has no row for %s: %+v", id, cards)
		}
		if card.NeedsCredential {
			t.Errorf("%s is drawn as needing a key", id)
		}
		if !card.Configured {
			t.Errorf("%s is not usable out of the box: %+v", id, card)
		}
	}
	// The names are the design's own: the two free options the mock draws.
	if got := byID[freeMicrosoftID].DisplayName; got != "微软 Edge 翻译" {
		t.Errorf("the first free option is called %q", got)
	}
	if got := byID[freeBilibiliID].DisplayName; got != "B站 Index-Translate" {
		t.Errorf("the second free option is called %q", got)
	}
	// A fresh installation translates with the Microsoft engine, which is one
	// of the two free choices rather than a tier above them.
	if got := aTranslationSettings(t, state).ProviderOr(); got != freeMicrosoftID {
		t.Errorf("a fresh installation uses %q, want the Microsoft engine", got)
	}
}

// The picker's two groups: the free engines above, the services that have
// something to fill in below.
func TestThePickerGroupsFreeEnginesSeparately(t *testing.T) {
	state, fixture, _ := translateState(t)
	// A service of the user's own is added, because that is the only way one
	// exists: there is no card for a service nobody configured.
	own := fixture.addService(translate.CustomSettings{
		BaseURL:     "https://api.example.com/v1",
		DisplayName: "本地 Ollama",
	})

	free, keyed := splitTranslateCards(state.credentialUI.TranslateCards())
	if len(free) != 2 {
		t.Fatalf("free = %+v, want the two free engines", free)
	}
	for _, card := range free {
		if !isFreeTranslator(card.ID) {
			t.Errorf("%s is in the free group and is not a free engine", card.ID)
		}
	}
	ids := map[string]bool{}
	for _, card := range keyed {
		ids[card.ID] = true
	}
	// DeepSeek needs a key, and the user's own service has an address it is
	// asked at: both belong under the heading that says something has to be
	// configured, even though the second one's key is optional.
	for _, want := range []string{"deepseek", own} {
		if !ids[want] {
			t.Errorf("%s is not in the group that needs configuring: %+v", want, ids)
		}
	}
	for id := range ids {
		if isFreeTranslator(id) {
			t.Errorf("%s is a free engine and is in the group that needs a key", id)
		}
	}
}

// The API Key section draws only the services that have a key: the free
// engines are chosen in the translation section instead, where the user is
// deciding what to translate with rather than what to type.
func TestTheAPIKeySectionLeavesTheFreeEnginesOut(t *testing.T) {
	state, fixture, _ := translateState(t)
	own := fixture.addService(translate.CustomSettings{
		BaseURL:     "https://api.example.com/v1",
		DisplayName: "本地 Ollama",
	})

	keyed := credentialui.KeyedTranslateCards(state.credentialUI.TranslateCards())
	for _, card := range keyed {
		if isFreeTranslator(card.ID) {
			t.Errorf("the API Key section draws %s, a free engine", card.ID)
		}
	}
	ids := map[string]bool{}
	for _, card := range keyed {
		ids[card.ID] = true
	}
	// The cards the design draws there: the services with a key, and the ones
	// the user runs themselves.
	for _, want := range []string{"deepseek", own} {
		if !ids[want] {
			t.Errorf("the API Key section lost %s: %+v", want, ids)
		}
	}
	if len(keyed) != 2 {
		t.Errorf("the API Key section draws %d cards, want the keyed ones: %+v", len(keyed), keyed)
	}
}

// The user's own services are the last cards in the API Key section, whichever
// order the registry hands the translators over in, and the blank card that
// adds another is drawn after them.
//
// The named services are a fixed catalogue the application knows; these are the
// user's own entries. A list reads better when what is fixed comes first and
// what was added comes after it.
func TestTheOwnServiceCardsComeLast(t *testing.T) {
	state, fixture, _ := translateState(t)

	// An unconfigured service is not a card: it has nothing to show and
	// nothing to ask.
	keyed := credentialui.KeyedTranslateCards(state.credentialUI.TranslateCards())
	if len(keyed) == 0 {
		t.Fatal("the API Key section has no cards")
	}
	for _, card := range keyed {
		if translate.IsCustomService(card.ID) {
			t.Fatalf("an unconfigured own service is a card: %+v", keyed)
		}
	}

	// Once added, every one of them is after every named service.
	ids := []string{}
	for i := 0; i < 2; i++ {
		id := fixture.addService(translate.CustomSettings{
			BaseURL:     "https://api" + strconv.Itoa(i) + ".example.com/v1",
			DisplayName: "服务" + strconv.Itoa(i),
		})
		ids = append(ids, id)
	}
	keyed = credentialui.KeyedTranslateCards(state.credentialUI.TranslateCards())
	ownFrom := -1
	for i, card := range keyed {
		if translate.IsCustomService(card.ID) {
			if ownFrom < 0 {
				ownFrom = i
			}
			continue
		}
		if ownFrom >= 0 {
			t.Fatalf("a named service comes after the user's own: %+v", keyed)
		}
	}
	if ownFrom < 0 {
		t.Fatalf("the added services are not cards: %+v", keyed)
	}
	if len(keyed)-ownFrom != len(ids) {
		t.Errorf("the section draws %d services of the user's own, want %d: %+v",
			len(keyed)-ownFrom, len(ids), keyed)
	}
}

// A service of the user's own is configured when it has an address, which is
// the same fact the badge states for every other card: it is added configured,
// so the badge never reads 未配置 on one.
func TestAnAddedOwnServiceIsConfigured(t *testing.T) {
	state, fixture, _ := translateState(t)

	id := fixture.addService(translate.CustomSettings{
		BaseURL:     "https://api.example.com/v1",
		DisplayName: "本地 Ollama",
	})
	card := cardOf(t, state, id)
	if !card.Configured {
		t.Errorf("a service the user just added is not configured: %+v", card)
	}
	// Its name is the user's own, not this feature's word for a service.
	if card.DisplayName != "本地 Ollama" {
		t.Errorf("the card is called %q, want the name the user gave it", card.DisplayName)
	}
}

// Every card the user can take something away from offers 删除, which is two
// kinds of card and not one.
//
// The bug this pins: the word was drawn only once a credential was stored, so a
// service of the user's own with no key — a model on their own machine needs
// none — was a card they had added and could not get rid of.
func TestDeleteIsOnEveryCardThatHasSomethingToRemove(t *testing.T) {
	state, fixture, _ := translateState(t)

	// The user's own service, added with no key at all: nothing is stored for
	// it, and it is still removable.
	own := fixture.addService(translate.CustomSettings{
		BaseURL:     "http://127.0.0.1:11434/v1",
		DisplayName: "本地 Ollama",
	})
	if state.credentialUI.Form.HasCredential[own] {
		t.Fatal("the fixture stored a key this test is about not having")
	}
	if !credentialui.CardCanBeRemoved(cardOf(t, state, own), state.credentialUI.Form.HasCredential[own]) {
		t.Error("a keyless service of the user's own cannot be removed")
	}

	// A built-in service with no key stored has nothing for the word to do.
	if credentialui.CardCanBeRemoved(cardOf(t, state, "deepseek"), false) {
		t.Error("a card with nothing stored offers to remove something")
	}
	// Once it holds a key, it does.
	if !credentialui.CardCanBeRemoved(cardOf(t, state, "deepseek"), true) {
		t.Error("a card holding a key cannot give it up")
	}
}

// Removing a keyless service of the user's own works: the service goes, and
// the missing key under it is not reported as a failure.
//
// It is what a local model goes through — added with no key, then removed —
// and the credential store answers ErrSecretNotFound for a name it never had.
func TestRemovingAKeylessOwnServiceWorks(t *testing.T) {
	state, fixture, _ := translateState(t)
	id := fixture.addService(translate.CustomSettings{
		BaseURL:     "http://127.0.0.1:11434/v1",
		DisplayName: "本地 Ollama",
	})

	// What the removal does on the goroutine, without the window handoff: the
	// settings lose the service, and the key it never had is not an error.
	if err := fixture.service.RemoveCustomTranslator(t.Context(), id); err != nil {
		t.Fatalf("RemoveCustomTranslator failed: %v", err)
	}
	if err := credentialui.ForgetCredential(t.Context(), state.secretService, id, "apiKey", false); err != nil {
		t.Fatalf("dropping a key that was never stored reported %v", err)
	}
	settings, err := fixture.service.Settings()
	if err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	if _, still := settings.CustomService(id); still {
		t.Errorf("the service is still there: %+v", settings.Custom)
	}
}

// A removed service leaves the page completely: its card is gone, and so is
// its row in the picker.
//
// The bug this pins: the cards are drawn from the registry, and removing a
// service only took it out of the settings — so the card stayed, with no
// address and nothing to ask. Both halves have to go.
func TestARemovedServiceLeavesThePage(t *testing.T) {
	state, fixture, _ := translateState(t)
	id := fixture.addService(translate.CustomSettings{
		BaseURL:     "http://127.0.0.1:11434/v1",
		DisplayName: "本地 Ollama",
	})

	// It is on the page in both places to begin with.
	if !hasCard(state, id) {
		t.Fatalf("the service that was just added is not a card: %+v", state.credentialUI.TranslateCards())
	}
	if !hasPickerRow(state, id) {
		t.Fatalf("the service that was just added is not a row: %+v", pickerTranslateCards(state.credentialUI.TranslateCards()))
	}

	// What the removal does on the goroutine, without the window handoff.
	if err := fixture.service.RemoveCustomTranslator(t.Context(), id); err != nil {
		t.Fatalf("RemoveCustomTranslator failed: %v", err)
	}
	if err := fixture.registerOwn(); err != nil {
		t.Fatalf("the registry could not be brought in step: %v", err)
	}

	if hasCard(state, id) {
		t.Errorf("the removed service is still a card: %+v", state.credentialUI.TranslateCards())
	}
	if hasPickerRow(state, id) {
		t.Errorf("the removed service is still a row: %+v", pickerTranslateCards(state.credentialUI.TranslateCards()))
	}
	// The other cards are untouched: removing one service does not empty the
	// section.
	if !hasCard(state, freeMicrosoftID) && !hasCard(state, "deepseek") {
		t.Error("the removal took the named services with it")
	}
}

// hasCard reports whether the API Key section draws a card for an id.
func hasCard(state *State, id string) bool {
	for _, card := range credentialui.KeyedTranslateCards(state.credentialUI.TranslateCards()) {
		if card.ID == id {
			return true
		}
	}
	return false
}

// hasPickerRow reports whether the translation section offers an id.
func hasPickerRow(state *State, id string) bool {
	for _, card := range pickerTranslateCards(state.credentialUI.TranslateCards()) {
		if card.ID == id {
			return true
		}
	}
	return false
}

// A service of the user's own is a row only once it exists: it is created from
// the API Key section, and there is nothing to choose until then.
func TestAnUnaddedOwnServiceIsNotARow(t *testing.T) {
	state, fixture, _ := translateState(t)

	// Nothing added yet: no row of the user's own.
	cards := pickerTranslateCards(state.credentialUI.TranslateCards())
	for _, card := range cards {
		if translate.IsCustomService(card.ID) {
			t.Fatalf("a service that does not exist is a row: %+v", cards)
		}
	}

	// Once added it is a row, under the heading for services that have
	// something to configure.
	id := fixture.addService(translate.CustomSettings{
		BaseURL:     "https://api.example.com/v1",
		DisplayName: "本地 Ollama",
	})
	cards = pickerTranslateCards(state.credentialUI.TranslateCards())
	free, keyed := splitTranslateCards(cards)
	if len(free) != 2 {
		t.Errorf("free = %+v, want only the two free engines", free)
	}
	found := false
	for _, card := range keyed {
		if card.ID == id {
			found = true
		}
	}
	if !found {
		t.Errorf("the added service is not a row: %+v", keyed)
	}
}

// 免费 is the free engines' own word and nobody else's: the user's own service
// needs no key either, but it is not one of the free ones.
func TestOnlyTheFreeEnginesWearTheFreeChip(t *testing.T) {
	state, fixture, _ := translateState(t)
	// Add a service of the user's own so that it is a row at all.
	id := fixture.addService(translate.CustomSettings{
		BaseURL:     "https://api.example.com/v1",
		DisplayName: "本地 Ollama",
	})

	for _, card := range state.credentialUI.TranslateCards() {
		chipIsFree := isFreeTranslator(card.ID)
		switch card.ID {
		case freeMicrosoftID, freeBilibiliID:
			if !chipIsFree {
				t.Errorf("%s does not wear the free chip", card.ID)
			}
		case id:
			if chipIsFree {
				t.Error("the user's own service wears the free chip")
			}
			if card.NeedsCredential {
				t.Error("the user's own service is drawn as needing a key")
			}
		}
	}
}

// Two options mean two answers: picking one asks that engine, and nothing else
// is asked in its place when it fails.
func TestPickingAFreeEngineAsksThatEngineOnly(t *testing.T) {
	state, fixture, _ := translateState(t)

	if err := state.translate.SetProvider(t.Context(), freeBilibiliID); err != nil {
		t.Fatalf("SetProvider failed: %v", err)
	}
	// The B 站 engine fails; the Microsoft engine is never asked.
	fixture.translator(t, freeBilibiliID).fail(errTranslationUnavailable())
	fixture.translator(t, freeMicrosoftID).answer(okTranslation())

	if _, err := fixture.service.Translate(t.Context(), translate.Request{Text: "こんにちは"}); err == nil {
		t.Fatal("a failed engine answered")
	}
	if fixture.translator(t, freeBilibiliID).requestCount() != 1 {
		t.Errorf("the chosen engine was asked %d times, want once", fixture.translator(t, freeBilibiliID).requestCount())
	}
	if fixture.translator(t, freeMicrosoftID).requestCount() != 0 {
		t.Errorf("the other free engine was asked %d times, want none: nothing hands the text over",
			fixture.translator(t, freeMicrosoftID).requestCount())
	}
}

// And the other way round: picking the Microsoft engine does not fall back to
// B 站 when it fails.
func TestAFailedFreeEngineDoesNotFallBackToTheOther(t *testing.T) {
	state, fixture, _ := translateState(t)

	if err := state.translate.SetProvider(t.Context(), freeMicrosoftID); err != nil {
		t.Fatalf("SetProvider failed: %v", err)
	}
	fixture.translator(t, freeMicrosoftID).fail(errTranslationUnavailable())
	fixture.translator(t, freeBilibiliID).answer(okTranslation())

	if _, err := fixture.service.Translate(t.Context(), translate.Request{Text: "Hello."}); err == nil {
		t.Fatal("a failed engine answered")
	}
	if fixture.translator(t, freeMicrosoftID).requestCount() != 1 {
		t.Errorf("the chosen engine was asked %d times, want once", fixture.translator(t, freeMicrosoftID).requestCount())
	}
	if fixture.translator(t, freeBilibiliID).requestCount() != 0 {
		t.Errorf("the other free engine was asked %d times, want none", fixture.translator(t, freeBilibiliID).requestCount())
	}
}

// Switching between the two free engines is an ordinary choice: whatever was
// stored is what is asked.
func TestTheTwoFreeEnginesAreSwitchedBetween(t *testing.T) {
	state, fixture, _ := translateState(t)
	fixture.translator(t, freeMicrosoftID).answer(okTranslation())
	fixture.translator(t, freeBilibiliID).answer(okTranslation())

	for _, id := range []string{freeMicrosoftID, freeBilibiliID, freeMicrosoftID} {
		if err := state.translate.SetProvider(t.Context(), id); err != nil {
			t.Fatalf("SetProvider(%s) failed: %v", id, err)
		}
		settings, err := fixture.service.Settings()
		if err != nil {
			t.Fatalf("Settings failed: %v", err)
		}
		if settings.ProviderOr() != id {
			t.Errorf("the stored engine is %q, want %q", settings.ProviderOr(), id)
		}
		if _, err := fixture.service.Translate(t.Context(), translate.Request{Text: "Hello."}); err != nil {
			t.Fatalf("Translate failed with %s: %v", id, err)
		}
	}
	if fixture.translator(t, freeMicrosoftID).requestCount() != 2 || fixture.translator(t, freeBilibiliID).requestCount() != 1 {
		t.Errorf("calls = %d / %d, want each engine asked for its own turns",
			fixture.translator(t, freeMicrosoftID).requestCount(), fixture.translator(t, freeBilibiliID).requestCount())
	}
}

// The free engines are recognised by id, and the ids are the contract's, so
// that the settings file and the window cannot disagree about them.
func TestTheFreeEngineIDsComeFromTheContract(t *testing.T) {
	if !isFreeTranslator(contractfree.MicrosoftEdge) || !isFreeTranslator(contractfree.Bilibili) {
		t.Error("the contract's free engine ids are not the ones the window knows")
	}
	for _, id := range []string{"deepseek", customTranslatorID, "openai", ""} {
		if isFreeTranslator(id) {
			t.Errorf("%q is treated as a free engine", id)
		}
	}
}
