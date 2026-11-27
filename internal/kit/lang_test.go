package kit

import (
	"testing"

	"boku/internal/domain"
)

// 语言猜测让窗口在发出任何内容前就能命名源语言并选择目标。
// 它依据书写系统，故以下用例关注文本含多种书写系统时哪一种胜出。
func TestDetectLanguage(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"english", "A compiler is a software that converts source code.", "en"},
		{"chinese", "编译器是将源代码转换成机器语言的软件。", "zh"},
		{"japanese with kana", "コンパイラはソースコードを機械語に変換するソフトです。", "ja"},
		// 关键用例：主要由漢字构成的日语句子仍是日语，因为中文文本不含假名。
		{"japanese mostly kanji", "機械語に変換するソフト", "ja"},
		{"japanese with one kana", "日本語の文章です", "ja"},
		{"korean", "컴파일러는 소스 코드를 기계어로 변환하는 소프트웨어입니다.", "ko"},
		{"russian", "Компилятор преобразует исходный код.", "ru"},
		{"arabic", "المترجم يحول الكود المصدري.", "ar"},
		{"thai", "พระเจ้าลงโทษคนสร้างหอคอยบาเบล", "th"},
		{"empty", "", ""},
		{"whitespace", "   \n\t ", ""},
		{"numbers and punctuation", "42.10 % — (1,234)", ""},
		{"emoji only", "🙂🎉", ""},
		{"one letter", "A", "en"},
		{"one kana among kanji", "日本語のテキスト", "ja"},
		// URL 虽是拉丁字母却不是单词：徽标称其为英语，比不给出任何语言更好。
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

func TestImageSourceLanguage(t *testing.T) {
	cases := []struct {
		name    string
		claimed domain.Language
		text    string
		want    domain.Language
	}{
		{"recognized script corrects a model's wrong claim", domain.LangEnglish, "พระเจ้าลงโทษคน", domain.LangThai},
		{"recognized script corrects an unknown claim", domain.Language("vi"), "พระเจ้าลงโทษคน", domain.LangThai},
		{"valid claim remains when the script is unknown", domain.LangEnglish, "გამარჯობა", domain.LangEnglish},
		{"unknown claim and script become unknown", domain.Language("ka"), "გამარჯობა", domain.LangUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ImageSourceLanguage(tc.claimed, tc.text); got != tc.want {
				t.Errorf("ImageSourceLanguage(%q, %q) = %q, want %q", tc.claimed, tc.text, got, tc.want)
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
		"th":      "ไทย",
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

// 目标方向：非中文一律变中文，中文变英语。后半段让中文选区不至于成为死胡同。
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
