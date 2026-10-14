// Package tools holds the pure functions the application is built from:
// they take values and return values, and they depend on nothing — no
// window, no network, no database, no framework.
package tools

import (
	"strings"
	"unicode"
)

// Language guesses which language a piece of text is written in, from the
// scripts its letters come from.
//
// It is a guess by design, and a cheap one: it reads the text the user
// already copied, so the window can name the source language and pick a
// target before anything is sent anywhere. It is not a detector that costs a
// request, and it does not claim to be right about a language whose script
// is shared with another (Chinese and Japanese both write 漢字).
//
// The order below is what makes the shared scripts work: kana decide
// Japanese even in a text that is mostly 漢字, Hangul decides Korean, and
// only a text with no kana and no Hangul is read as Chinese. The result is
// the language code the rest of the application uses; an empty string means
// the text gave nothing to go on — numbers, punctuation, an empty string.
func DetectLanguage(text string) string {
	var han, kana, hangul, cyrillic, arabic, latin, other int
	for _, r := range text {
		switch {
		case unicode.In(r, unicode.Hiragana, unicode.Katakana):
			kana++
		case unicode.In(r, unicode.Hangul):
			hangul++
		case unicode.In(r, unicode.Han):
			han++
		case unicode.In(r, unicode.Cyrillic):
			cyrillic++
		case unicode.In(r, unicode.Arabic):
			arabic++
		case unicode.In(r, unicode.Latin):
			latin++
		case unicode.IsLetter(r):
			other++
		}
	}
	// A single kana character is enough to tell Japanese from Chinese: no
	// Chinese text has one, and a Japanese text that is mostly 漢字 always
	// has a few.
	switch {
	case kana > 0:
		return "ja"
	case hangul > 0:
		return "ko"
	case han > 0:
		return "zh"
	case cyrillic > 0:
		return "ru"
	case arabic > 0:
		return "ar"
	case latin > 0:
		return "en"
	case other > 0:
		// Letters of a script this build has no word for: the code is the
		// best honest answer, and the window shows it as it is.
		return ""
	}
	return ""
}

// LanguageName is what the interface calls a language code, in the
// language's own words. A code nobody knows is named by itself.
func LanguageName(code string) string {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "zh", "zh-cn", "zh-hans", "zh-tw", "zh-hant":
		return "中文"
	case "ja", "jp":
		return "日本語"
	case "en":
		return "English"
	case "ko":
		return "한국어"
	case "ru":
		return "Русский"
	case "ar":
		return "العربية"
	case "fr":
		return "Français"
	case "de":
		return "Deutsch"
	case "es":
		return "Español"
	case "pt":
		return "Português"
	case "it":
		return "Italiano"
	default:
		return strings.TrimSpace(code)
	}
}

// IsChinese reports whether the code names Chinese, whatever variant it
// carries: the tool treats 中文 as one target, and a service that answers
// "zh-Hans" has named the same thing.
func IsChinese(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "zh", "zh-cn", "zh-hans", "zh-tw", "zh-hant", "cn", "chi", "zho":
		return true
	}
	return false
}

// TargetLanguage is the language the tool translates into, given what the
// source text is: anything that is not Chinese becomes Chinese, and Chinese
// becomes English. It is what the design asks for — 任意语言 → 中文 — with a
// second direction so that Chinese text is not a dead end.
func TargetLanguage(source string) string {
	if IsChinese(source) {
		return "en"
	}
	return "zh"
}
