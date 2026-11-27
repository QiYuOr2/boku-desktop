package translate

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// 翻译默认值即全新安装的状态：工具开启、内置翻译器、两个日语注释开启、不固定、
// 失去焦点后很快关闭。
func TestTheDefaultsAreWhatAFreshInstallationHas(t *testing.T) {
	got := Defaults()
	if !got.Enabled {
		t.Error("translation starts off")
	}
	if got.ProviderOr() != DefaultProvider {
		t.Errorf("provider = %q, want %q", got.ProviderOr(), DefaultProvider)
	}
	if got.AcceleratorOr() != DefaultAccelerator {
		t.Errorf("shortcut = %q, want %q", got.AcceleratorOr(), DefaultAccelerator)
	}
	if got.ScreenshotAcceleratorOr() != DefaultScreenshotAccelerator {
		t.Errorf("screenshot shortcut = %q, want %q", got.ScreenshotAcceleratorOr(), DefaultScreenshotAccelerator)
	}
	if !got.Furigana || !got.LoanwordGloss {
		t.Error("the Japanese annotations do not start on")
	}
	if got.Pinned {
		t.Error("the window starts pinned")
	}
	if got.CloseDelayOr() != DefaultCloseDelay {
		t.Errorf("close delay = %s, want %s", got.CloseDelayOr(), DefaultCloseDelay)
	}
}

func TestScreenshotShortcutMayBeClearedAndPersists(t *testing.T) {
	section := NewSection()
	if err := section.Decode(map[string]json.RawMessage{"translateScreenshotAccelerator": json.RawMessage(`""`)}); err != nil {
		t.Fatal(err)
	}
	if got := section.Current().ScreenshotAcceleratorOr(); got != "" {
		t.Fatalf("shortcut = %q, want cleared", got)
	}
	encoded, err := section.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if got := encoded["translateScreenshotAccelerator"]; got != "" {
		t.Fatalf("stored = %#v, want empty", got)
	}
}

func TestSettingsAreBroughtIntoTheirBounds(t *testing.T) {
	cases := []struct {
		name string
		in   Settings
		want Settings
	}{
		{
			name: "empty provider falls back",
			in:   Settings{Provider: "  "},
			want: Settings{Provider: DefaultProvider},
		},
		{
			name: "delay below zero falls back",
			in:   Settings{CloseDelay: -time.Second},
			want: Settings{CloseDelay: DefaultCloseDelay},
		},
		{
			name: "delay above the bound is clamped",
			in:   Settings{CloseDelay: time.Hour},
			want: Settings{CloseDelay: MaxCloseDelay},
		},
		{
			name: "zero delay is allowed",
			in:   Settings{CloseDelay: 0},
			want: Settings{CloseDelay: 0},
		},
		{
			name: "a bare host becomes https",
			in:   Settings{Custom: map[string]CustomSettings{"custom": {BaseURL: "api.example.com/v1/"}}},
			want: Settings{Custom: map[string]CustomSettings{"custom": {BaseURL: "https://api.example.com/v1"}}},
		},
		{
			name: "another scheme is dropped, and so is the service",
			in:   Settings{Custom: map[string]CustomSettings{"custom": {BaseURL: "file:///etc/passwd"}}},
			want: Settings{Custom: map[string]CustomSettings{}},
		},
		{
			name: "a long name is cut",
			in: Settings{Custom: map[string]CustomSettings{
				"custom": {
					BaseURL:     "https://api.example.com/v1",
					DisplayName: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ",
				},
			}},
			want: Settings{Custom: map[string]CustomSettings{
				"custom": {
					BaseURL:     "https://api.example.com/v1",
					DisplayName: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMN",
				},
			}},
		},
		{
			name: "several services keep their own ids",
			in: Settings{Custom: map[string]CustomSettings{
				"custom":   {BaseURL: "http://127.0.0.1:11434/v1", DisplayName: "本地"},
				"custom-2": {BaseURL: "https://api.example.com/v1", DisplayName: "远端"},
			}},
			want: Settings{Custom: map[string]CustomSettings{
				"custom":   {BaseURL: "http://127.0.0.1:11434/v1", DisplayName: "本地"},
				"custom-2": {BaseURL: "https://api.example.com/v1", DisplayName: "远端"},
			}},
		},
		{
			name: "a service with no address is dropped rather than kept nameless",
			in: Settings{Custom: map[string]CustomSettings{
				"custom":   {BaseURL: "https://api.example.com/v1"},
				"custom-2": {DisplayName: "空地址"},
			}},
			want: Settings{Custom: map[string]CustomSettings{
				"custom": {BaseURL: "https://api.example.com/v1"},
			}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.in.Normalize()
			// 除第一个用例外，其余都不涉及 provider，而规范化在未指定时会给出默认值。
			wantProvider := tc.want.Provider
			if wantProvider == "" {
				wantProvider = DefaultProvider
			}
			if got.Provider != wantProvider {
				t.Errorf("provider = %q, want %q", got.Provider, wantProvider)
			}
			if got.CloseDelay != tc.want.CloseDelay {
				t.Errorf("close delay = %s, want %s", got.CloseDelay, tc.want.CloseDelay)
			}
			if !sameServices(got.Custom, tc.want.Custom) {
				t.Errorf("custom = %+v, want %+v", got.Custom, tc.want.Custom)
			}
		})
	}
}

