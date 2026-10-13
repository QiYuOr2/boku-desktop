package app

import (
	"context"
	"image"
	"testing"

	"github.com/egoist/mygo/ui"

	"boku/internal/domain/deepseek"
)

func settingsText(t *testing.T, state *State) []string {
	t.Helper()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)
	return tt.Texts()
}

// The settings window is a page of settings, and connecting a data source is
// one section of it: no header of its own — the title bar names the window —
// and the provider in a block that carries its own name and mark. The window
// also says where a credential is going before it is handed over.
func TestSettingsShowsTheConnectForm(t *testing.T) {
	state := testState(t, newTestProvider(), newTestStore(), "0.1.0")
	texts := settingsText(t, state)

	for _, want := range []string{
		"DeepSeek",
		"API Key",
		"测试连接",
		"保存",
		"Boku 0.1.0",
	} {
		if !contains(texts, want) {
			t.Errorf("the settings window does not show %q; it shows %q", want, texts)
		}
	}
	// Neither the window's own name nor a page header: the title bar says
	// what this window is, and the sections say what is in it. Nor the
	// address the provider asks, which is the provider's business.
	for _, unwanted := range []string{appName + " 设置", "配置数据源", "关闭", "API endpoint", "api.deepseek.com"} {
		if contains(texts, unwanted) {
			t.Errorf("the settings window still says %q: %q", unwanted, texts)
		}
	}
	marked := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)
	provider, ok := marked.Find("DeepSeek")
	if !ok {
		t.Fatalf("the provider's block was not found; the window shows %q", texts)
	}
	if provider.Y > 40 {
		t.Errorf("the provider's block starts at y=%.0f: something is above it", provider.Y)
	}
}

// A data source is one block among the settings window's blocks, not the
// window: the provider's own name heads it, and its fields sit at the
// window's margin rather than in a box.
func TestSettingsPutsTheProviderInItsOwnSection(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredential()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)

	provider := findText(t, tt, "DeepSeek")
	key := findText(t, tt, "API Key")
	warning := findTextPrefix(t, tt, "凭据目前以明文保存")

	if provider.Y > 40 {
		t.Errorf("the provider's section starts at y=%.0f: something is above it", provider.Y)
	}
	if provider.X < 8 {
		t.Errorf("the provider's section reaches the window's left edge at x=%.0f", provider.X)
	}
	// The section's own rows start at the section's margin; the provider's
	// name sits further right, beside its mark.
	if key.X > provider.X {
		t.Errorf("the field label at x=%.0f is indented under the provider's name at x=%.0f", key.X, provider.X)
	}
	if warning.Y < key.Y {
		t.Errorf("the fields are out of order: key y=%.0f, warning y=%.0f", key.Y, warning.Y)
	}
	// The credential fields belong to the provider's section, so they stay
	// above the footer's divider, which is drawn edge to edge.
	img := ui.Render(state.settingsView, settingsWidth, settingsHeight, 2)
	rows := dividerRows(img, ui.LightTheme().Border, settingsWidth*2)
	if len(rows) == 0 {
		t.Fatal("the settings window draws no divider at all")
	}
	underProvider := false
	for _, y := range rows {
		if at := float32(y) / 2; at > warning.Y {
			underProvider = true
		}
	}
	if !underProvider {
		t.Errorf("nothing separates the provider's section from the footer: dividers at %v, warning at y=%.0f",
			rows, warning.Y)
	}
}

// The credential is stored in the clear today, and the window has to say so
// rather than imply otherwise.
func TestSettingsAdmitsTheCredentialIsNotEncrypted(t *testing.T) {
	state := testState(t, newTestProvider(), newTestStore(), "0.1.0")
	texts := settingsText(t, state)
	if !contains(texts, "明文") {
		t.Errorf("the settings window does not warn about plain-text storage: %q", texts)
	}
}

// A stored credential is shown as its last four characters, never in full.
func TestSettingsShowsAStoredCredentialMasked(t *testing.T) {
	store := withCredential(newTestStore())
	state := testState(t, newTestProvider(), store, "0.1.0")
	state.loadCredential()

	if !state.form.hasCredential {
		t.Fatal("the form does not see the stored credential")
	}
	if state.form.masked != "…0001" {
		t.Errorf("the form shows %q, want only the last four characters", state.form.masked)
	}
	texts := settingsText(t, state)
	if !contains(texts, "已配置：…0001") {
		t.Errorf("the settings window does not show the masked credential: %q", texts)
	}
	for _, text := range texts {
		if contains([]string{text}, "sk-test-0001") {
			t.Fatalf("the settings window shows the credential in full: %q", text)
		}
	}
}

