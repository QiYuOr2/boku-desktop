package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo"

	"boku/internal/settings"
)

// 外观和任何功能的设置一样：有默认值、只读自己的键、并以同名写回选择。
// 这些是 settings.json 的规则，在写该 section 的地方校验。
func TestAppearanceDefaultsToFollowingTheDesktop(t *testing.T) {
	appearance := NewAppearanceSection()
	if got := appearance.Name(); got != "appearance" {
		t.Errorf("Name = %q, want appearance", got)
	}
	if got := appearance.Mode(); got != AppearanceSystem {
		t.Errorf("Mode = %q, want %q", got, AppearanceSystem)
	}
	values, err := appearance.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if got := values[appearanceThemeKey]; got != "system" {
		t.Errorf("%s = %v, want system", appearanceThemeKey, got)
	}
}

func TestAppearanceReadsEachMode(t *testing.T) {
	for _, mode := range []AppearanceMode{AppearanceLight, AppearanceDark, AppearanceSystem} {
		t.Run(string(mode), func(t *testing.T) {
			appearance := NewAppearanceSection()
			values := map[string]json.RawMessage{
				appearanceThemeKey: json.RawMessage(`"` + string(mode) + `"`),
			}
			if err := appearance.Decode(values); err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got := appearance.Mode(); got != mode {
				t.Errorf("Mode = %q, want %q", got, mode)
			}
		})
	}
}

// 文件中没有该键意味着用户从未选择：保留默认值，对所有功能的设置都如此。
func TestAppearanceAbsentKeyKeepsTheChoice(t *testing.T) {
	appearance := NewAppearanceSection()
	appearance.Set(AppearanceDark)
	if err := appearance.Decode(map[string]json.RawMessage{}); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got := appearance.Mode(); got != AppearanceDark {
		t.Errorf("Mode = %q, want the previous dark", got)
	}
}

// 本版本不认识的值跟随桌面，而非拒绝启动：更新版本写的设置文件仍可读取。
func TestAppearanceUnknownModeFollowsTheDesktop(t *testing.T) {
	appearance := NewAppearanceSection()
	appearance.Set(AppearanceDark)
	values := map[string]json.RawMessage{appearanceThemeKey: json.RawMessage(`"sepia"`)}
	if err := appearance.Decode(values); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got := appearance.Mode(); got != AppearanceSystem {
		t.Errorf("Mode = %q, want %q", got, AppearanceSystem)
	}
}

// JSON 类型错误的值是损坏的文件，存储会报告它，而不是静默改变用户的选择。
func TestAppearanceRejectsAValueThatIsNotAString(t *testing.T) {
	appearance := NewAppearanceSection()
	values := map[string]json.RawMessage{appearanceThemeKey: json.RawMessage(`1`)}
	if err := appearance.Decode(values); err == nil {
		t.Fatal("Decode accepted a number as a theme")
	}
}

func TestAppearanceDarkFollowsTheChoice(t *testing.T) {
	tests := []struct {
		mode       AppearanceMode
		systemDark bool
		want       bool
	}{
		{AppearanceLight, false, false},
		{AppearanceLight, true, false},
		{AppearanceDark, false, true},
		{AppearanceDark, true, true},
		{AppearanceSystem, false, false},
		{AppearanceSystem, true, true},
		{"", true, true},
	}
	for _, test := range tests {
		appearance := NewAppearanceSection()
		appearance.Set(test.mode)
		if got := appearance.Dark(test.systemDark); got != test.want {
			t.Errorf("Dark(%q, system=%v) = %v, want %v", test.mode, test.systemDark, got, test.want)
		}
	}
}

func TestThemeSourceMatchesTheMode(t *testing.T) {
	tests := []struct {
		mode AppearanceMode
		want mygo.ThemeSource
	}{
		{AppearanceLight, mygo.ThemeLight},
		{AppearanceDark, mygo.ThemeDark},
		{AppearanceSystem, mygo.ThemeSystem},
		{"unknown", mygo.ThemeSystem},
	}
	for _, test := range tests {
		if got := ThemeSource(test.mode); got != test.want {
			t.Errorf("ThemeSource(%q) = %q, want %q", test.mode, got, test.want)
		}
	}
}

// appearanceStore 是记录被要求做什么的设置存储。
type appearanceStore struct {
	calls   int
	section string
	err     error
	// changeRan 表示存储是否到达了变更：读取失败时不会，而窗口仍需对此作答。
	changeRan bool
}

func (s *appearanceStore) Update(_ context.Context, section string, change func() error) error {
	s.calls++
	s.section = section
	if s.err != nil {
		return s.err
	}
	s.changeRan = true
	return change()
}

// 主题点击经存储写入、立即应用并重绘：文件与帧是同一个答案。
func TestSetAppearanceWritesAppliesAndRepaints(t *testing.T) {
	store := &appearanceStore{}
	var applied []AppearanceMode
	state := &State{
		appearance:      NewAppearanceSection(),
		appearanceStore: store,
		applyAppearance: func(mode AppearanceMode) { applied = append(applied, mode) },
	}

	state.setAppearance(AppearanceDark)

	if store.calls != 1 || store.section != "appearance" {
		t.Errorf("the change went through %d store calls for %q, want one for appearance", store.calls, store.section)
	}
	if !store.changeRan {
		t.Error("the store did not run the change")
	}
	if got := state.appearance.Mode(); got != AppearanceDark {
		t.Errorf("stored mode = %q, want dark", got)
	}
	if !state.dark {
		t.Error("the state is still light after choosing dark")
	}
	if len(applied) != 1 || applied[0] != AppearanceDark {
		t.Errorf("applyAppearance got %v, want [dark]", applied)
	}
	if state.appearanceStatusLine() != "" {
		t.Errorf("a successful save said %q", state.appearanceStatusLine())
	}
}

