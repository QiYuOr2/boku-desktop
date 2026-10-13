package app

import (
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

// exact reports whether one drawn text is exactly this, rather than
// containing it: what a window must not offer is its own word, and a
// sentence about it is not the same thing.
func exact(texts []string, want string) bool {
	for _, text := range texts {
		if text == want {
			return true
		}
	}
	return false
}

// The settings window is a page of settings, and connecting a data source is
// one section of it: the title bar names the window, the section carries the
// provider's own mark and name, and the page says where a credential is going
// before it is handed over.
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
	// The window says what it is once — in the system's title bar — and not
	// again inside itself. Nor the address the provider asks, which is the
	// provider's business. These are matched whole: a sentence about the
	// clipboard may well contain the word "关闭" without offering a close
	// action.
	for _, unwanted := range []string{appName + " 设置", "配置数据源", "关闭", "API endpoint", "api.deepseek.com"} {
		if exact(texts, unwanted) {
			t.Errorf("the settings window still says %q: %q", unwanted, texts)
		}
	}
	// The provider's section is the first thing the window draws: nothing of
	// the window's own is above it, since its name is in the system's title
	// bar rather than in the view.
	marked := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)
	provider, ok := marked.Find("DeepSeek")
	if !ok {
		t.Fatalf("the provider's section was not found; the window shows %q", texts)
	}
	if provider.Y > pageEdge+4 {
		t.Errorf("the provider's section starts at y=%.0f: something is above it", provider.Y)
	}
}

// A data source is one section among the settings window's sections, not the
// window: the provider's own name heads it, and its rows sit at the page's
// margin rather than in a box.
func TestSettingsPutsTheProviderInItsOwnSection(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredential()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)

	provider := findText(t, tt, "DeepSeek")
	key := findText(t, tt, "API Key")
	warning := findTextPrefix(t, tt, "凭据目前以明文保存")

	if provider.X < pageEdge {
		t.Errorf("the provider's section reaches the page's edge at x=%.0f", provider.X)
	}
	// The section's own rows start at the page's margin; the provider's name
	// sits further right, beside its mark.
	if key.X > provider.X {
		t.Errorf("the field label at x=%.0f is indented under the provider's name at x=%.0f", key.X, provider.X)
	}
	if warning.Y < key.Y {
		t.Errorf("the fields are out of order: key y=%.0f, warning y=%.0f", key.Y, warning.Y)
	}
	// The credential fields belong to the provider's section, so they stay
	// above the footer's divider, which is drawn inside the page's margin.
	img := ui.Render(state.settingsView, settingsWidth, settingsHeight, 2)
	rows := pageRows(img, colorBorder, settingsWidth*2, 2)
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
	if state.form.stored != "…0001" {
		t.Errorf("the form shows %q, want only the last four characters", state.form.stored)
	}
	// The stored value is not in the field: the field is what a new key is
	// typed into, and it draws its own hint while it is empty.
	if state.form.apiKey != "" {
		t.Errorf("the field holds %q, want the empty field that draws its hint", state.form.apiKey)
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

// Typing is what replaces a stored credential, and it does so on its own: the
// window offers no 更换 to press, and until the field holds something there is
// nothing new to save. What is stored is said in one line, as its last four
// characters.
func TestSettingsATypedKeyIsTheReplacement(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredential()

	// The window as it opens: the stored credential is named, and the field
	// is empty, which is the state that has nothing new in it.
	if !state.credentialIsShown() {
		t.Fatal("a stored credential with an empty field is not read as stored")
	}
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)
	if !tt.HasText("已配置：…0001") {
		t.Errorf("the window does not say what is stored: %q", tt.Texts())
	}
	for _, gone := range []string{"更换", "删除", "确认", "取消"} {
		if tt.HasText(gone) {
			t.Errorf("the window still offers %q: %q", gone, tt.Texts())
		}
	}

	// Typing into the field is the whole of asking to replace it.
	state.form.apiKey = "sk-replaced-0002"
	if state.credentialIsShown() {
		t.Error("a typed key is still read as nothing new to save")
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
	if !contains(texts, "还没有可配置的数据源") {
		t.Errorf("the settings window does not say there is nothing to configure: %q", texts)
	}
}

// The window is closed by the close button the system draws in its title
// bar; the view therefore has no action of its own, and its footer is the
// version alone.
func TestSettingsHasNoCloseAction(t *testing.T) {
	state := testState(t, newTestProvider(), newTestStore(), "0.1.0")
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)
	if exact(tt.Texts(), "关闭") {
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

func TestCredentialLabel(t *testing.T) {
	cases := map[string]string{
		"apiKey":   "API Key",
		"adminKey": "Admin Key",
		"steamId":  "Steam ID",
		"":         "凭据",
		"other":    "other",
	}
	for key, want := range cases {
		if got := credentialLabel(key); got != want {
			t.Errorf("credentialLabel(%q) = %q, want %q", key, got, want)
		}
	}
}