// Deleting asks once before it does anything.
func TestSettingsAsksBeforeDeleting(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredential()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)

	if err := tt.Click("删除"); err != nil {
		t.Fatalf("click 删除: %v", err)
	}
	tt.Frame()
	if !state.form.confirmDelete {
		t.Fatal("clicking 删除 did not ask for confirmation")
	}
	if !tt.HasText("确认") || !tt.HasText("取消") {
		t.Errorf("the confirmation does not offer both answers: %q", tt.Texts())
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatalf("click 取消: %v", err)
	}
	tt.Frame()
	if state.form.confirmDelete {
		t.Error("取消 did not call the deletion off")
	}
	// The credential is still there: nothing was deleted.
	if _, found, err := state.secretService.Masked(context.Background(), "deepseek", "apiKey"); err != nil || !found {
		t.Errorf("the credential was removed without confirmation: found=%v err=%v", found, err)
	}
}

// Saving without a key says what is missing instead of doing nothing.
func TestSettingsRefusesToSaveAnEmptyKey(t *testing.T) {
	state := testState(t, newTestProvider(), newTestStore(), "0.1.0")
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)

	if err := tt.Click("保存"); err != nil {
		t.Fatalf("click 保存: %v", err)
	}
	tt.Frame()
	if state.form.status != statusError {
		t.Fatalf("status = %d, want an error", state.form.status)
	}
	if !contains([]string{state.form.statusText}, "API Key") {
		t.Errorf("the form does not say what is missing: %q", state.form.statusText)
	}
}

func TestSettingsWithoutAPanel(t *testing.T) {
	state := testState(t, newTestProvider(), newTestStore(), "0.1.0")
	state.panelService = emptyPanelService(t)
	texts := settingsText(t, state)
	if !contains(texts, "还没有可以配置的数据源") {
		t.Errorf("the settings window does not say there is nothing to configure: %q", texts)
	}
}

// The window is closed by the close button the system draws in its title
// bar; the view therefore has no action of its own, and its footer is the
// version alone.
func TestSettingsHasNoCloseAction(t *testing.T) {
	state := testState(t, newTestProvider(), newTestStore(), "0.1.0")
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)
	if tt.HasText("关闭") {
		t.Errorf("the settings window offers 关闭 in its own view: %q", tt.Texts())
	}
	if version, ok := tt.Find("Boku 0.1.0"); !ok {
		t.Errorf("the settings footer does not show the version: %q", tt.Texts())
	} else if version.W > 100 {
		t.Errorf("the version is %.0f wide: something was added beside it", version.W)
	}
}

// The header shows the provider's mark inside its block, drawn in the
// provider's own color: it is what tells the user which account they are
// configuring.
func TestSettingsHeaderDrawsTheProviderMark(t *testing.T) {
	state := testState(t, newTestProvider(), newTestStore(), "0.1.0")
	img := ui.Render(state.settingsView, settingsWidth, settingsHeight, 2)
	if got := countColor(img, deepseekBrandColor); got < 200 {
		t.Errorf("the DeepSeek mark covers %d pixels, want the logo to be drawn", got)
	}
	if state.providerMark("weread").svg != nil {
		t.Error("a provider the application has no mark for got one anyway")
	}
	if state.providerMark("deepseek").svg == nil {
		t.Error("the DeepSeek mark was not found for the DeepSeek provider")
	}
}

// countColor counts the pixels drawn in exactly this color: a mark drawn in
// a flat color has a large interior of it, so a handful of pixels means it
// was not drawn at all.
func countColor(img *image.RGBA, want ui.Color) int {
	count := 0
	for i := 0; i+3 < len(img.Pix); i += 4 {
		if img.Pix[i] == want.R && img.Pix[i+1] == want.G && img.Pix[i+2] == want.B && img.Pix[i+3] == want.A {
			count++
		}
	}
	return count
}

func TestCredentialPathIsShownAndNeverEmpty(t *testing.T) {
	state := testState(t, newTestProvider(), newTestStore(), "0.1.0")
	if got := state.credentialPath(); got == "" {
		t.Error("the warning has no path to name")
	}
	texts := settingsText(t, state)
	if !contains(texts, state.credentialPath()) {
		t.Errorf("the warning does not name where the credentials live: %q", texts)
	}
}

func TestTargetFollowsThePanel(t *testing.T) {
	state := testState(t, newTestProvider(), newTestStore(), "0.1.0")
	configured, ok := state.target()
	if !ok {
		t.Fatal("the form found nothing to configure")
	}
	if configured.providerID != "deepseek" || configured.credentialKey != "apiKey" {
		t.Errorf("target = %+v", configured)
	}
	if configured.displayName != "DeepSeek" {
		t.Errorf("display name = %q", configured.displayName)
	}
	if configured.endpoint == "" || configured.docsURL == "" {
		t.Errorf("target = %+v, want an endpoint and a documentation link", configured)
	}
	if configured.panelID != deepseek.PanelID {
		t.Errorf("panel id = %q, want %q", configured.panelID, deepseek.PanelID)
	}
}
