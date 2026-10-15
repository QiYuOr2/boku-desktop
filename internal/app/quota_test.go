package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"boku/internal/credential"
	"boku/internal/domain"
	"boku/internal/panel"
	codexdomain "boku/internal/panel/codex"
	"boku/internal/panel/deepseek"
)

// The quota block is driven here without a network and without the codex
// packages: a panel whose headline is a percentage, an adapter that fills the
// three kinds of value, and a credential source the test owns. The provider
// and the adapter themselves are tested where they live.
//
// Nothing here names the panel: the block is chosen by the headline's unit, so
// these tests are about "a proportion panel", not about one vendor.

// quotaFetchedAt is the moment the fake answer was taken.
var quotaFetchedAt = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

// quotaProvider is a provider whose answer the adapter below decides: it
// exists so that the panel can be refreshed without a network.
type quotaProvider struct{ err error }

func (p *quotaProvider) ID() string { return codexdomain.ProviderID }

func (p *quotaProvider) Descriptor() domain.ProviderDescriptor {
	return domain.ProviderDescriptor{
		ID:                codexdomain.ProviderID,
		DisplayName:       "Codex",
		CredentialKeys:    []string{codexdomain.AccessTokenCredential},
		DocsURL:           "https://example.com/codex",
		UsageURL:          codexdomain.UsageURL,
		Endpoint:          "https://example.com/backend-api/wham/usage",
		MinRefreshSeconds: 60,
	}
}

func (p *quotaProvider) Fetch(context.Context, domain.Credentials) (domain.FetchResult, error) {
	if p.err != nil {
		return domain.FetchResult{}, p.err
	}
	return domain.FetchResult{
		ProviderID: codexdomain.ProviderID,
		Endpoint:   p.Descriptor().Endpoint,
		FetchedAt:  quotaFetchedAt,
		Payload:    []byte(`{}`),
		HTTPStatus: 200,
	}, nil
}

// quotaAdapter fills the panel's three metrics with what the test asked for.
type quotaAdapter struct {
	remaining int64
	resetsIn  int64
	plan      string
}

func (a quotaAdapter) ProviderID() string { return codexdomain.ProviderID }

func (a quotaAdapter) Normalize(result domain.FetchResult, panel domain.Panel) (domain.Snapshot, error) {
	values := map[string]domain.Value{
		codexdomain.MetricSessionRemainingPercent: domain.NumberValue(a.remaining),
		codexdomain.MetricSessionResetsInSeconds:  domain.DurationValue(a.resetsIn),
		codexdomain.MetricPlanName:                domain.TextValue(a.plan),
	}
	snapshot := domain.Snapshot{
		PanelID:    panel.ID,
		ProviderID: codexdomain.ProviderID,
		ObservedAt: result.FetchedAt,
		Status:     domain.StatusOK,
	}
	for _, key := range panel.MetricKeys {
		snapshot.Observations = append(snapshot.Observations, domain.Observation{
			MetricKey:  key,
			Value:      values[key],
			ObservedAt: result.FetchedAt,
		})
	}
	return snapshot, nil
}

// quotaSource is a credential source the test decides: it hands over a token,
// or says the tool is not logged in, and it records whether its login was
// started and how often its credential was read — which is what a panel the
// user turned off must never do.
type quotaSource struct {
	credentialsErr error
	startErr       error
	started        int
	reads          int
}

func (s *quotaSource) ProviderID() string { return codexdomain.ProviderID }

func (s *quotaSource) Credentials(context.Context) (domain.Credentials, error) {
	s.reads++
	if s.credentialsErr != nil {
		return domain.Credentials{}, s.credentialsErr
	}
	return domain.Credentials{
		ProviderID: codexdomain.ProviderID,
		Secrets:    map[string]string{codexdomain.AccessTokenCredential: "token"},
	}, nil
}

func (s *quotaSource) StartLogin() error {
	s.started++
	return s.startErr
}

