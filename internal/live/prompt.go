package live

import "strings"

const translateInstruction = "将以下文本翻译为中文，注意**只需要输出翻译后的结果，不要额外解释**："

// Prompt is the single user message. A hit uses the model card's terminology
// template, including the bold markers. A miss uses the default template.
// The two are never combined, and there is no system message and no previous
// sentence.
func Prompt(sentence string, terms []Entry) string {
	sentence = strings.TrimSpace(sentence)
	if len(terms) == 0 {
		return translateInstruction + "\n\n" + sentence
	}
	var b strings.Builder
	b.WriteString("参考下面的翻译：\n")
	for _, term := range terms {
		b.WriteString(term.Source)
		b.WriteString(" 翻译成 ")
		b.WriteString(term.Target)
		b.WriteString("\n")
	}
	b.WriteString(translateInstruction)
	b.WriteString("\n\n")
	b.WriteString(sentence)
	return b.String()
}
