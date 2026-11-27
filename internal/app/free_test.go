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

const (
	freeMicrosoftID = contractfree.MicrosoftEdge
	freeBilibiliID  = contractfree.Bilibili
)

// okTranslation 是免费引擎正常工作时返回的结果。
func okTranslation() domain.TranslationResult {
	return domain.TranslationResult{
		TranslatedText: "你好",
		SourceLang:     domain.LangEnglish,
		TargetLang:     domain.LangChinese,
	}
}

// errTranslationUnavailable 是免费引擎无法作答时的失败（端点拒绝），也是用户看到的错误。
func errTranslationUnavailable() error {
	return fmt.Errorf("%w: the endpoint answered 429", domain.ErrProviderRateLimited)
}

// aTranslationSettings 读取某个 state 正在使用的设置。
func aTranslationSettings(t *testing.T, state *State) translate.Settings {
	t.Helper()
	settings, err := state.translate.Settings()
	if err != nil {
		t.Fatalf("Settings failed: %v", err)
	}
	return settings
}

// cardOf 按 id 取窗口中的一张卡片。
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

// 免费引擎是设置窗口里的两个独立选项，位于同一分组；API Key 分区不包含它们。

// 两个免费引擎是独立选项，也是新安装时的默认选择。
func TestTheFreeEnginesAreTwoSeparateChoices(t *testing.T) {
	state, _ := translateState(t)
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
	// 名字取自设计稿：两个免费选项的名字。
	if got := byID[freeMicrosoftID].DisplayName; got != "微软 Edge 翻译" {
		t.Errorf("the first free option is called %q", got)
	}
	if got := byID[freeBilibiliID].DisplayName; got != "B站 Index-Translate" {
		t.Errorf("the second free option is called %q", got)
	}
	// 新安装使用微软引擎，它是两个免费选项之一，而非更高一档。
	if got := aTranslationSettings(t, state).ProviderOr(); got != freeMicrosoftID {
		t.Errorf("a fresh installation uses %q, want the Microsoft engine", got)
	}
}

// 选择器的两个分组：免费引擎在上，有内容需要填写的服务在下。
func TestThePickerGroupsFreeEnginesSeparately(t *testing.T) {
	state, fixture := translateState(t)
	// 用户自定义服务只能通过添加产生，因为没有人为之配置就不存在卡片。
	own := fixture.addService(translate.CustomSettings{
		BaseURL:     "https://api.example.com/v1",
		DisplayName: "本地 Ollama",
	})

	free, keyed := splitTranslateCards(state.credentialUI.TranslateCards())
	if len(free) != 2 {
		t.Fatalf("free = %+v, want the two free engines", free)
	}
	for _, card := range free {
		if card.ID != freeMicrosoftID && card.ID != freeBilibiliID {
			t.Errorf("%s is in the free group and is not a free engine", card.ID)
		}
	}
	ids := map[string]bool{}
	for _, card := range keyed {
		ids[card.ID] = true
	}
	// DeepSeek 需要 key；用户自己的服务有请求地址，两者都属于「需要配置」分组，
	// 即便后者 key 可选。
	for _, want := range []string{"deepseek", own} {
		if !ids[want] {
			t.Errorf("%s is not in the group that needs configuring: %+v", want, ids)
		}
	}
	for id := range ids {
		if id == freeMicrosoftID || id == freeBilibiliID {
			t.Errorf("%s is a free engine and is in the group that needs a key", id)
		}
	}
}

// API Key 分区只绘制有 key 的服务；免费引擎在翻译分区中选择。
func TestTheAPIKeySectionLeavesTheFreeEnginesOut(t *testing.T) {
	state, fixture := translateState(t)
	own := fixture.addService(translate.CustomSettings{
		BaseURL:     "https://api.example.com/v1",
		DisplayName: "本地 Ollama",
	})

	keyed := credentialui.KeyedTranslateCards(state.credentialUI.TranslateCards())
	for _, card := range keyed {
		if card.ID == freeMicrosoftID || card.ID == freeBilibiliID {
			t.Errorf("the API Key section draws %s, a free engine", card.ID)
		}
	}
	ids := map[string]bool{}
	for _, card := range keyed {
		ids[card.ID] = true
	}
	// 该分区绘制的卡片：有 key 的服务，以及用户自己运行的服务。
	for _, want := range []string{"deepseek", own} {
		if !ids[want] {
			t.Errorf("the API Key section lost %s: %+v", want, ids)
		}
	}
	if len(keyed) != 2 {
		t.Errorf("the API Key section draws %d cards, want the keyed ones: %+v", len(keyed), keyed)
	}
}

