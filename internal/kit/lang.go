// 本文件从文本使用的书写系统猜测语言。与包内其他内容一样，是文本的纯函数。
package kit

import (
	"strings"
	"unicode"

	"boku/internal/domain"
)

// DetectLanguage 根据文本字母所属的书写系统猜测语言。
// 这是刻意的、廉价的猜测：读取用户已复制的文本，让窗口在任何内容发出前
// 就能命名源语言并选择目标。它不是会消耗请求的检测器；对于与其他语言共用
// 书写系统的情况（中文和日文都写漢字）也不保证正确。
// 下面的顺序让共用书写系统得以区分：假名判定日语（即便文本多为漢字），
// 谚文判定韩语，只有不含假名和谚文时才判为中文。返回应用使用的语言代码；
// 空字符串表示文本没有可用线索——数字、标点或空字符串。
func DetectLanguage(text string) string {
	var han, kana, hangul, cyrillic, arabic, thai, latin, other int
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
		case unicode.In(r, unicode.Thai):
			thai++
		case unicode.In(r, unicode.Latin):
			latin++
		case unicode.IsLetter(r):
			other++
		}
	}
	// 一个假名字符就足以区分日语与中文：中文文本没有假名，
	// 而多为漢字的日文总会有几个。
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
	case thai > 0:
		return "th"
	case latin > 0:
		return "en"
	case other > 0:
		// 本版本没有对应词的书写系统：返回空代码是最诚实的答案，窗口如实显示。
		return ""
	}
	return ""
}

// ImageSourceLanguage 用识别出的文本校正图片模型声称的源语言。
// 模型可能返回不支持的语言代码，或把它明显识别出的书写系统标成英语。
// 本地书写系统匹配是更强的答案；否则有效的模型答案在本版本无法识别的
// 书写系统上仍有用。Unknown 是刻意为之，让好的源/译对保持可见而不出现错误语言徽标。
func ImageSourceLanguage(claimed domain.Language, sourceText string) domain.Language {
	if detected := domain.Language(DetectLanguage(sourceText)); detected.Valid() {
		return detected
	}
	if claimed.Valid() {
		return claimed
	}
	return domain.LangUnknown
}

// LanguageName 返回界面某语言代码的称呼，用该语言自己的文字。没人认识的代码原样返回。
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
	case "th":
		return "ไทย"
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

// IsChinese 判断代码是否指中文，无论带何种变体：工具把中文当作同一目标，
// 服务返回 "zh-Hans" 也是同一个意思。
func IsChinese(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "zh", "zh-cn", "zh-hans", "zh-tw", "zh-hant", "cn", "chi", "zho":
		return true
	}
	return false
}

// TargetLanguage 根据源文本给出翻译目标语言：非中文一律译为中文，中文则译为英语。
// 这符合设计要求的「任意语言 → 中文」，第二个方向让中文文本不至于成为死胡同。
func TargetLanguage(source string) string {
	if IsChinese(source) {
		return "en"
	}
	return "zh"
}