// quotaState builds the quick window over one quota panel, with an adapter
// whose answer the test decides and a credential source it owns.
func quotaState(t *testing.T, adapter quotaAdapter, provider *quotaProvider, source *quotaSource) *State {
	t.Helper()
	if provider == nil {
		provider = &quotaProvider{}
	}
	if source == nil {
		source = &quotaSource{}
	}
	secrets, err := credential.NewSecrets(newTestStore())
	if err != nil {
		t.Fatalf("NewSecrets failed: %v", err)
	}
	panels, err := panel.NewPanels(
		testRegistry{provider: provider, adapter: adapter},
		newTestStore(),
		nil,
		[]panel.PanelDef{{Panel: codexdomain.Panel(), Metrics: codexdomain.Metrics()}},
		source,
	)
	if err != nil {
		t.Fatalf("NewPanels failed: %v", err)
	}
	state, err := New(mygo.App, Deps{
		Panels:    panels,
		Secrets:   secrets,
		Clipboard: testClipboard(t, nil, nil),
		Version:   "0.1.0",
		Quit:      func() {},
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	return state
}

// loadQuota refreshes the one panel, so that the view has an answer to draw.
func loadQuota(t *testing.T, state *State) {
	t.Helper()
	if _, err := state.panelService.Refresh(context.Background(), codexdomain.PanelID); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}
}

// The switch the settings window shows for a panel that is the user's to
// allow: turning it off takes the block out of the quick window and stops
// anything from reading the credential the panel lives on.
func TestThePanelSwitchTakesTheBlockAway(t *testing.T) {
	provider := &quotaProvider{}
	source := &quotaSource{}
	tools := &testToolStore{}
	panels, err := panel.NewPanels(
		testRegistry{provider: provider, adapter: quotaAdapter{remaining: 68, resetsIn: 7200, plan: "plus"}},
		newTestStore(),
		tools,
		[]panel.PanelDef{{
			Panel:    codexdomain.Panel(),
			Metrics:  codexdomain.Metrics(),
			Optional: true,
		}},
		source,
	)
	if err != nil {
		t.Fatalf("NewPanels failed: %v", err)
	}
	if err := panels.LoadChoices(context.Background()); err != nil {
		t.Fatalf("LoadChoices failed: %v", err)
	}
	secrets, err := credential.NewSecrets(newTestStore())
	if err != nil {
		t.Fatalf("NewSecrets failed: %v", err)
	}
	state, err := New(mygo.App, Deps{
		Panels:    panels,
		Secrets:   secrets,
		Clipboard: testClipboard(t, nil, nil),
		Version:   "0.1.0",
		Quit:      func() {},
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// The switch is in the general section, in the application's own words,
	// with the line that says what turning it off does.
	texts := settingsText(t, state)
	if !contains(texts, "本地 Codex 额度监控") {
		t.Errorf("the general section has no switch for the panel: %q", texts)
	}
	if !contains(texts, "读取本机 Codex 命令行的登录状态，在悬浮窗显示额度；关闭后不再读取。") {
		t.Errorf("the switch does not say what it does: %q", texts)
	}

	// On: the block is drawn and the switch is on.
	if texts := cardText(t, state); !contains(texts, codexdomain.Panel().Title) {
		t.Errorf("the block is missing while the switch is on: %q", texts)
	}

	// Off: it is drawn nowhere, and nothing reads the login it came from.
	state.setPanelOn(codexdomain.PanelID, false)
	if texts := cardText(t, state); contains(texts, codexdomain.Panel().Title) {
		t.Errorf("the block is still there after the switch was turned off: %q", texts)
	}
	reads := source.reads
	state.refreshAsync(true)
	time.Sleep(20 * time.Millisecond)
	if source.reads != reads {
		t.Errorf("the credential was read %d times after the panel was turned off", source.reads-reads)
	}
	if !contains(settingsText(t, state), "已关闭") {
		t.Errorf("the switch said nothing about what happened: %q", settingsText(t, state))
	}
	// The choice is in the file, so the next launch starts without the panel.
	if on, chosen := tools.settings.Panels[codexdomain.PanelID]; !chosen || on {
		t.Errorf("the stored choices are %v, want the panel off", tools.settings.Panels)
	}

	// Back on: it is drawn again.
	state.setPanelOn(codexdomain.PanelID, true)
	if texts := cardText(t, state); !contains(texts, codexdomain.Panel().Title) {
		t.Errorf("the block did not come back: %q", texts)
	}
}

// The quota block is a bar and the account it belongs to: what is left, when
// the window resets, and which plan it is.
func TestQuotaBlockDrawsTheBarAndTheAccount(t *testing.T) {
	state := quotaState(t, quotaAdapter{remaining: 68, resetsIn: 7200, plan: "plus"}, nil, nil)
	loadQuota(t, state)
	tt := ui.NewTester(state.View, panelWidth, panelHeight)

	if !tt.HasText(codexdomain.Panel().Title) {
		t.Errorf("the block does not name the panel: %q", tt.Texts())
	}
	// The window shows what is spent: 68% left is 32% used, written once on
	// the account's own line and once over the bar.
	if !tt.HasText("已用 32%") {
		t.Errorf("the block does not say how much of the window is gone: %q", tt.Texts())
	}
	if tt.HasText("剩余 68%") {
		t.Errorf("the block still counts what is left: %q", tt.Texts())
	}
	note := findTextPrefix(t, tt, "Plus · 下次重置 ")
	account, ok := textWithPrefix(tt.Texts(), "Plus · 下次重置 ")
	if !ok {
		t.Fatalf("the block does not say what the account is and when it resets: %q", tt.Texts())
	}
	want := quotaFetchedAt.Add(7200 * time.Second).Local().Format("01-02 15:04")
	if !strings.Contains(account, want) {
		t.Errorf("the reset time reads %q, want the moment the reading points at (%s)", account, want)
	}
	// The used share stands at the far end of that line, not beside its
	// words: the line is what the account is, and the number belongs to the
	// right edge of the block.
	used := findText(t, tt, "已用 32%")
	if used.X <= note.X+note.W-1 {
		t.Errorf("the used share at x=%.0f is not past the account's own words, which end at %.0f",
			used.X, note.X+note.W)
	}
	if used.Y < note.Y-1 || used.Y > note.Y+note.H {
		t.Errorf("the used share at y=%.0f is not on the account's line at y=%.0f h=%.0f",
			used.Y, note.Y, note.H)
	}
}

// The bar's progress is what is spent, not what is left: the fill takes that
// much of the track, which is what makes the block readable without the
// number. It is drawn in the block's own color, and the bar itself — its
// track, its height, its rounding — does not change with the meaning.
func TestQuotaBlockFillsTheBarByTheUsedShare(t *testing.T) {
	for _, remaining := range []int64{0, 25, 68, 100} {
		state := quotaState(t, quotaAdapter{remaining: remaining, resetsIn: 3600, plan: "pro"}, nil, nil)
		loadQuota(t, state)

		const scale = 2
		tt := ui.NewTester(state.View, panelWidth, panelHeight)
		img := ui.Render(state.View, panelWidth, panelHeight, scale)
		track := findText(t, tt, "已用额度")

		row := int(track.Y+track.H/2) * scale
		filled := 0
		for x := int(track.X)*scale + 1; x < int(track.X+track.W)*scale-1; x++ {
			if img.RGBAAt(x, row) == rgba(codexBlack) {
				filled++
			}
		}
		width := (int(track.W)*scale - 2)
		got := float64(filled) / float64(width) * 100
		want := float64(100 - remaining)
		if got < want-3 || got > want+3 {
			t.Errorf("%d%% left filled %.0f%% of the track, want the used share %.0f%%", remaining, got, want)
		}
		if !tt.HasText("已用 " + strconv.FormatInt(100-remaining, 10) + "%") {
			t.Errorf("%d%% left does not read as %d%% used: %q", remaining, 100-remaining, tt.Texts())
		}
	}
}

// A panel whose credential comes from a tool nobody has logged into says so,
// and offers the way to log in: it never draws an empty bar as "0% used".
func TestQuotaBlockWithoutALoginOffersTheLogin(t *testing.T) {
	source := &quotaSource{credentialsErr: fmt.Errorf("%w: the tool is not logged in", domain.ErrCredentialMissing)}
	state := quotaState(t, quotaAdapter{remaining: 68, resetsIn: 7200, plan: "plus"}, nil, source)
	if _, err := state.panelService.Refresh(context.Background(), codexdomain.PanelID); err == nil {
		t.Fatal("a panel without a credential reported an answer")
	}
	tt := ui.NewTester(state.View, panelWidth, panelHeight)

	if !tt.HasText("还没有登录") {
		t.Errorf("the block does not say why there is nothing to show: %q", tt.Texts())
	}
	if tt.HasText("已用 0%") || tt.HasText("已用 68%") {
		t.Errorf("the block drew a bar for an account it cannot read: %q", tt.Texts())
	}
	if err := tt.Click("登录 Codex"); err != nil {
		t.Fatalf("click 登录 Codex: %v", err)
	}
	tt.Frame()
	if source.started != 1 {
		t.Errorf("the login was started %d time(s), want 1", source.started)
	}
	if !tt.HasText("已在终端里打开登录") {
		t.Errorf("the block does not say the login was started: %q", tt.Texts())
	}
}

// A login that could not be started is reported where the button is, in words
// that name the service — a machine without the command is not a silent
// no-op.
func TestQuotaBlockReportsALoginThatCouldNotStart(t *testing.T) {
	source := &quotaSource{
		credentialsErr: fmt.Errorf("%w: the tool is not logged in", domain.ErrCredentialMissing),
		startErr:       domain.ErrLoginCommandMissing,
	}
	state := quotaState(t, quotaAdapter{remaining: 68, resetsIn: 7200, plan: "plus"}, nil, source)
	_, _ = state.panelService.Refresh(context.Background(), codexdomain.PanelID)
	tt := ui.NewTester(state.View, panelWidth, panelHeight)

	if err := tt.Click("登录 Codex"); err != nil {
		t.Fatalf("click 登录 Codex: %v", err)
	}
	tt.Frame()
	if !tt.HasText("没有找到 Codex 的命令行工具") {
		t.Errorf("a login that could not start says nothing: %q", tt.Texts())
	}
}

// A quota that failed to refresh says why, and shows no bar at all: a failure
// is not an empty quota.
func TestQuotaBlockFailureIsNotAnEmptyBar(t *testing.T) {
	state := quotaState(t, quotaAdapter{remaining: 68, resetsIn: 7200, plan: "plus"},
		&quotaProvider{err: domain.ErrProviderUnauthorized}, nil)
	if _, err := state.panelService.Refresh(context.Background(), codexdomain.PanelID); err == nil {
		t.Fatal("the failed provider reported an answer")
	}
	tt := ui.NewTester(state.View, panelWidth, panelHeight)

	if !tt.HasText("凭据被拒绝") {
		t.Errorf("the block does not say why it failed: %q", tt.Texts())
	}
	if tt.HasText("已用 0%") || tt.HasText("已用 —") {
		t.Errorf("a failed quota was drawn as a bar: %q", tt.Texts())
	}
}

// An answer that is already there stays on screen when the next attempt
// fails, marked as old: an old quota is worth more than none.
func TestQuotaBlockKeepsTheOldBarWhenARefreshFails(t *testing.T) {
	provider := &quotaProvider{}
	state := quotaState(t, quotaAdapter{remaining: 68, resetsIn: 7200, plan: "plus"}, provider, nil)
	loadQuota(t, state)

	provider.err = domain.ErrProviderTimeout
	if _, err := state.panelService.Refresh(context.Background(), codexdomain.PanelID); err == nil {
		t.Fatal("the failed provider reported an answer")
	}
	tt := ui.NewTester(state.View, panelWidth, panelHeight)

	if !tt.HasText("已用 32%") {
		t.Errorf("the old quota was dropped: %q", tt.Texts())
	}
	if !tt.HasText("旧数据") {
		t.Errorf("the old quota is not marked as old: %q", tt.Texts())
	}
	if !tt.HasText("请求超时") {
		t.Errorf("the block does not say why the refresh failed: %q", tt.Texts())
	}
}

// A block that is not a proportion keeps the figure layout: the shape follows
// the headline, not the provider.
func TestABalancePanelIsNotDrawnAsAQuota(t *testing.T) {
	// The fixture state is the deepseek one: a currency headline.
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	loaded(t, state)
	tt := ui.NewTester(state.View, panelWidth, panelHeight)

	if tt.HasText("已用额度") {
		t.Errorf("a balance was drawn as a quota: %q", tt.Texts())
	}
	if !tt.HasText("总余额") {
		t.Errorf("the balance panel lost its own layout: %q", tt.Texts())
	}
}

// twoProviders is a registry over the balance fake and the quota fake: a
// window that shows both blocks needs a registry that holds both.
type twoProviders struct {
	balance        domain.Provider
	balanceAdapter domain.Adapter
	quota          domain.Provider
	quotaAdapter   domain.Adapter
}

func (r twoProviders) Provider(id string) (domain.Provider, error) {
	switch id {
	case r.balance.ID():
		return r.balance, nil
	case r.quota.ID():
		return r.quota, nil
	}
	return nil, fmt.Errorf("%w: provider %s", domain.ErrProviderNotFound, id)
}

func (r twoProviders) Adapter(id string) (domain.Adapter, error) {
	switch id {
	case r.balance.ID():
		return r.balanceAdapter, nil
	case r.quota.ID():
		return r.quotaAdapter, nil
	}
	return nil, fmt.Errorf("%w: adapter %s", domain.ErrProviderNotFound, id)
}

// bothBlocksState is the quick window with the balance panel and the quota
// panel: the screen the progress bar was designed for, and what the manual
// render writes out.
func bothBlocksState(t *testing.T) *State {
	t.Helper()
	balance, quota := newTestProvider(), &quotaProvider{}
	secrets, err := credential.NewSecrets(withCredential(newTestStore()))
	if err != nil {
		t.Fatalf("NewSecrets failed: %v", err)
	}
	panels, err := panel.NewPanels(
		twoProviders{
			balance:        balance,
			balanceAdapter: testAdapter{provider: balance},
			quota:          quota,
			quotaAdapter:   quotaAdapter{remaining: 68, resetsIn: 7200, plan: "plus"},
		},
		withCredential(newTestStore()),
		nil,
		[]panel.PanelDef{
			{Panel: deepseek.Panel(), Metrics: deepseek.Metrics()},
			// The quota panel is the optional one: it reads the login the
			// Codex command line already made, so the settings window
			// offers a switch for it.
			{Panel: codexdomain.Panel(), Metrics: codexdomain.Metrics(), Optional: true},
		},
		&quotaSource{},
	)
	if err != nil {
		t.Fatalf("NewPanels failed: %v", err)
	}
	state, err := New(mygo.App, Deps{
		Panels:    panels,
		Secrets:   secrets,
		Clipboard: testClipboard(t, nil, nil),
		Version:   "0.1.0",
		Quit:      func() {},
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	for _, id := range []string{deepseek.PanelID, codexdomain.PanelID} {
		if _, err := panels.Refresh(context.Background(), id); err != nil {
			t.Fatalf("refresh %s: %v", id, err)
		}
	}
	return state
}

// The line between two blocks is the design's: one point of stone-100, inside
// the same 16-point edge the blocks keep — while the line above the foot runs
// the window's whole width, because that one says where the window ends.
func TestBlocksDividedByTheDesignsLine(t *testing.T) {
	state := bothBlocksState(t)
	const scale = 2
	img := ui.Render(state.View, panelWidth, panelHeight, scale)

	inset := int(quickEdge) * scale
	across := (panelWidth - int(quickEdge)*2) * scale

	divider, windowEdge := false, false
	for y := 0; y < img.Rect.Dy(); y++ {
		insideAll := true
		for x := inset; x < across; x++ {
			if img.RGBAAt(x, y) != rgba(panelLine) {
				insideAll = false
				break
			}
		}
		switch {
		case !insideAll:
			continue
		case img.RGBAAt(2, y) != rgba(panelLine):
			// Stopped short of the window's own edge: the line between two
			// blocks, which keeps the blocks' 16.
			divider = true
		default:
			windowEdge = true
		}
	}
	if !divider {
		t.Errorf("no line between two blocks keeps the design's %d-point edge", int(quickEdge))
	}
	if !windowEdge {
		t.Error("the line above the foot does not run the window's whole width")
	}
}

// A block's head is the design's: the provider's mark in a tile, a gap, then
// the name — and the mark is drawn, not only laid out.
func TestABlockCarriesItsMarkAndName(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	loaded(t, state)
	const scale = 2
	tt := ui.NewTester(state.View, panelWidth, panelHeight)
	img := ui.Render(state.View, panelWidth, panelHeight, scale)

	title := findText(t, tt, "DeepSeek 余额")
	want := float32(quickEdge + quickTileSize + 8)
	if title.X < want-2 || title.X > want+2 {
		t.Errorf("the name starts at x=%.0f, want the room the design gives it (%.0f)", title.X, want)
	}
	// The DeepSeek block's tile is the provider's own blue with the whale
	// drawn on it: solid enough that a few hundred pixels of it are on
	// screen.
	if got := countColor(img, deepseekBlue); got < 400 {
		t.Errorf("the block's mark covers %d pixels: no tile is drawn at its head", got)
	}
}

func TestPlanLabel(t *testing.T) {
	cases := map[string]string{
		"plus":        "Plus",
		"PRO":         "Pro",
		"business":    "Business",
		"enterprise":  "Enterprise",
		"very_custom": "very_custom",
		"":            "",
		"  team  ":    "Team",
	}
	for plan, want := range cases {
		if got := planLabel(plan); got != want {
			t.Errorf("planLabel(%q) = %q, want %q", plan, got, want)
		}
	}
}

func TestResetClock(t *testing.T) {
	observed := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	if got := resetClock(observed, 7200); got != observed.Add(2*time.Hour).Local().Format("01-02 15:04") {
		t.Errorf("resetClock = %q", got)
	}
	// A window that already passed is not a time in the past and not a
	// negative duration: it reads as the moment that was.
	if got := resetClock(observed, 0); got != "—" {
		t.Errorf("resetClock(0) = %q, want a dash", got)
	}
	if got := resetClock(time.Time{}, 60); got != "—" {
		t.Errorf("resetClock without a moment = %q, want a dash", got)
	}
}