// 用户自己的服务排在 API Key 分区的最后，无论注册表给出翻译器的顺序如何；
// 用于新增的空白卡片绘制在它们之后。
func TestTheOwnServiceCardsComeLast(t *testing.T) {
	state, fixture := translateState(t)

	// 未配置的服务不成卡片：没有内容可显示，也无从请求。
	keyed := credentialui.KeyedTranslateCards(state.credentialUI.TranslateCards())
	if len(keyed) == 0 {
		t.Fatal("the API Key section has no cards")
	}
	for _, card := range keyed {
		if translate.IsCustomService(card.ID) {
			t.Fatalf("an unconfigured own service is a card: %+v", keyed)
		}
	}

	// 添加后，它们都排在所有具名服务之后。
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

// 用户自己的服务有地址即为已配置，与其他卡片状态徽标一致：添加时即已配置，
// 因此其徽标不会显示「未配置」。
func TestAnAddedOwnServiceIsConfigured(t *testing.T) {
	state, fixture := translateState(t)

	id := fixture.addService(translate.CustomSettings{
		BaseURL:     "https://api.example.com/v1",
		DisplayName: "本地 Ollama",
	})
	card := cardOf(t, state, id)
	if !card.Configured {
		t.Errorf("a service the user just added is not configured: %+v", card)
	}
	// 名字是用户自己起的，不是本功能对服务的称呼。
	if card.DisplayName != "本地 Ollama" {
		t.Errorf("the card is called %q, want the name the user gave it", card.DisplayName)
	}
}

// 每张有内容可移除的卡片都提供删除；共两类卡片。
func TestDeleteIsOnEveryCardThatHasSomethingToRemove(t *testing.T) {
	state, fixture := translateState(t)

	// 用户自己的服务，完全没 key：没有存储任何内容，仍可移除。
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

	// 内置服务没有存 key 时，删除按钮无事可做。
	if credentialui.CardCanBeRemoved(cardOf(t, state, "deepseek"), false) {
		t.Error("a card with nothing stored offers to remove something")
	}
	// 一旦持有 key，即可移除。
	if !credentialui.CardCanBeRemoved(cardOf(t, state, "deepseek"), true) {
		t.Error("a card holding a key cannot give it up")
	}
}

// 移除用户自己无 key 的服务可以成功：服务被删掉，其下缺失的 key 不报为失败。
// 凭据存储对于从未有过的名字会返回 ErrSecretNotFound。
func TestRemovingAKeylessOwnServiceWorks(t *testing.T) {
	state, fixture := translateState(t)
	id := fixture.addService(translate.CustomSettings{
		BaseURL:     "http://127.0.0.1:11434/v1",
		DisplayName: "本地 Ollama",
	})

	// 不含窗口交接，只测移除在 goroutine 上做的事：设置里去掉该服务，
	// 从未有过的 key 不视为错误。
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

// 注册表同步由 assembly 包的 ownservices_test.go 覆盖。
func TestRemovedTranslatorIsAbsentFromCardsAndPicker(t *testing.T) {
	state, fixture := translateState(t)
	id := fixture.addService(translate.CustomSettings{
		BaseURL:     "http://127.0.0.1:11434/v1",
		DisplayName: "本地 Ollama",
	})

	// 一开始它同时出现在两个位置。
	if !hasCard(state, id) {
		t.Fatalf("the service that was just added is not a card: %+v", state.credentialUI.TranslateCards())
	}
	if !hasPickerRow(state, id) {
		t.Fatalf("the service that was just added is not a row: %+v", pickerTranslateCards(state.credentialUI.TranslateCards()))
	}

	// 不含窗口交接，只测移除在 goroutine 上做的事。
	if err := fixture.service.RemoveCustomTranslator(t.Context(), id); err != nil {
		t.Fatalf("RemoveCustomTranslator failed: %v", err)
	}
	fixture.registry.Remove(id)

	if hasCard(state, id) {
		t.Errorf("the removed service is still a card: %+v", state.credentialUI.TranslateCards())
	}
	if hasPickerRow(state, id) {
		t.Errorf("the removed service is still a row: %+v", pickerTranslateCards(state.credentialUI.TranslateCards()))
	}
	// 其他卡片不受影响：移除一个服务不会清空该分区。
	if !hasCard(state, freeMicrosoftID) && !hasCard(state, "deepseek") {
		t.Error("the removal took the named services with it")
	}
}

// hasCard 报告 API Key 分区是否为某 id 绘制了卡片。
func hasCard(state *State, id string) bool {
	for _, card := range credentialui.KeyedTranslateCards(state.credentialUI.TranslateCards()) {
		if card.ID == id {
			return true
		}
	}
	return false
}

// hasPickerRow 报告翻译分区是否提供某 id。
func hasPickerRow(state *State, id string) bool {
	for _, card := range pickerTranslateCards(state.credentialUI.TranslateCards()) {
		if card.ID == id {
			return true
		}
	}
	return false
}

// 用户自己的服务只有存在后才是选项：它由 API Key 分区创建，在此之前无从选择。
func TestAnUnaddedOwnServiceIsNotARow(t *testing.T) {
	state, fixture := translateState(t)

	// 尚未添加：没有用户自己的选项。
	cards := pickerTranslateCards(state.credentialUI.TranslateCards())
	for _, card := range cards {
		if translate.IsCustomService(card.ID) {
			t.Fatalf("a service that does not exist is a row: %+v", cards)
		}
	}

	// 添加后成为选项，归属「有内容需要配置」的分组。
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

// 两个选项意味着两个独立答案：选哪个就问哪个引擎，失败时也不会用另一个顶替。
func TestPickingAFreeEngineAsksThatEngineOnly(t *testing.T) {
	state, fixture := translateState(t)

	if err := state.translate.SetProvider(t.Context(), freeBilibiliID); err != nil {
		t.Fatalf("SetProvider failed: %v", err)
	}
	// B 站引擎失败；微软引擎完全不会被询问。
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

// 反过来也一样：选择微软引擎失败时不会回退到 B 站。
func TestAFailedFreeEngineDoesNotFallBackToTheOther(t *testing.T) {
	state, fixture := translateState(t)

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

// 在两个免费引擎间切换是普通选择：存的是哪个就问哪个。
func TestTheTwoFreeEnginesAreSwitchedBetween(t *testing.T) {
	state, fixture := translateState(t)
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
