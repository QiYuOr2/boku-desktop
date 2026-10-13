package app

import (
	"context"
	"image"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
	"boku/internal/domain/deepseek"
)

// cardText is the texts of one frame of the quick window.
func cardText(t *testing.T, state *State) []string {
	t.Helper()
	tt := ui.NewTester(state.View, panelWidth, panelHeight)
	return tt.Texts()
}

// findText returns where one text of the frame sits. The tester matches a
// text in full, so a label and the value beside it can both be found.
func findText(t *testing.T, tt *ui.Tester, want string) ui.Rect {
	t.Helper()
	rect, ok := tt.Find(want)
	if !ok {
		t.Fatalf("no text is %q; the frame shows %q", want, tt.Texts())
	}
	return rect
}

// findTextPrefix returns where the text starting with prefix sits. It is for
// the lines whose value the test does not decide, such as the clock.
func findTextPrefix(t *testing.T, tt *ui.Tester, prefix string) ui.Rect {
	t.Helper()
	for _, text := range tt.Texts() {
		if strings.HasPrefix(text, prefix) {
			return findText(t, tt, text)
		}
	}
	t.Fatalf("no text starts with %q; the frame shows %q", prefix, tt.Texts())
	return ui.Rect{}
}

func contains(texts []string, want string) bool {
	for _, text := range texts {
		if strings.Contains(text, want) {
			return true
		}
	}
	return false
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

	for _, want := range []string{"DeepSeek 余额", "总余额", "¥42.10", "明细", "已更新"} {
		if !contains(texts, want) {
			t.Errorf("the card does not show %q; it shows %q", want, texts)
		}
	}
	if contains(texts, "旧数据") {
		t.Errorf("a fresh card is marked as old data: %q", texts)
	}
}

// The panel's first line is its name, and beside it how old the numbers are
// with the action that fetches them again — one line, one request.
func TestCardTitleCarriesTheTimeAndTheAction(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	loaded(t, state)
	tt := ui.NewTester(state.View, panelWidth, panelHeight)

	title := findText(t, tt, "DeepSeek 余额")
	updated := findTextPrefix(t, tt, "已更新")
	refresh := findText(t, tt, "刷新")

	if updated.Y < title.Y-1 || updated.Y > title.Y+title.H {
		t.Errorf("the updated line at y=%.0f is not on the title's line at y=%.0f h=%.0f",
			updated.Y, title.Y, title.H)
	}
	if refresh.Y < title.Y-1 || refresh.Y > title.Y+title.H {
		t.Errorf("the refresh action at y=%.0f is not on the title's line at y=%.0f h=%.0f",
			refresh.Y, title.Y, title.H)
	}
	if updated.X <= title.X+title.W {
		t.Errorf("the updated line at x=%.0f is not right of the title, which ends at x=%.0f",
			updated.X, title.X+title.W)
	}
	if refresh.X <= updated.X {
		t.Errorf("the action at x=%.0f is not right of the time at x=%.0f", refresh.X, updated.X)
	}
	// Nothing else claims the bottom of the window: the panel's own line is
	// gone, and the footer is all that is left below it.
	if version := findText(t, tt, "Boku 0.1.0"); version.Y <= updated.Y {
		t.Errorf("the footer at y=%.0f is not below the title line at y=%.0f", version.Y, updated.Y)
	}
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

// dividerRows returns the y of every row of the frame that is drawn edge to
// edge in one color, which is how a divider looks in a rendered frame.
func dividerRows(img *image.RGBA, c ui.Color, width int) []int {
	var rows []int
	for y := 0; y < img.Rect.Dy(); y++ {
		row := 0
		for x := 0; x < width; x++ {
			i := img.PixOffset(x, y)
			if img.Pix[i] == c.R && img.Pix[i+1] == c.G && img.Pix[i+2] == c.B {
				row++
			}
		}
		if row == width {
			rows = append(rows, y)
		}
	}
	return rows
}

// A window marks its sections with dividers, not with boxes: the panel and
// the footer each end at one.
func TestWindowSectionsAreMarkedByDividers(t *testing.T) {
	state := testState(t, newTestProvider(), withCredential(newTestStore()), "0.1.0")
	loaded(t, state)
	const scale = 2
	img := ui.Render(state.View, panelWidth, panelHeight, scale)

	tt := ui.NewTester(state.View, panelWidth, panelHeight)
	collapsed := findText(t, tt, "明细").Y
	version := findText(t, tt, "Boku 0.1.0").Y

	rows := dividerRows(img, ui.LightTheme().Border, panelWidth*scale)
	if len(rows) == 0 {
		t.Fatal("the window draws no divider at all: the panel is not separated from the footer")
	}
	between := false
	for _, y := range rows {
		if at := float32(y) / scale; at > collapsed && at < version {
			between = true
		}
	}
	if !between {
		t.Errorf("no divider falls between the panel at y=%.0f and the footer at y=%.0f: %v",
			collapsed, version, rows)
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
	texts := cardText(t, state)
	if !contains(texts, "服务暂时不可用") {
		t.Errorf("the card does not explain the failure: %q", texts)
	}
	if !contains(texts, "重试") {
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
	texts := cardText(t, state)
	if !contains(texts, "¥42.10") {
		t.Errorf("the card dropped the last good balance: %q", texts)
	}
	if !contains(texts, "旧数据") {
		t.Errorf("the card does not mark the numbers as old: %q", texts)
	}
	if !contains(texts, "重试") {
		t.Errorf("the card offers no retry: %q", texts)
	}
	// One line, one action: retrying and refreshing fetch the same thing.
	if contains(texts, "刷新") {
		t.Errorf("the failing card offers both 重试 and 刷新: %q", texts)
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
