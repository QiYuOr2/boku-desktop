package openai

import (
	"errors"
	"strings"
	"testing"

	"boku/internal/domain"
)

func TestParseImageResponse(t *testing.T) {
	answer, err := ParseImageResponse(`{"status":"success","sourceLang":"en","sourceText":"Hello","targetLang":"zh","translatedText":"你好","failure":null}`)
	if err != nil {
		t.Fatal(err)
	}
	if answer.SourceText != "Hello" || answer.TranslatedText != "你好" {
		t.Errorf("answer = %+v", answer)
	}
}

func TestParseImageResponsePreservesStructuredModelFailure(t *testing.T) {
	_, err := ParseImageResponse(`{"status":"failure","sourceLang":"","sourceText":"","targetLang":"","translatedText":"","failure":{"code":"no_readable_text"}}`)
	var failure *domain.ImageTranslationError
	if !errors.As(err, &failure) || failure.Code != domain.ImageFailureNoReadableText {
		t.Fatalf("error = %v, want no_readable_text failure", err)
	}
}

func TestParseImageResponseRejectsAnUnknownFailureCode(t *testing.T) {
	_, err := ParseImageResponse(`{"status":"failure","failure":{"code":"overloaded"}}`)
	if !errors.Is(err, domain.ErrProviderBadResponse) {
		t.Fatalf("error = %v, want ErrProviderBadResponse", err)
	}
}

func TestReadSSEConsumesAMultilineEvent(t *testing.T) {
	stream := strings.NewReader("data: {\"choices\":[{\"delta\":\n" +
		"data: {\"content\":\"answer\"}}]}\n\n" +
		"data: [DONE]\n\n")
	got, err := readSSE(stream)
	if err != nil {
		t.Fatal(err)
	}
	if got != "answer" {
		t.Errorf("answer = %q, want answer", got)
	}
}
