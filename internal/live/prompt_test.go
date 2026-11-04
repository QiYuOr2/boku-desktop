package live

import (
	"strings"
	"testing"
)

func TestPromptWithoutTermsUsesTheDefaultTemplate(t *testing.T) {
	got := Prompt(" こんにちは ", nil)
	want := "将以下文本翻译为中文，注意**只需要输出翻译后的结果，不要额外解释**：\n\nこんにちは"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestPromptWithTermsUsesOnlyTheTerminologyTemplate(t *testing.T) {
	got := Prompt("部室に行く", []Entry{{Source: "部室", Target: "社团活动室"}, {Source: "行く", Target: "去"}})
	want := "参考下面的翻译：\n部室 翻译成 社团活动室\n行く 翻译成 去\n将以下文本翻译为中文，注意**只需要输出翻译后的结果，不要额外解释**：\n\n部室に行く"
	if got != want {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "简体中文") || strings.Contains(got, "*参考") {
		t.Fatalf("template was decorated: %q", got)
	}
}
