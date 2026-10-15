package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	"boku/internal/panel/deepseek"
)

// cardText is the texts of one frame of the quick window.
func cardText(t *testing.T, state *State) []string {
	t.Helper()
	tt := ui.NewTester(state.View, panelWidth, panelHeight)
	return tt.Texts()
}

// A failure must never be drawn as 0: the card says what happened instead.
func assertNoZero(t *testing.T, texts []string) {
	t.Helper()
	for _, text := range texts {
		if strings.Contains(text, "0.00") || strings.Contains(text, "¥0") {
			t.Errorf("a failed card shows a zero: %q", text)
		}
	}
}

// At a glance the card shows the headline and the age of the numbers; the
// detail rows are folded away until they are asked for.
func TestCardShowsTheBalance(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	loaded(t, state)
	texts := cardText(t, state)

	for _, want := range []string{"DeepSeek 余额", "总余额", "¥42.10", "明细", fetchedClock(t, state)} {
		if !contains(texts, want) {
			t.Errorf("the card does not show %q; it shows %q", want, texts)
		}
	}
	if contains(texts, "旧数据") {
		t.Errorf("a fresh card is marked as old data: %q", texts)
	}
}

// The panel's first line is its name, and beside it the age of the numbers
// with the action that fetches them again — one line, one request. The design
// writes that age as the clock alone, and the action as a mark rather than a
// word, so it is found by its label.
func TestCardTitleCarriesTheTimeAndTheAction(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	loaded(t, state)
	tt := ui.NewTester(state.View, panelWidth, panelHeight)

	title := findText(t, tt, "DeepSeek 余额")
	updated := findText(t, tt, fetchedClock(t, state))
	refresh := findText(t, tt, "刷新")

	if updated.Y < title.Y-1 || updated.Y > title.Y+title.H {
		t.Errorf("the updated line at y=%.0f is not on the title's line at y=%.0f h=%.0f",
			updated.Y, title.Y, title.H)
	}
	// The control is a mark with a little room around it, so what sits on
	// the title's line is its middle rather than its top edge.
	if center := refresh.Y + refresh.H/2; center < title.Y || center > title.Y+title.H {
		t.Errorf("the refresh action centered at y=%.0f is not on the title's line at y=%.0f h=%.0f",
			center, title.Y, title.H)
	}
	if updated.X <= title.X+title.W {
		t.Errorf("the updated line at x=%.0f is not right of the title, which ends at x=%.0f",
			updated.X, title.X+title.W)
	}
	if refresh.X <= updated.X {
		t.Errorf("the action at x=%.0f is not right of the time at x=%.0f", refresh.X, updated.X)
	}
	// The action is a mark in the room a word would have taken: it is found
	// by its label, and it is no bigger than a small icon needs.
	if refresh.W > 24 || refresh.H > 24 {
		t.Errorf("the refresh action is %.0fx%.0f: an icon takes a mark's room, not a word's", refresh.W, refresh.H)
	}
	// Nothing else claims the bottom of the window: the panel's own line is
	// gone, and the footer is all that is left below it.
	if version := findText(t, tt, "Boku 0.1.0"); version.Y <= updated.Y {
		t.Errorf("the footer at y=%.0f is not below the title line at y=%.0f", version.Y, updated.Y)
	}
}

// The refresh action is drawn as a mark, and it turns for as long as a fetch
// runs: a still mark beside unchanged numbers would leave the user wondering
// whether the click landed. Once the answer is in, it stops.
func TestCardRefreshMarkTurnsWhileFetching(t *testing.T) {
	provider := newTestProvider()
	state := testState(t, provider, withCredential(newTestStore()), "0.1.0")
	loaded(t, state)

	// A settled card draws the same frame twice: nothing on it animates.
	settled := ui.NewTester(state.View, panelWidth, panelHeight)
	before := frameBytes(settled.Image())
	time.Sleep(40 * time.Millisecond)
	settled.Frame()
	if after := frameBytes(settled.Image()); !bytes.Equal(before, after) {
		t.Error("the card animates while no fetch is running")
	}

	// Hold the next fetch open and draw the card again: the mark turns, so
	// two frames taken apart differ.
	gate := provider.hold()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = state.panelService.Refresh(context.Background(), deepseek.PanelID)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for provider.count() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !state.panelService.View(deepseek.PanelID).Loading {
		close(gate)
		<-done
		t.Fatal("the provider was not held in a fetch")
	}
	turning := ui.NewTester(state.View, panelWidth, panelHeight)
	first := frameBytes(turning.Image())
	time.Sleep(120 * time.Millisecond)
	turning.Frame()
	second := frameBytes(turning.Image())
	close(gate)
	<-done
	if bytes.Equal(first, second) {
		t.Error("the refresh mark does not turn while a fetch is running")
	}
}

