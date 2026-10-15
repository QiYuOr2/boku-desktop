package openai

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"boku/internal/domain"
)

// The Japanese settings ask the translator for more than a translation: each
// word of the source, how it is read, and — for a loanword written in
// katakana — the word it came from. The model answers with one JSON object
// per line, and this file is what turns that answer into segments.
//
// The parser is deliberately forgiving. A model's output is not a contract:
// it may wrap the lines in a code fence, spell a field out in full, forget
// one, or answer the translation alone. None of that is a failure — the
// window can always draw what came back — so nothing here returns an error.
// What it cannot read as a segment becomes plain text, and if it reads
// nothing at all, the whole answer is the translation.

// AnnotationOptions is what to ask for. It is the domain's own value: the
// choice belongs to the user's settings, and the request carries it from
// there to here.
type AnnotationOptions = domain.AnnotationOptions

// SystemPrompt is the instruction that decides the shape of the answer: a
// plain translation when nothing more was asked for, and the line-by-line
// annotation when the Japanese settings want it.
//
// The prompt names the languages in the interface's own words: a model is
// better at "翻译成中文" than at "translate into zh".
//
// Two of the requirements are the user's, and both are asked for in words a
// model cannot read as optional — a katakana loanword must carry the word it
// came from (モード is "mode", and a Japanese speaker reading 模式 wants to see
// that), and the translation must keep the source's own marks: a source that
// says 【回想モード完備】 is answered with 【完备回想模式】, brackets and all.
// The example is those two rules at once, and it is also what tells a model
// that punctuation gets a piece of its own, since that is how the brackets
// survive into the pieces.
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

// annotationExample is the prompt's example: one line per piece of
// 【回想モード完備】, with the optional fields only when they were asked for.
//
// It is the three rules at once, which is why it is worth more than a
// sentence of instructions: 回想 is one piece with one reading rather than two
// characters with two, モード carries the word it came from and no reading of
// its own, and the brackets are pieces that translate to themselves.
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

// readingFor is the reading a piece should carry: one that belongs to it.
//
// A reading is for kanji — that is what the setting is called, and what the
// design draws above the word. A model that annotates every piece writes
// katakana above は and above モード: the first is a reading nobody needs, and
// the second is the word itself, which says nothing. Neither is dropped from
// the answer as a mistake — the answer is still a translation — but the window
// draws the annotation the user asked for, whatever the model sent.
func readingFor(text, reading string) string {
	if reading == "" || !hasHan(text) {
		// Kana, punctuation, digits: there is nothing to read. That covers a
		// loanword too, whose katakana is already its reading.
		return ""
	}
	return reading
}

// glossFor is the gloss a piece should carry: the word a loanword came from,
// which only a piece written in katakana has.
func glossFor(text, gloss string) string {
	if gloss == "" || !hasKatakana(text) {
		return ""
	}
	return gloss
}

// hasHan reports whether any of the text is a kanji.
func hasHan(text string) bool {
	for _, r := range text {
		if unicode.In(r, unicode.Han) {
			return true
		}
	}
	return false
}

// looksLikeData reports whether a piece of an answer is the answer's own
// format rather than something to read: a JSON object or array, whole or
// broken, on its own or spread over the line.
//
// It exists for one rule: what the window shows is the translation. A model
// that answers badly must not put `{"s":"\n","t":"\n"}` in front of the user —
// the line is not a translation, and the window says what it says about an
// answer it could not use instead.
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

// hasKatakana reports whether any of the text is katakana.
func hasKatakana(text string) bool {
	for _, r := range text {
		if unicode.In(r, unicode.Katakana) {
			return true
		}
	}
	return false
}

// Annotation is what was read out of an answer: the segments, the
// translation they add up to, and whether the translation could be cut into
// segments at all.
type Annotation struct {
	Segments []domain.Segment
	// TranslatedText is the segments' own translations, joined in the order
	// they were given; for an answer that could not be segmented it is the
	// whole answer as it came.
	TranslatedText string
	// Segmented reports whether the answer could be read as pieces. It is
	// false for a plain translation, and the window then draws the source
	// as one line.
	Segmented bool
	// UnreadableLines is how many lines were not a JSON object. Those lines
	// are still shown — as plain text — but the caller may say that the
	// answer was not understood in full. It is 0 for an answer that read as
	// segments throughout, and for one that was never meant to be segmented.
	UnreadableLines int
}

