package kit

import "testing"

// The language guess is what lets the window name the source language and
// pick a target before anything is sent anywhere. It reads scripts, so the
// cases below are about which script wins when a text has more than one.
func TestDetectLanguage(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"english", "A compiler is a software that converts source code.", "en"},
		{"chinese", "编译器是将源代码转换成机器语言的软件。", "zh"},
		{"japanese with kana", "コンパイラはソースコードを機械語に変換するソフトです。", "ja"},
		// The interesting one: a Japanese sentence written mostly in 漢字 is
		// still Japanese, because no Chinese text has a kana in it.
		{"japanese mostly kanji", "機械語に変換するソフト", "ja"},
		{"japanese with one kana", "日本語の文章です", "ja"},
		{"korean", "컴파일러는 소스 코드를 기계어로 변환하는 소프트웨어입니다.", "ko"},
		{"russian", "Компилятор преобразует исходный код.", "ru"},
		{"arabic", "المترجم يحول الكود المصدري.", "ar"},
		{"empty", "", ""},
		{"whitespace", "   \n\t ", ""},
		{"numbers and punctuation", "42.10 % — (1,234)", ""},
		{"emoji only", "🙂🎉", ""},
		{"one letter", "A", "en"},
		{"one kana among kanji", "日本語のテキスト", "ja"},
		// A URL is Latin even though it is not a word: the badge may call it
		// English, which is a better answer than naming no language.
		{"url", "https://example.com/a", "en"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectLanguage(tc.text); got != tc.want {
				t.Errorf("DetectLanguage(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

func TestLanguageName(t *testing.T) {
	cases := map[string]string{
		"zh":      "中文",
		"zh-Hans": "中文",
		"ZH":      "中文",
		"ja":      "日本語",
		"en":      "English",
		"ko":      "한국어",
		"fr":      "Français",
		"xx":      "xx",
		"":        "",
	}
	for code, want := range cases {
		if got := LanguageName(code); got != want {
			t.Errorf("LanguageName(%q) = %q, want %q", code, got, want)
		}
	}
}

func TestIsChinese(t *testing.T) {
	for _, code := range []string{"zh", "zh-CN", "ZH-Hant", "cn", "zho"} {
		if !IsChinese(code) {
			t.Errorf("IsChinese(%q) = false", code)
		}
	}
	for _, code := range []string{"", "ja", "en", "zhx"} {
		if IsChinese(code) {
			t.Errorf("IsChinese(%q) = true", code)
		}
	}
}

// The target direction: everything that is not Chinese becomes Chinese, and
// Chinese becomes English. The second half is what keeps a Chinese
// selection from being a dead end.
func TestTargetLanguage(t *testing.T) {
	cases := map[string]string{
		"en": "zh",
		"ja": "zh",
		"ko": "zh",
		"ru": "zh",
		"":   "zh",
		"zh": "en",
	}
	for source, want := range cases {
		if got := TargetLanguage(source); got != want {
			t.Errorf("TargetLanguage(%q) = %q, want %q", source, got, want)
		}
	}
}
