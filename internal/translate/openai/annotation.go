package openai

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"boku/internal/domain"
)

// 日文设置要求译者给出比译文更多的内容：原文的每个词、它的读音，以及片假名外来语的
// 原词。模型每行返回一个 JSON 对象，本文件负责把它转成片段。
//
// 解析器刻意宽容：模型输出不是契约，可能包在代码围栏里、字段写全称、漏字段、或只
// 给译文。这些都不算失败——窗口总能画出返回的内容——所以这里不返回错误。无法读成
// 片段的内容当作纯文本，若一条都读不出，整段回答即译文。

// AnnotationOptions 是域自身的类型别名：要哪些标注由用户设置决定。
type AnnotationOptions = domain.AnnotationOptions

// SystemPrompt 决定回答形状的指令：未要求标注时是纯译文，日文设置要标注时是逐行
// 注解。
//
// 提示词用界面自身的语言称呼语种：模型更懂「翻译成中文」而不是「translate into zh」。
//
// 其中两条是用户的硬要求，都用模型无法读作可选的措辞表达：片假名外来语必须带上它的
// 原词（モード 是 "mode"，读 模式 的日文使用者想看到它），且译文必须保留原文自身的
// 标点：原文【回想モード完備】应回答【完备回想模式】，括号也要保留。
// 示例同时体现这两条规则，也告诉模型标点单独成片段——括号正是如此才得以保留。
func SystemPrompt(source, target domain.Language, opts AnnotationOptions) string {
	into := target.Name()
	from := "用户给出的原文"
	if source != domain.LangUnknown {
		from = "用户给出的" + source.Name() + "原文"
	}
	if !opts.Wants() {
		return fmt.Sprintf("你是翻译引擎。把%s翻译成%s。"+
			"只输出译文本身：不要解释，不要加引号，不要重复原文。"+
			"译文必须保留原文的标点与排版：原文的括号（【】「」『』（）等）、引号、顿号、"+
			"省略号、换行、项目符号都要照原样出现在译文里，一处不漏、一处不添。", from, into)
	}
	lines := []string{
		fmt.Sprintf("你是翻译引擎，把%s翻译成%s。", from, into),
		"输出要求：把原文按词或词组切成片段（一个词/词组一个片段，按原文顺序排列），" +
			"每个片段一行，每行一个 JSON 对象，字段如下：",
		`  "s"：原文片段（必填，必须与原文逐字一致）`,
		`  "t"：该片段的译文（必填；片段本身是标点或符号时，与 "s" 相同）`,
	}
	if opts.Furigana {
		lines = append(lines,
			`  "r"：该片段的读音，用片假名写出。只有含汉字的词/词组才给 "r"`)
	}
	if opts.LoanwordGloss {
		lines = append(lines,
			`  "g"：片段是片假名外来语时，写出它来源语言的单词（通常是小写英文）`)
	}
	var notes []string
	if opts.Furigana {
		notes = append(notes,
			`· 汉字按词组注音，不要拆成单个汉字：「回想」整个给一个 "r"（カイソウ），`+
				`不要 回=カイ、想=ソウ；`,
			`· 句子里的平假名（助词、词尾、送假名）不给 "r"：「は」「です」「を」「ました」`+
				`这些片段上面是空的；`,
			`· 不注音的片段省略 "r" 这个字段，不要写成 "r":""。`)
	}
	if opts.LoanwordGloss {
		notes = append(notes,
			`· 片假名外来语用它的原词注音，而不是用片假名注音：「モード」上面写 mode；`+
				`外来语给 "g"、不给 "r"（它的读音就是它自己）。`)
	}
	if len(notes) > 0 {
		lines = append(lines, "注音规矩（重要）：")
		lines = append(lines, notes...)
	}
	lines = append(lines,
		"两条硬要求：",
		"1. 所有片段的 \"s\" 按顺序拼起来，必须与原文完全一致：标点、括号、空格、换行都原样保留；",
		"2. 译文必须保留原文的格式与标点：原文有【】「」『』（）等括号、引号、省略号、换行、"+
			"项目符号，译文里同样要有，位置一一对应，不增不减。",
		"例如原文【回想モード完備】，正确的输出是：")
	lines = append(lines, annotationExample(opts)...)
	lines = append(lines,
		"不要输出 JSON 数组、代码块或任何解释，每行一个对象。",
		"若原文不是日语，也要逐词输出，只是没有读音。")
	return strings.Join(lines, "\n")
}