// frameBytes copies what a frame was drawn as, so two of them can be
// compared.
func frameBytes(img *image.RGBA) []byte {
	return append([]byte(nil), img.Pix...)
}

// The detail rows stay folded away until they are asked for: the panel says
// what it is about at a glance, and the rest is one click deeper.
func TestCardDetailsAreCollapsedUntilClicked(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	loaded(t, state)
	tt := ui.NewTester(state.View, panelWidth, panelHeight)

	if !tt.HasText("明细") {
		t.Fatalf("the card offers no detail row; it shows %q", tt.Texts())
	}
	for _, hidden := range []string{"赠送余额", "充值余额", "¥10.00", "¥32.10"} {
		if tt.HasText(hidden) {
			t.Errorf("the collapsed card already shows %q: %q", hidden, tt.Texts())
		}
	}

	if err := tt.Click("明细"); err != nil {
		t.Fatalf("click 明细: %v", err)
	}
	tt.Frame()
	if !state.detailsOpen {
		t.Fatal("clicking 明细 did not open the details")
	}
	granted := findText(t, tt, "赠送余额")
	toppedUp := findText(t, tt, "充值余额")
	total := findText(t, tt, "¥42.10")
	if !tt.HasText("¥10.00") || !tt.HasText("¥32.10") {
		t.Errorf("the open details do not show the numbers: %q", tt.Texts())
	}
	// The rows line up with the headline they belong to, and are one line
	// apart: they are a group, not two more sections.
	if diff := granted.X - total.X; diff > 1 || diff < -1 {
		t.Errorf("the detail rows at x=%.0f do not line up with the headline at x=%.0f", granted.X, total.X)
	}
	if granted.Y < total.Y {
		t.Errorf("the detail rows at y=%.0f are above the headline at y=%.0f", granted.Y, total.Y)
	}
	if step := toppedUp.Y - granted.Y; step > 20 {
		t.Errorf("赠送余额 and 充值余额 are %.0f apart: they should be close together", step)
	}

	if err := tt.Click("明细"); err != nil {
		t.Fatalf("click 明细 again: %v", err)
	}
	tt.Frame()
	if state.detailsOpen {
		t.Error("clicking 明细 again did not close the details")
	}
	if tt.HasText("赠送余额") {
		t.Errorf("the collapsed card still shows the detail rows: %q", tt.Texts())
	}
}

// Without credentials the card asks for them, and shows no numbers at all.
func TestCardAsksForCredentials(t *testing.T) {
	state := testState(t, newTestProvider(), newTestStore(), "0.1.0")
	if _, err := state.panelService.Refresh(context.Background(), deepseek.PanelID); err == nil {
		t.Fatal("the refresh succeeded without a credential")
	}
	texts := cardText(t, state)
	if !contains(texts, "还没有配置凭据") {
		t.Errorf("the card does not say a credential is missing: %q", texts)
	}
	if !contains(texts, "去配置") {
		t.Errorf("the card offers no way to configure: %q", texts)
	}
	assertNoZero(t, texts)
}

func TestCardShowsWhyAFailureHappened(t *testing.T) {
	provider := newTestProvider()
	provider.fail(domain.ErrProviderUnavailable)
	state := testState(t, provider, withCredential(newTestStore()), "0.1.0")
	if _, err := state.panelService.Refresh(context.Background(), deepseek.PanelID); err == nil {
		t.Fatal("the refresh succeeded although the provider failed")
	}
	tt := ui.NewTester(state.View, panelWidth, panelHeight)
	texts := tt.Texts()
	if !contains(texts, "服务暂时不可用") {
		t.Errorf("the card does not explain the failure: %q", texts)
	}
	if _, ok := tt.Find("重试"); !ok {
		t.Errorf("the card offers no retry: %q", texts)
	}
	assertNoZero(t, texts)
}

