package domain

import (
	"errors"
	"testing"
	"time"
)

// The translation contract is what a translator must answer, and what the
// application refuses to show: an empty answer is a failure at the boundary,
// not an empty window.

func TestTranslationRequestValidate(t *testing.T) {
	cases := []struct {
		name string
		req  TranslationRequest
		want error
	}{
		{
			name: "complete",
			req:  TranslationRequest{Text: "hello", TargetLang: LangChinese},
		},
		{
			name: "with a source language",
			req:  TranslationRequest{Text: "hello", SourceLang: LangEnglish, TargetLang: LangChinese},
		},
		{
			name: "no text",
			req:  TranslationRequest{TargetLang: LangChinese},
			want: ErrInvalidArgument,
		},
		{
			name: "blank text",
			req:  TranslationRequest{Text: "   \n", TargetLang: LangChinese},
			want: ErrInvalidArgument,
		},
		{
			name: "no target",
			req:  TranslationRequest{Text: "hello"},
			want: ErrInvalidArgument,
		},
		{
			name: "unknown target",
			req:  TranslationRequest{Text: "hello", TargetLang: "xx"},
			want: ErrInvalidArgument,
		},
		{
			name: "unknown source",
			req:  TranslationRequest{Text: "hello", SourceLang: "xx", TargetLang: LangChinese},
			want: ErrInvalidArgument,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if tc.want == nil {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v, want %v", err, tc.want)
			}
		})
	}
}

// An answer with no translation is not an answer: a translator that returns
// nothing must become a visible failure rather than a window with a blank
// half.
func TestTranslationResultValidate(t *testing.T) {
	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	good := TranslationResult{
		SourceText:     "コンパイラ",
		TranslatedText: "编译器",
		SourceLang:     LangJapanese,
		TargetLang:     LangChinese,
		ProviderID:     "deepseek",
		CreatedAt:      at,
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}

	segmented := good
	segmented.TranslatedText = ""
	segmented.Segments = []Segment{
		{Text: "コンパイラ", Reading: "コンパイラ", Gloss: "compiler", Target: "编译器"},
	}
	if err := segmented.Validate(); err != nil {
		t.Errorf("a result made of segments alone is not valid: %v", err)
	}
	if !segmented.Annotated() {
		t.Error("a result with readings is not reported as annotated")
	}
	if good.Annotated() {
		t.Error("a result with no segments is reported as annotated")
	}

	bad := []struct {
		name string
		in   TranslationResult
		want error
	}{
		{"no source text", func() TranslationResult { r := good; r.SourceText = ""; return r }(), ErrProviderBadResponse},
		{"no translation", func() TranslationResult { r := good; r.TranslatedText = "  "; return r }(), ErrProviderBadResponse},
		{"no target", func() TranslationResult { r := good; r.TargetLang = ""; return r }(), ErrProviderBadResponse},
		{"unknown target", func() TranslationResult { r := good; r.TargetLang = "xx"; return r }(), ErrProviderBadResponse},
		{"unknown source", func() TranslationResult { r := good; r.SourceLang = "xx"; return r }(), ErrProviderBadResponse},
		{"empty segment", func() TranslationResult {
			r := good
			r.Segments = []Segment{{Text: "  "}}
			return r
		}(), ErrInvalidArgument},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.in.Validate()
			if !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestTranslatorDescriptorValidate(t *testing.T) {
	good := TranslatorDescriptor{
		ID:             "deepseek",
		DisplayName:    "DeepSeek",
		CredentialKeys: []string{"apiKey"},
		DocsURL:        "https://platform.deepseek.com/api_keys",
		Endpoint:       "https://api.deepseek.com/chat/completions",
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}

	// A translator whose address is the user's to name is valid without an
	// endpoint: the setting is the address.
	bySetting := good
	bySetting.ID, bySetting.Endpoint, bySetting.BaseURLSetting = "custom", "", "custom.baseURL"
	if err := bySetting.Validate(); err != nil {
		t.Errorf("a translator addressed by a setting is not valid: %v", err)
	}

	bad := []struct {
		name string
		in   TranslatorDescriptor
	}{
		{"no id", func() TranslatorDescriptor { d := good; d.ID = ""; return d }()},
		{"no name", func() TranslatorDescriptor { d := good; d.DisplayName = " "; return d }()},
		{"no credentials", func() TranslatorDescriptor { d := good; d.CredentialKeys = nil; return d }()},
		{"empty credential key", func() TranslatorDescriptor { d := good; d.CredentialKeys = []string{" "}; return d }()},
		{"duplicate credential key", func() TranslatorDescriptor { d := good; d.CredentialKeys = []string{"apiKey", "apiKey"}; return d }()},
		{"no docs", func() TranslatorDescriptor { d := good; d.DocsURL = ""; return d }()},
		{"nowhere to ask", func() TranslatorDescriptor { d := good; d.Endpoint = ""; return d }()},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.in.Validate(); !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("Validate() = %v, want ErrInvalidArgument", err)
			}
		})
	}
}

// A translator that runs on the user's own machine works without a key, and
// the descriptor is what says so.
func TestTranslatorDescriptorNeedsCredential(t *testing.T) {
	local := TranslatorDescriptor{
		ID:                   "custom",
		DisplayName:          "自定义",
		CredentialKeys:       []string{"apiKey"},
		DocsURL:              "https://platform.openai.com/api-keys",
		BaseURLSetting:       "custom.baseURL",
		AllowEmptyCredential: true,
	}
	if local.NeedsCredential("apiKey") {
		t.Error("a local translator still requires a key")
	}
	remote := local
	remote.AllowEmptyCredential = false
	if !remote.NeedsCredential("apiKey") {
		t.Error("a remote translator does not require the key it asked for")
	}
	if remote.NeedsCredential("adminKey") {
		t.Error("a translator requires a key it never asked for")
	}
}

func TestLanguageNameAndValid(t *testing.T) {
	if !LangChinese.Valid() || !LangJapanese.Valid() || !LangThai.Valid() {
		t.Error("a known language is not valid")
	}
	if Language("xx").Valid() {
		t.Error("an unknown language is valid")
	}
	if LangUnknown.Valid() {
		t.Error("the empty language is valid")
	}
	if got := LangChinese.Name(); got != "中文" {
		t.Errorf("LangChinese.Name() = %q", got)
	}
	if got := LangThai.Name(); got != "ไทย" {
		t.Errorf("LangThai.Name() = %q", got)
	}
	// A code this build has no word for is named by itself, so a translator
	// may answer with one without the window going blank.
	if got := Language("sv").Name(); got != "sv" {
		t.Errorf("Language(\"sv\").Name() = %q, want the code itself", got)
	}
}