// 无法写入的文件仍会改变本次运行的外观：点击是用户的选择。窗口会说明该选择未被存储，
// 因为下次启动不会记得它。
func TestSetAppearanceSurvivesAFailedWrite(t *testing.T) {
	store := &appearanceStore{err: errors.New("disk is full")}
	var applied []AppearanceMode
	state := &State{
		appearance:      NewAppearanceSection(),
		appearanceStore: store,
		applyAppearance: func(mode AppearanceMode) { applied = append(applied, mode) },
	}

	state.setAppearance(AppearanceLight)

	if got := state.appearance.Mode(); got != AppearanceLight {
		t.Errorf("stored mode = %q, want light in this run", got)
	}
	if state.dark {
		t.Error("the state is dark after choosing light")
	}
	if len(applied) != 1 || applied[0] != AppearanceLight {
		t.Errorf("applyAppearance got %v, want [light]", applied)
	}
	if !strings.Contains(state.appearanceStatusLine(), "本次运行内已生效") {
		t.Errorf("the failure said %q, want it to say this run still uses the choice", state.appearanceStatusLine())
	}
}

// 真实存储是这里唯一会以假实现无法展示的方式出错的地方：Update 在执行变更前先读文件，
// 因此只在调用前设好值的 section 会被磁盘上已有的值覆盖并写回。选择必须在这次读取后存活。
func TestSetAppearanceSurvivesTheStoresRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	appearance := NewAppearanceSection()
	store, err := settings.New(path, appearance)
	if err != nil {
		t.Fatalf("settings.New: %v", err)
	}
	// 给文件一个可被覆盖的值：默认值已在内存中，因此保存一次。
	if err := store.Save(context.Background()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	state := &State{appearance: appearance, appearanceStore: store}
	state.setAppearance(AppearanceDark)
	if state.appearanceStatusLine() != "" {
		t.Fatalf("the save failed: %s", state.appearanceStatusLine())
	}

	reloaded := NewAppearanceSection()
	reopened, err := settings.New(path, reloaded)
	if err != nil {
		t.Fatalf("settings.New: %v", err)
	}
	if err := reopened.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := reloaded.Mode(); got != AppearanceDark {
		t.Errorf("the file has %q after choosing dark", got)
	}
}

// 固定外观不能保留系统 popover：其材质跟随桌面，而非 Boku 强制的外观，
// 页面与背景会不一致。只有自动模式（页面与 popover 都跟随桌面）才用原生 popover。
func TestTheQuickWindowIsAPopoverOnlyInTheAutomaticAppearance(t *testing.T) {
	state := &State{appearance: NewAppearanceSection()}
	if !state.quickWindowUsesPopover() {
		t.Error("the automatic appearance does not use the system popover")
	}
	state.appearance.Set(AppearanceLight)
	if state.quickWindowUsesPopover() {
		t.Error("a fixed light appearance still asks for the system popover")
	}
	state.appearance.Set(AppearanceDark)
	if state.quickWindowUsesPopover() {
		t.Error("a fixed dark appearance still asks for the system popover")
	}
}

// 固定外观绘制的卡片在托盘图标处居中，也就是 popover 箭头所在处。其它平台保持末端对齐。
func TestTheFixedAppearanceCardCentresOnTheTrayIcon(t *testing.T) {
	tests := []struct {
		goos    string
		popover bool
		want    mygo.Placement
	}{
		{"darwin", true, mygo.PlacementTopEnd},
		{"darwin", false, mygo.PlacementTop},
		{"windows", true, mygo.PlacementTopEnd},
		{"windows", false, mygo.PlacementTopEnd},
		{"linux", false, mygo.PlacementTopEnd},
	}
	for _, test := range tests {
		if got := quickWindowPlacement(test.goos, test.popover); got != test.want {
			t.Errorf("quickWindowPlacement(%q, popover=%v) = %q, want %q",
				test.goos, test.popover, got, test.want)
		}
	}
}

// popoverAppearance 记录 State 会对平台发起的调用。
type popoverAppearance struct {
	dark  bool
	calls int
}

func (p *popoverAppearance) SetDark(uintptr) bool {
	p.calls++
	return p.dark
}

// 强制深色仅在注入了平台桥接时才用原生 popover；自动外观总是用，强制浅色从不用（其材质可能跟随深色桌面）。
func TestThePopoverBridgeIsOnlyNeededForForcedDark(t *testing.T) {
	bridge := &popoverAppearance{dark: true}
	state := &State{appearance: NewAppearanceSection(), popoverAppearance: bridge}

	state.appearance.Set(AppearanceSystem)
	if !state.quickWindowUsesPopover() {
		t.Error("the automatic appearance stopped using the system popover")
	}
	state.appearance.Set(AppearanceLight)
	if state.quickWindowUsesPopover() {
		t.Error("a forced light appearance used the system popover")
	}
	state.appearance.Set(AppearanceDark)
	if !state.quickWindowUsesPopover() {
		t.Error("a forced dark appearance with a bridge did not use the system popover")
	}

	// 没有桥接时，强制深色使用应用卡片。
	state.popoverAppearance = nil
	if state.quickWindowUsesPopover() {
		t.Error("forced dark used a popover without a bridge to make it dark")
	}
}
