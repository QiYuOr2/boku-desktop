package openai

import (
	"strings"
	"testing"
	"time"

	"boku/internal/domain"
)

func TestTextResultKeepsPlainAnswerAndLetsACutWin(t *testing.T) {
	at := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	plain, err := TextResult("  hello  ", "hello", domain.LangEnglish, domain.LangChinese, "deepseek", at, AnnotationOptions{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if plain.TranslatedText != "  hello  " || plain.Note != "" || plain.ProviderID != "deepseek" {
		t.Fatalf("plain = %+v", plain)
	}

	cut, err := TextResult("短", strings.Repeat("a", DefaultMaxChars), domain.LangEnglish, domain.LangChinese, "custom", at, AnnotationOptions{Furigana: true}, true)
	if err != nil {
		t.Fatal(err)
	}
	if cut.Note != TruncationNote(true) {
		t.Fatalf("note = %q", cut.Note)
	}
	if TruncationNote(false) != "" {
		t.Fatal("an uncut text produced a truncation note")
	}
}

func TestPrepareDetectsAnUnknownSource(t *testing.T) {
	text, source, cut := Prepare(domain.TranslationRequest{Text: "hello", SourceLang: domain.LangUnknown, TargetLang: domain.LangChinese})
	if text != "hello" || source == domain.LangUnknown || cut {
		t.Fatalf("prepare = %q %q cut=%v", text, source, cut)
	}
}

func TestImageResultUsesTheCallerTarget(t *testing.T) {
	at := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	result, err := ImageResult(ImageResponse{
		Status: "success", SourceLang: domain.LangEnglish, SourceText: "  hi  ", TranslatedText: "  你好  ",
	}, domain.LangChinese, "deepseek", at)
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceText != "hi" || result.TranslatedText != "你好" || result.TargetLang != domain.LangChinese || result.SourceLang != domain.LangEnglish {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(ImageSystemPrompt, "no_readable_text") {
		t.Fatal("image prompt lost the failure code")
	}
}