// ParseAnnotation reads an annotated answer.
//
// It never fails: an answer that is not a list of JSON objects is a plain
// translation, which is a thing the window can draw.
func ParseAnnotation(content string) Annotation {
	trimmed := strings.TrimSpace(content)
	lines := answerLines(trimmed)
	// A model that answers one JSON array is not following the prompt, but it
	// is saying the same thing: read the array's own elements as the lines.
	// Without this, a compact array on one line is one unreadable line, and
	// an annotated answer the user asked for is drawn as a plain translation.
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
		// dropped says the answer carried pieces of the service's own format
		// that could not be read. What they were is not shown, and the answer
		// is then the readable lines rather than the raw one.
		dropped bool
	)
	for _, line := range lines {
		var parsed struct {
			Text    string `json:"s"`
			Reading string `json:"r"`
			Gloss   string `json:"g"`
			Target  string `json:"t"`
			// The spelled-out names, for a model that ignores the prompt's
			// field names and writes its own.
			TextLong    string `json:"text"`
			ReadingLong string `json:"reading"`
			GlossLong   string `json:"gloss"`
			TargetLong  string `json:"translation"`
		}
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			// A line that is the service's own format and nothing else is
			// not shown: the window shows the translation, and a user must
			// never be handed the wire format because a model malformed it.
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
			// An object that names no source piece is not a segment: there
			// is nothing to draw it under. Its translation is not dropped
			// with it, when it has one.
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
			// The annotations that belong to the piece: a reading for a word
			// with kanji in it, a gloss for one written in katakana. What a
			// model puts where it does not belong is left out here rather
			// than drawn.
			Reading: readingFor(text, firstNonEmpty(parsed.Reading, parsed.ReadingLong)),
			Gloss:   glossFor(text, firstNonEmpty(parsed.Gloss, parsed.GlossLong)),
			Target:  target,
		})
		if target != "" {
			translated = append(translated, target)
		} else {
			// A piece the model annotated but did not translate: the piece
			// itself stands in for its translation rather than leaving a
			// hole in the paragraph.
			translated = append(translated, text)
		}
	}
	if len(segments) == 0 {
		// Nothing in the answer was an object it could read. An answer that
		// carried the service's own format anyway has braces in it that are
		// not a translation: what was readable is the answer, and the rest is
		// dropped rather than drawn.
		if dropped || looksLikeData(trimmed) {
			return Annotation{TranslatedText: joinTranslated(translated), UnreadableLines: unreadable}
		}
		// Otherwise it is the translation itself, however it was wrapped.
		return Annotation{TranslatedText: trimmed}
	}
	// Latin scripts separate their words, and the joining puts a space back
	// between two pieces that were written apart. Han, kana, Hangul and
	// punctuation are written without spaces, and get none.
	return Annotation{
		Segments:        segments,
		TranslatedText:  joinTranslated(translated),
		Segmented:       true,
		UnreadableLines: unreadable,
	}
}

// joinTranslated puts the pieces of a translation back together.
//
// It cannot know where the model's own spaces were, so it uses one rule:
// a space goes between two pieces only when both sides are written in a
// script that separates its words. Joining a Japanese answer that way leaves
// it exactly as it was written; joining a Latin one keeps its words apart.
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

// separatesWords reports whether a script writes its words apart, which is
// what decides whether a space belongs between two pieces.
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

// answerLines splits an answer into the lines a person would read: fenced
// code is unwrapped, and blank lines and the punctuation of an array are
// dropped.
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

// firstNonEmpty returns the first of the values with anything in it.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// answerArray reads an answer that is one JSON array, as one piece of JSON per
// element. The pieces are read exactly as a line of the answer would be, so an
// array and a column of objects come out the same.
//
// It reports false when the answer is not one array, which leaves it to the
// line-by-line reading the prompt asks for.
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

// AnnotationNote is what the window should say about an annotated answer: how
// many of its lines could not be read, and — when the settings asked for
// readings and the answer came back with no pieces at all — that there are
// none. The second one is what keeps a missing annotation from looking like a
// translation that simply never had any.
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

// JoinSegments is the source text a list of segments adds up to. The window
// draws the source from the pieces rather than from what was sent, so that
// what is highlighted and what is read are the same thing.
func JoinSegments(segments []domain.Segment) string {
	pieces := make([]string, 0, len(segments))
	for _, segment := range segments {
		if text := strings.TrimSpace(segment.Text); text != "" {
			pieces = append(pieces, text)
		}
	}
	return joinTranslated(pieces)
}

// Truncate cuts text to at most max characters, on a character boundary, and
// reports whether it had to. It is the tool's own ceiling: a selection is a
// sentence or a paragraph, and a whole document in the clipboard is not what
// translation is for.
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
