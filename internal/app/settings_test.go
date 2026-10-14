package app

import (
	"errors"
	"image"
	"testing"

	"github.com/egoist/mygo/ui"
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
	// The key section is the first thing the window draws: nothing of the
	// window's own is above it, since its name is in the system's title bar
	// rather than in the view.
	marked := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)
	head, ok := marked.Find("API Key")
	if !ok {
		t.Fatalf("the key section was not found; the window shows %q", texts)
	}
	if head.Y > pageEdge+4 {
		t.Errorf("the key section starts at y=%.0f: something is above it", head.Y)
	}
}

// A service is one section among the settings window's sections, and one card
// among the services the section configures: the card's own name heads it, its
// rows sit at the page's margin rather than in a box of their own, and the
// field's label starts at that margin rather than under the card's name.
func TestSettingsPutsTheProvidersInCards(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredentials()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)

	// The section's own rows start at the page's margin; the service's name
	// sits further right, inside the card's own padding. The field's label is
	// the one in that card, so the card's name is found by the title it
	// carries and the label by the section head's subtitle: several labels on
	// the page say "API Key".
	card := findText(t, tt, "DeepSeek")
	field := findText(t, tt, "已配置：…0001")
	section := findText(t, tt, "配置各 AI 服务密钥")
	warning := findTextPrefix(t, tt, "凭据目前以明文保存")

	if card.X < pageEdge {
		t.Errorf("the service's card reaches the page's edge at x=%.0f", card.X)
	}
	if field.X > card.X {
		t.Errorf("the card's rows at x=%.0f are indented under the card's own name at x=%.0f", field.X, card.X)
	}
	if section.Y >= card.Y {
		t.Errorf("the first card at y=%.0f is not under the section's head at y=%.0f", card.Y, section.Y)
	}
	if warning.Y < section.Y {
		t.Errorf("the fields are out of order: head y=%.0f, warning y=%.0f", section.Y, warning.Y)
	}
}