// annotationExample 是提示词的示例：【回想モード完備】每段一行，可选字段只在
// 被要求时才出现。
//
// 它同时体现三条规则，比一句说明更有价值：回想 是一段一个读音而非两字两音，モード
// 带原词而无自身读音，括号是译成自身的片段。
func annotationExample(opts AnnotationOptions) []string {
	line := func(s, t, reading, gloss string) string {
		var b strings.Builder
		b.WriteString(`{"s":"` + s + `"`)
		if opts.Furigana && reading != "" {
			b.WriteString(`,"r":"` + reading + `"`)
		}
		if opts.LoanwordGloss && gloss != "" {
			b.WriteString(`,"g":"` + gloss + `"`)
		}
		b.WriteString(`,"t":"` + t + `"}`)
		return b.String()
	}
	return []string{
		line("【", "【", "", ""),
		line("回想", "回想", "カイソウ", ""),
		line("モード", "模式", "", "mode"),
		line("完備", "完备", "カンビ", ""),
		line("】", "】", "", ""),
	}
}

// readingFor 是片段该带的读音：只属于它自己的读音。
//
// 读音是给汉字的——设置因此得名，界面也把它画在词的上方。给每段都注音的模型会在
// は 和 モード 上方写片假名：前者是没人需要的读音，后者就是词本身，说了等于没说。
// 这两者不会当错误丢掉——回答仍是译文——但窗口只画用户真正要的标注，不随模型乱给。
func readingFor(text, reading string) string {
	if reading == "" || !hasHan(text) {
		// 假名、标点、数字没有可读的东西；外来语也算在内，它的片假名本身就是读音。
		return ""
	}
	return reading
}

// glossFor 是片段该带的释义：外来语的原词，只有片假名写成的片段才有。
func glossFor(text, gloss string) string {
	if gloss == "" || !hasKatakana(text) {
		return ""
	}
	return gloss
}

// hasHan 报告文本里是否有汉字。
func hasHan(text string) bool {
	for _, r := range text {
		if unicode.In(r, unicode.Han) {
			return true
		}
	}
	return false
}

// looksLikeData 报告一段回答是否为回答自身的格式而非可读内容：单独或跨行的、完整
// 或残缺的 JSON 对象或数组。
//
// 它只为一条规则存在：窗口显示的是译文。回答得糟糕的模型不能把
// `{"s":"\n","t":"\n"}` 摆在用户面前——那不是译文，窗口会改用「无法使用此回答」的
// 提示。
func looksLikeData(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	switch trimmed[0] {
	case '{', '[':
		return true
	}
	last := trimmed[len(trimmed)-1]
	return last == '}' || last == ']'
}

// hasKatakana 报告文本里是否有片假名。
func hasKatakana(text string) bool {
	for _, r := range text {
		if unicode.In(r, unicode.Katakana) {
			return true
		}
	}
	return false
}

// Annotation 是从回答中读出的内容：片段、它们拼成的译文，以及译文能否被切分。
type Annotation struct {
	Segments []domain.Segment
	// TranslatedText 是各片段译文按给定顺序拼接；无法分段的回答就是原样整段。
	TranslatedText string
	// Segmented 报告回答能否读成片段。纯译文为 false，此时窗口把原文画成一行。
	Segmented bool
	// UnreadableLines 是非 JSON 对象的行数。这些行仍会以纯文本显示，但调用方可据此
	// 说明回答未被完全理解。全部读成片段、或本就不需分段时为 0。
	UnreadableLines int
}

