package presentation

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// 两个外观都必须选齐每一种颜色。某个调色板字段在两个字面量中漏掉就是零色，
// 绘制出来什么都没有，而且不会编译报错——这是编译器无法为颜色表做的检查。
//
// 浅色外观的 tileEdge 故意缺席：它没有 provider 磁贴的描边，因此值为 ui.Transparent。
func TestBothPalettesChooseEveryColor(t *testing.T) {
	for _, test := range []struct {
		name  string
		p     Palette
		clear string
	}{
		{name: "light", p: LightPalette, clear: "tileEdge"},
		{name: "dark", p: DarkPalette},
	} {
		value := reflect.ValueOf(test.p)
		var checkColor func(reflect.Value, string)
		checkColor = func(color reflect.Value, field string) {
			if color.Type() == reflect.TypeFor[ReadingAccent]() {
				for index := 0; index < color.NumField(); index++ {
					checkColor(color.Field(index), field+"."+color.Type().Field(index).Name)
				}
				return
			}
			// ui.Color 的第四个字节是 alpha。缺了它的颜色不论 RGB 是什么都画不出来。
			if alpha := color.Field(3).Uint(); alpha == 0 {
				t.Errorf("%s palette: %s has no alpha", test.name, field)
			}
		}
		for index := 0; index < value.NumField(); index++ {
			field := value.Type().Field(index).Name
			if field == test.clear {
				continue
			}
			checkColor(value.Field(index), field)
		}
	}
}

// 两个外观都必须指定应用字体：它保证中文字形无论系统语言是什么都形状一致，
// 留空的主题会回退到框架按脚本的字形。两个主题只在颜色上不同，字体相同。
func TestBothAppearancesNameTheApplicationFont(t *testing.T) {
	for _, dark := range []bool{false, true} {
		if got := Theme(dark).Font; got != UiFontFamily {
			t.Errorf("theme(dark=%v).Font = %q, want %q", dark, got, UiFontFamily)
		}
	}
}

// 字体列表以 sans-serif 结尾：在没有任何具名字体的系统上，字符会用平台提供的
// 字体绘制而不是被拒绝。CJK 字体必须在列表结束之前命名，这正是本任务的目的。
func TestUIFontFamilyEndsAtTheSystemFallback(t *testing.T) {
	families := strings.Split(UiFontFamily, ", ")
	for i, family := range families {
		families[i] = strings.Trim(strings.TrimSpace(family), `"`)
	}
	if last := families[len(families)-1]; last != "sans-serif" {
		t.Errorf("the family list ends at %q, want sans-serif", last)
	}
	// CJK 字体必须在通用回退之前命名：排在通用字体之后的字体永远不会被用到。
	for _, want := range []string{"Microsoft YaHei", "PingFang SC", "Noto Sans CJK SC"} {
		if !slices.Contains(families, want) {
			t.Errorf("the family list does not name %q: %q", want, families)
		}
	}
}