// An old answer beats no answer, as long as it is labelled as old.
func TestCardKeepsTheOldNumbersWhenARefreshFails(t *testing.T) {
	provider := newTestProvider()
	state := testState(t, provider, withCredential(newTestStore()), "0.1.0")
	loaded(t, state)

	provider.fail(domain.ErrProviderTimeout)
	if _, err := state.panelService.Refresh(context.Background(), deepseek.PanelID); err == nil {
		t.Fatal("the refresh succeeded although the provider timed out")
	}
	tt := ui.NewTester(state.View, panelWidth, panelHeight)
	texts := tt.Texts()
	if !contains(texts, "¥42.10") {
		t.Errorf("the card dropped the last good balance: %q", texts)
	}
	if !contains(texts, "旧数据") {
		t.Errorf("the card does not mark the numbers as old: %q", texts)
	}
	// One line, one action: retrying and refreshing fetch the same thing.
	if _, ok := tt.Find("重试"); !ok {
		t.Errorf("the card offers no retry: %q", texts)
	}
	if _, ok := tt.Find("刷新"); ok {
		t.Errorf("the failing card offers both 重试 and 刷新: %q", texts)
	}
}

// Clicking the mark asks the provider again, whatever the cache says: it is
// the one action of the block.
func TestCardRefreshMarkFetches(t *testing.T) {
	provider := newTestProvider()
	state := testState(t, provider, withCredential(newTestStore()), "0.1.0")
	loaded(t, state)
	if got := provider.count(); got != 1 {
		t.Fatalf("the card was loaded with %d fetches, want 1", got)
	}

	tt := ui.NewTester(state.View, panelWidth, panelHeight)
	if err := tt.Click("刷新"); err != nil {
		t.Fatalf("click the refresh mark: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for provider.count() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := provider.count(); got != 2 {
		t.Errorf("the provider was fetched %d times after one click, want 2", got)
	}
}

// The headline is the way to the provider's own page for the account: a
// balance is worth seeing in full where it is kept, and the number is what
// the user is looking at when that thought arrives.
func TestCardBalanceOpensTheProvidersDetailPage(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	loaded(t, state)
	tt := ui.NewTester(state.View, panelWidth, panelHeight)

	if !tt.HasText("¥42.10") {
		t.Fatalf("the card does not show the balance: %q", tt.Texts())
	}
	if err := tt.Click("¥42.10"); err != nil {
		t.Fatalf("click the balance: %v", err)
	}
	opened := tt.OpenedURLs()
	if len(opened) != 1 || opened[0] != deepseek.UsageURL {
		t.Errorf("clicking the balance opened %v, want %q", opened, deepseek.UsageURL)
	}
	// It is a link and not a hidden one: it says so under the pointer, and
	// assistive technology reads it as an action.
	if _, ok := tt.Find("查看总余额明细"); !ok {
		t.Errorf("the balance is not named as an action: %q", tt.Texts())
	}
}

// A provider that keeps no such page leaves a number that is only a number:
// nothing is opened, and nothing pretends there is somewhere to go.
func TestCardBalanceWithoutADetailPageIsJustANumber(t *testing.T) {
	provider := newTestProvider()
	provider.descriptor.UsageURL = ""
	state := testState(t, provider, withCredential(newTestStore()), "0.1.0")
	loaded(t, state)
	tt := ui.NewTester(state.View, panelWidth, panelHeight)

	if _, ok := tt.Find("查看总余额明细"); ok {
		t.Errorf("a provider without a detail page still offers one: %q", tt.Texts())
	}
	if err := tt.Click("¥42.10"); err != nil {
		t.Fatalf("click the balance: %v", err)
	}
	if opened := tt.OpenedURLs(); len(opened) != 0 {
		t.Errorf("clicking a plain number opened %v", opened)
	}
}

// While the first fetch runs, the card shows that it is working and no
// numbers: there is nothing to show yet.
func TestCardShowsProgressWithoutNumbers(t *testing.T) {
	provider := newTestProvider()
	gate := provider.hold()
	state := testState(t, provider, withCredential(newTestStore()), "0.1.0")

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = state.panelService.Refresh(context.Background(), deepseek.PanelID)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for provider.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	texts := cardText(t, state)
	if !contains(texts, "正在获取") {
		t.Errorf("the card does not say it is fetching: %q", texts)
	}
	assertNoZero(t, texts)

	close(gate)
	<-done
}

// An account the service calls unavailable is an answer: the numbers are
// real zeros and the card repeats the explanation.
func TestCardShowsTheProvidersMessage(t *testing.T) {
	provider := newTestProvider()
	provider.setStatus(domain.StatusPartial, "账户当前不可用，余额仅供参考。")
	provider.mu.Lock()
	provider.values = map[string]int64{
		deepseek.MetricBalanceMinor:  0,
		deepseek.MetricGrantedMinor:  0,
		deepseek.MetricToppedUpMinor: 0,
	}
	provider.mu.Unlock()
	state := testState(t, provider, withCredential(newTestStore()), "0.1.0")
	loaded(t, state)

	texts := cardText(t, state)
	if !contains(texts, "¥0.00") {
		t.Errorf("the card does not show the real zero balance: %q", texts)
	}
	if !contains(texts, "账户当前不可用") {
		t.Errorf("the card does not repeat the provider's message: %q", texts)
	}
}

func TestCardWithoutPanels(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	// A service with no definitions at all: the dashboard has nothing to
	// show yet.
	state.panelService = emptyPanelService(t)
	texts := cardText(t, state)
	if !contains(texts, "还没有面板") {
		t.Errorf("an empty dashboard does not say so: %q", texts)
	}
}

func TestFriendlyErrorSpeaksTheUsersLanguage(t *testing.T) {
	cases := []struct {
		err      error
		contains string
	}{
		{fmt.Errorf("%w: deepseek.apiKey", domain.ErrCredentialMissing), "凭据"},
		{fmt.Errorf("%w: 401", domain.ErrProviderUnauthorized), "被拒绝"},
		{fmt.Errorf("%w: 429", domain.ErrProviderRateLimited), "频繁"},
		{fmt.Errorf("%w: timeout", domain.ErrProviderTimeout), "超时"},
		{fmt.Errorf("%w: 500", domain.ErrProviderUnavailable), "不可用"},
		{fmt.Errorf("%w: deepseek", domain.ErrProviderNotFound), "找不到"},
		{&domain.BadResponseError{ProviderID: "deepseek", Reason: "balance_infos is empty"}, "balance_infos is empty"},
	}
	for _, c := range cases {
		message := friendlyError(c.err)
		if !strings.Contains(message, c.contains) {
			t.Errorf("friendlyError(%v) = %q, want it to mention %q", c.err, message, c.contains)
		}
	}
	if friendlyError(nil) != "" {
		t.Error("friendlyError(nil) is not empty")
	}
	if err := errors.New("something else"); friendlyError(err) != err.Error() {
		t.Errorf("friendlyError(%v) = %q", err, friendlyError(err))
	}
}

func TestFormatMetricNeverInventsAZero(t *testing.T) {
	metrics := deepseek.Metrics()
	empty := domain.Snapshot{ProviderID: "deepseek", Status: domain.StatusOK}
	if got := formatMetric(empty, metrics[0]); got != "—" {
		t.Errorf("formatMetric of an empty snapshot = %q, want a dash", got)
	}
	snapshot := domain.Snapshot{
		ProviderID: "deepseek",
		Status:     domain.StatusOK,
		Currency:   "CNY",
		Observations: []domain.Observation{
			{MetricKey: deepseek.MetricBalanceMinor, Value: domain.NumberValue(4210)},
			{MetricKey: deepseek.MetricGrantedMinor, Value: domain.TextValue("plenty")},
		},
	}
	if got := formatMetric(snapshot, metrics[0]); got != "¥42.10" {
		t.Errorf("formatMetric(balance) = %q, want ¥42.10", got)
	}
	if got := formatMetric(snapshot, metrics[1]); got != "—" {
		t.Errorf("formatMetric of a text where a number belongs = %q, want a dash", got)
	}
	noCurrency := snapshot
	noCurrency.Currency = ""
	if got := formatMetric(noCurrency, metrics[0]); got != "—" {
		t.Errorf("formatMetric without a currency = %q, want a dash", got)
	}
}

func TestClock(t *testing.T) {
	if got := clock(time.Time{}); got != "—" {
		t.Errorf("clock of the zero time = %q", got)
	}
	at := time.Date(2026, 1, 2, 15, 4, 5, 0, time.Local)
	if got := clock(at); got != "15:04" {
		t.Errorf("clock = %q, want 15:04", got)
	}
}