// ParseAnnotation 读取带标注的回答。
//
// 它从不失败：不是 JSON 对象列表的回答就是纯译文，窗口能画出它。
func ParseAnnotation(content string) Annotation {
	trimmed := strings.TrimSpace(content)
	lines := answerLines(trimmed)
	// 用单个 JSON 数组作答的模型虽不合提示，但意思相同：把数组元素当作各行。
	// 否则一整行紧凑数组会成为一行不可读内容，用户要的标注就被画成纯译文了。
	if elements, ok := answerArray(trimmed); ok {
		lines = elements
	}
	if len(lines) == 0 {
		return Annotation{TranslatedText: trimmed}
	}
	var (
		segments   []domain.Segment
		translated []string
		unreadable int
		// dropped 表示回答里夹带了无法读取的服务自身格式。它们不会显示，回答取可读的
		// 行而非原始内容。
		dropped bool
	)
	for _, line := range lines {
		var parsed struct {
			Text    string `json:"s"`
			Reading string `json:"r"`
			Gloss   string `json:"g"`
			Target  string `json:"t"`
			// 字段全称，用于无视提示字段名、自造名字的模型。
			TextLong    string `json:"text"`
			ReadingLong string `json:"reading"`
			GlossLong   string `json:"gloss"`
			TargetLong  string `json:"translation"`
		}
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			// 纯服务格式的行不显示：窗口显示译文，绝不能因模型格式错误就把线格式塞给用户。
			if looksLikeData(line) {
				dropped = true
			} else {
				translated = append(translated, line)
			}
			unreadable++
			continue
		}
		text := firstNonEmpty(parsed.Text, parsed.TextLong)
		target := firstNonEmpty(parsed.Target, parsed.TargetLong)
		if text == "" {
			// 未指明原文片段的对象不是片段：没有东西可承载它。若它带译文，译文保留。
			switch {
			case target != "":
				translated = append(translated, target)
			case looksLikeData(line):
				dropped = true
			default:
				translated = append(translated, line)
			}
			unreadable++
			continue
		}
		segments = append(segments, domain.Segment{
			Text: text,
			// 属于片段的标注：含汉字的词给读音，片假名片段给释义。模型放错位置的东西
			// 在此剔除而非画出。
			Reading: readingFor(text, firstNonEmpty(parsed.Reading, parsed.ReadingLong)),
			Gloss:   glossFor(text, firstNonEmpty(parsed.Gloss, parsed.GlossLong)),
			Target:  target,
		})
		if target != "" {
			translated = append(translated, target)
		} else {
			// 模型标注了却没翻译的片段：用片段本身顶替译文，免得段落出现空洞。
			translated = append(translated, text)
		}
	}
	if len(segments) == 0 {
		// 回答里没有能读成对象的内容。若回答里夹带服务格式，其花括号不是译文：可读部分
		// 即回答，其余丢弃而非画出。
		if dropped || looksLikeData(trimmed) {
			return Annotation{TranslatedText: joinTranslated(translated), UnreadableLines: unreadable}
		}
		// 否则就是译文本身，无论它怎么被包裹。
		return Annotation{TranslatedText: trimmed}
	}
	// 拉丁文字以空格分词，拼接时在分开书写两片段之间补回一个空格；汉字、假名、谚文与
	// 标点不空格，也不补。
	return Annotation{
		Segments:        segments,
		TranslatedText:  joinTranslated(translated),
		Segmented:       true,
		UnreadableLines: unreadable,
	}
}

// joinTranslated 把译文的片段重新拼起来。
//
// 它无从得知模型原本的空格位置，只用一条规则：仅当两侧都以分词书写时，两片段间才加
// 空格。这样拼日文能原样还原，拼拉丁文能保持词间距。
func joinTranslated(pieces []string) string {
	var b strings.Builder
	for _, piece := range pieces {
		if strings.TrimSpace(piece) == "" {
			continue
		}
		if b.Len() > 0 && separatesWords(lastRune(b.String())) && separatesWords(firstRune(piece)) {
			b.WriteByte(' ')
		}
		b.WriteString(piece)
	}
	return strings.TrimSpace(b.String())
}