// A data source is one section among the settings window's sections, not the
// window: the cards sit inside the key section rather than above it.
func TestSettingsCardsComeAfterTheSectionHead(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredentials()
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)

	head := findText(t, tt, "API Key")
	card := findText(t, tt, "DeepSeek")
	if card.Y <= head.Y {
		t.Errorf("the first card at y=%.0f is not under the section's head at y=%.0f", card.Y, head.Y)
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

// A provider's mark is drawn in the provider's own color, at the head of its
// card: it is the logo the user knows, and it is not replaced or recolored.
func TestSettingsCardsDrawTheProviderMark(t *testing.T) {
	state := testState(t, newTestProvider(), newTestStore(), "0.1.0")
	const scale = 2
	img := ui.Render(state.settingsView, settingsWidth, settingsHeight, scale)
	if got := countColor(img, deepseekBrandColor); got < 200 {
		t.Errorf("the DeepSeek mark covers %d pixels, want the logo drawn in its own color", got)
	}
	if state.providerMark("weread").svg != nil {
		t.Error("a provider the application has no mark for got one anyway")
	}
	if state.providerMark("deepseek").svg == nil {
		t.Error("the DeepSeek mark was not found for the DeepSeek provider")
	}
}

// A stored credential is shown as its last four characters, never in full.
func TestSettingsShowsAStoredCredentialMasked(t *testing.T) {
	store := withCredential(newTestStore())
	state := testState(t, newTestProvider(), store, "0.1.0")
	state.loadCredentials()

	if !state.form.hasCredential["deepseek"] {
		t.Fatal("the form does not see the stored credential")
	}
	if got := state.form.stored["deepseek"]; got != "…0001" {
		t.Errorf("the form shows %q, want only the last four characters", got)
	}
	// The stored value is not in the field: the field is what a new key is
	// typed into, and it draws its own hint while it is empty.
	if typed := state.cardInput("deepseek"); typed != "" {
		t.Errorf("the field holds %q, want the empty field that draws its hint", typed)
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
	state.loadCredentials()
	deepseekCard := translateCard{id: "deepseek", credentialKey: "apiKey", needsCredential: true}

	// The window as it opens: the stored credential is named, and the field
	// is empty, which is the state that has nothing new in it.
	if !state.credentialIsShown(deepseekCard) {
		t.Fatal("a stored credential with an empty field is not read as stored")
	}
	tt := ui.NewTester(state.settingsView, settingsWidth, settingsHeight)
	if !tt.HasText("已配置：…0001") {
		t.Errorf("the window does not say what is stored: %q", tt.Texts())
	}
	for _, gone := range []string{"更换", "删除", "确认", "取消"} {
		// The window draws these as actions, so an action by that name is
		// what is looked for: a sentence that happens to contain the word
		// ("冲突时请更换") is not an action.
		for _, label := range []string{gone, "快捷键 " + gone} {
			if exact(tt.Texts(), label) {
				t.Errorf("the window still offers %q as an action: %q", label, tt.Texts())
			}
		}
	}

	// Typing into the field is the whole of asking to replace it. The field
	// belongs to one card at a time, so what was typed is filed under the
	// card it was typed into.
	state.showCardInput("deepseek")
	state.form.input = "sk-replaced-0002"
	if state.credentialIsShown(deepseekCard) {
		t.Error("a typed key is still read as nothing new to save")
	}
	// Moving to the other card files it, and coming back finds it again.
	state.showCardInput("custom")
	if got := state.cardInput("deepseek"); got != "sk-replaced-0002" {
		t.Errorf("the typed key was lost when the field moved: %q", got)
	}
	if got := state.keyValue("deepseek"); got != "sk-replaced-0002" {
		t.Errorf("reading the card gives %q", got)
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
	card, ok := state.form.status["deepseek"]
	if !ok {
		t.Fatalf("the card said nothing; the window shows %q", tt.Texts())
	}
	if card.status != statusError {
		t.Fatalf("status = %d, want an error", card.status)
	}
	if !contains([]string{card.text}, "API Key") {
		t.Errorf("the form does not say what is missing: %q", card.text)
	}
}

// A service with no key is not a service the picker offers, and the window
// says which one to configure rather than failing later.
func TestSettingsReportsWhichServiceIsConfigured(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	texts := settingsText(t, state)
	// DeepSeek has the panel's key and the user's own service has no
	// address, so the two cards say different things.
	if !contains(texts, "已配置") || !contains(texts, "未配置") {
		t.Errorf("the cards do not report their state: %q", texts)
	}
	if !contains(texts, "仅显示已配置 API Key 的服务可选") {
		t.Errorf("the picker does not say what it is showing: %q", texts)
	}
}

// The five sections the design asks for, and the sections it does not: the
// reading and rent pages are not part of this build.
func TestSettingsShowsTheFiveSections(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.loadCredentials()
	texts := settingsText(t, state)
	for _, want := range []string{
		"API Key", "配置各 AI 服务密钥",
		"划词翻译", "任意语言 → 中文 · AI 翻译",
		"剪贴板", "监听与预览设置",
		"通用", "启动与外观",
		"关于", "版本与构建",
	} {
		if !contains(texts, want) {
			t.Errorf("the settings window does not show %q; it shows %q", want, texts)
		}
	}
	for _, unwanted := range []string{"阅读", "房租", "每日目标（分钟）", "检查更新"} {
		if exact(texts, unwanted) {
			t.Errorf("the settings window shows %q, which this build does not do", unwanted)
		}
	}
}

// The shortcut row: what the key does, what it is bound to, and how to record
// another one. The other keys the design draws are not implemented, and the
// window says so instead of drawing buttons that do nothing.
func TestSettingsShowsTheShortcutRow(t *testing.T) {
	state, _, shortcuts := testStateWith(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	state.RegisterShortcuts()
	texts := settingsText(t, state)
	for _, want := range []string{
		"快捷键",
		"划词后弹出翻译",
		"选中文字后按此键显示浮窗",
		"Ctrl + Shift + T",
		"恢复默认快捷键",
		"点击快捷键可重新录制；与系统或其他软件冲突时请更换。",
		"朗读、固定与关闭的组合键尚未实现。",
	} {
		if !contains(texts, want) {
			t.Errorf("the shortcut row does not show %q; it shows %q", want, texts)
		}
	}
	if got := shortcuts.bound(); got != "Ctrl+Shift+T" {
		t.Errorf("the registered shortcut is %q, want the default", got)
	}
	// The design's other keys are not registered either: one binding is the
	// whole of what this build does with the keyboard.
	if registrations, _ := shortcuts.counts(); registrations != 1 {
		t.Errorf("the application registered %d shortcuts, want one", registrations)
	}
}

// A registration the platform refuses is visible rather than silent, and the
// application keeps running: docs/tools.md §6.
func TestSettingsShowsARegistrationFailure(t *testing.T) {
	state, _, shortcuts := testStateWith(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	shortcuts.failWith(errors.New("already registered by another application"))
	state.RegisterShortcuts()

	texts := settingsText(t, state)
	if !contains(texts, "无法注册") {
		t.Errorf("a failed registration is not reported: %q", texts)
	}
	if shortcuts.bound() != "" {
		t.Errorf("something was bound although registration failed: %q", shortcuts.bound())
	}
	// The window still draws: a tray application without a shortcut starts,
	// and the section it failed to register is on the page.
	if texts := settingsText(t, state); len(texts) == 0 {
		t.Error("the settings window draws nothing")
	}
}

func TestSettingsWithoutAPanel(t *testing.T) {
	state := testState(t, newTestProvider(), newTestStore(), "0.1.0")
	state.panelService = emptyPanelService(t)
	texts := settingsText(t, state)
	if !contains(texts, "剪贴板") {
		t.Errorf("the window does not draw its own sections: %q", texts)
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

// The cards the window draws are the translators the registry has, with what
// each one needs: the picker and the key section are the same list, read
// twice.
func TestTranslateCardsFollowTheRegistry(t *testing.T) {
	state := testState(t, newTestProvider(), newTestStore(), "0.1.0")
	cards := state.translateCards()
	if len(cards) != 2 {
		t.Fatalf("cards = %+v, want both translators", cards)
	}
	// The one in use comes first.
	if cards[0].id != "deepseek" || cards[0].displayName != "DeepSeek" {
		t.Errorf("the first card is %+v", cards[0])
	}
	if cards[0].credentialKey != "apiKey" || !cards[0].needsCredential {
		t.Errorf("the first card does not ask for a key: %+v", cards[0])
	}
	// The user's own service needs no key and has no address yet.
	if cards[1].id != "custom" || cards[1].needsCredential || cards[1].configured {
		t.Errorf("the custom card is %+v, want an unconfigured service that needs no key", cards[1])
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
