package app

import (
	credentialui "boku/internal/credential/ui"
	"testing"
	"time"

	"boku/internal/translate"
)

// 凭据输入框一次绑定一张卡片；卡片下留的值会在该卡片重绘时被重新显示出来，
// 因此保存后必须清掉，否则用户会看到刚保存的 key。

// 保存卡片时清空它的输入，无论当前输入框是否绑定在该卡片上。
func TestSavingACardForgetsWhatWasTypedIntoIt(t *testing.T) {
	state, _ := translateState(t)

	state.credentialUI.Form.Keys = map[string]string{"deepseek": "sk-typed-0001"}
	state.credentialUI.Form.InputCard = freeMicrosoftID
	state.credentialUI.Form.Input = ""

	state.credentialUI.ClearCardInput("deepseek")

	if _, still := state.credentialUI.Form.Keys["deepseek"]; still {
		t.Error("the saved value is still filed under its card")
	}
	// 输入框绑定的是另一张卡片，用户正在那里输入，因此不受影响。
	if state.credentialUI.Form.InputCard != freeMicrosoftID {
		t.Errorf("the field moved to %q", state.credentialUI.Form.InputCard)
	}
}

// 当保存的正是当前显示的卡片，输入框也随之清空：key 已存储，输入框不应再保留它。
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

// 从未输入过的卡片返回空结果而不是 panic：操作会读取输入框，无论用户是否到过该卡片。
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
	// 翻译器会解析到保存的地址，并使用用户给它的名字。
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