// separatesWords 报告某种文字是否以空格分词，这决定两片段间是否该有空格。
func separatesWords(r rune) bool {
	switch {
	case r == 0:
		return false
	case unicode.IsSpace(r), unicode.IsPunct(r), unicode.IsSymbol(r):
		return false
	case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul):
		return false
	default:
		return true
	}
}

func firstRune(text string) rune {
	for _, r := range text {
		return r
	}
	return 0
}

func lastRune(text string) rune {
	var last rune
	for _, r := range text {
		last = r
	}
	return last
}

// answerLines 把回答拆成人会读的行：去掉代码围栏，丢弃空行与数组的标点。
func answerLines(content string) []string {
	text := strings.TrimSpace(strings.TrimPrefix(content, "\ufeff"))
	if strings.HasPrefix(text, "```") {
		if index := strings.Index(text, "\n"); index >= 0 {
			text = text[index+1:]
		}
		if index := strings.LastIndex(text, "```"); index >= 0 {
			text = text[:index]
		}
	}
	raw := strings.Split(text, "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ","))
		switch line {
		case "", "[", "]":
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// firstNonEmpty 返回首个非空的值。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// answerArray 把一个 JSON 数组形式的回答读成逐元素的 JSON 片段，读取方式与单行完全相同，
// 所以数组与对象列的结果一致。
//
// 若回答不是单个数组则返回 false，交回提示所要求的逐行读取。
func answerArray(content string) ([]string, bool) {
	text := strings.TrimSpace(strings.TrimPrefix(content, "\ufeff"))
	if strings.HasPrefix(text, "```") {
		if index := strings.Index(text, "\n"); index >= 0 {
			text = text[index+1:]
		}
		if index := strings.LastIndex(text, "```"); index >= 0 {
			text = text[:index]
		}
	}
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "[") {
		return nil, false
	}
	var elements []json.RawMessage
	if err := json.Unmarshal([]byte(text), &elements); err != nil {
		return nil, false
	}
	lines := make([]string, 0, len(elements))
	for _, element := range elements {
		lines = append(lines, string(element))
	}
	return lines, true
}

// AnnotationNote 是窗口对带标注回答该说的话：有多少行没读懂，以及——设置要了读音但
// 回答根本没有片段时——确实没有。后者让缺失的标注不至于看起来像本就没有标注的译文。
func AnnotationNote(annotation Annotation, opts AnnotationOptions) string {
	if !opts.Wants() {
		return ""
	}
	if !annotation.Segmented {
		return "翻译服务没有逐词分段，这次没有读音与外来语标注。"
	}
	if annotation.UnreadableLines > 0 {
		return "有 " + strconv.Itoa(annotation.UnreadableLines) + " 行没有读懂，按普通文本显示。"
	}
	return ""
}

// JoinSegments 是片段拼成的原文。窗口用片段而非发送内容来画原文，好让高亮的与读出的
// 是同一份。
func JoinSegments(segments []domain.Segment) string {
	pieces := make([]string, 0, len(segments))
	for _, segment := range segments {
		if text := strings.TrimSpace(segment.Text); text != "" {
			pieces = append(pieces, text)
		}
	}
	return joinTranslated(pieces)
}

// Truncate 把文本按字符边界截到至多 max 个字符，并报告是否截断过。它是工具自身上限：
// 选中内容是一句或一段，剪贴板里整篇文档不是翻译的用途。
func Truncate(text string, max int) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if len([]rune(trimmed)) <= max {
		return trimmed, false
	}
	if max <= 0 {
		return "", trimmed != ""
	}
	runes := []rune(trimmed)
	return strings.TrimSpace(string(runes[:max])), true
}