// sameServices 比较两组用户服务，把 nil map 与空 map 视为相同：
// 重要的是有哪些服务，而非该 map 是否被分配。
func sameServices(got, want map[string]CustomSettings) bool {
	if len(got) != len(want) {
		return false
	}
	for id, service := range want {
		if got[id] != service {
			return false
		}
	}
	return true
}

// 新增服务取第一个空闲 id，文件中已持有的 id 不会被再次分配。
func TestNextCustomServiceID(t *testing.T) {
	cases := []struct {
		name string
		have map[string]CustomSettings
		want string
	}{
		{"nothing stored", nil, "custom"},
		{"the first is taken", map[string]CustomSettings{"custom": {}}, "custom-2"},
		{"two are taken", map[string]CustomSettings{"custom": {}, "custom-2": {}}, "custom-3"},
		{"a gap in the middle is not reused past the end",
			map[string]CustomSettings{"custom": {}, "custom-3": {}}, "custom-2"},
		{"only a later id is taken", map[string]CustomSettings{"custom-2": {}}, "custom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			settings := Settings{Custom: tc.have}
			if got := settings.NextCustomServiceID(); got != tc.want {
				t.Errorf("NextCustomServiceID() = %q, want %q", got, tc.want)
			}
		})
	}
}

// 用户自有服务的 id 靠前缀识别，其它一律不算。
func TestIsCustomService(t *testing.T) {
	own := []string{"custom", "custom-2", "custom-10"}
	for _, id := range own {
		if !IsCustomService(id) {
			t.Errorf("%q is not recognised as the user's own service", id)
		}
	}
	others := []string{
		"", "deepseek", "free.microsoft", "free.bilibili",
		// 不是带分隔符的前缀：仅仅以该词开头的翻译器是另一回事。
		"customary", "customs-2", "Custom-2",
	}
	for _, id := range others {
		if IsCustomService(id) {
			t.Errorf("%q is treated as the user's own service", id)
		}
	}
}

// 每个用户可能留空的设置都有其空值答案：某个键存在前写下的文件按默认值读，
// 而不是零值。
//
// 关闭延迟是例外，也是规则按设置单独写、而非对整个结构体统一写的原因：零意味着
// 「立即关闭」，这是用户可选的值，所以缺席的延迟读作无延迟而不是默认延迟。
func TestTheEmptySettingsAnswerWithTheirDefaults(t *testing.T) {
	var empty Settings
	if empty.ProviderOr() != DefaultProvider {
		t.Errorf("ProviderOr of nothing = %q", empty.ProviderOr())
	}
	if empty.AcceleratorOr() != DefaultAccelerator {
		t.Errorf("AcceleratorOr of nothing = %q", empty.AcceleratorOr())
	}
	if empty.CloseDelayOr() != 0 {
		t.Errorf("CloseDelayOr of nothing = %s, want no delay: zero is a choice, not a gap",
			empty.CloseDelayOr())
	}
}

// 零是用户想让卡片立即关闭时的选择，所以写着零的文件必须读作零。
// 曾经它被读成「键不存在」——结果悄悄把这类用户下次启动时打回默认值。
func TestAZeroCloseDelayReadsAsZero(t *testing.T) {
	section := NewSection()
	values := map[string]json.RawMessage{
		keyCloseDelay: json.RawMessage(`0`),
	}
	if err := section.Decode(values); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if got := section.Current().CloseDelay; got != 0 {
		t.Errorf("CloseDelay = %s, want 0: the file said zero", got)
	}
}

// 未提及该键的文件保持默认：两种情况不同，区分它们正是修复的核心。
func TestAnAbsentCloseDelayKeepsTheDefault(t *testing.T) {
	section := NewSection()
	if err := section.Decode(map[string]json.RawMessage{}); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if got := section.Current().CloseDelayOr(); got != DefaultCloseDelay {
		t.Errorf("CloseDelayOr = %s, want the default %s", got, DefaultCloseDelay)
	}
}

// 零写入文件又读回来仍是零：重启后设置窗口显示的就是用户所选。
func TestAZeroCloseDelaySurvivesTheFile(t *testing.T) {
	section := NewSection()
	settings := section.Current()
	settings.CloseDelay = 0
	section.Set(settings)
	encoded, err := section.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if got, want := fmt.Sprint(encoded[keyCloseDelay]), "0"; got != want {
		t.Errorf("%s = %s, want %s", keyCloseDelay, got, want)
	}
	values := map[string]json.RawMessage{}
	for key, value := range encoded {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal %s: %v", key, err)
		}
		values[key] = raw
	}
	reread := NewSection()
	if err := reread.Decode(values); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if got := reread.Current().CloseDelay; got != 0 {
		t.Errorf("after the round trip CloseDelay = %s, want 0", got)
	}
}
