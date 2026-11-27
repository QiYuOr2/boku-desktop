package live

import "strings"

const translateInstruction = "将以下文本翻译为中文，注意**只需要输出翻译后的结果，不要额外解释**："

// Prompt 生成唯一的 user 消息。命中术语时用模型卡片的术语模板（含粗体标记），
// 未命中用默认模板；两者永不合并，也没有 system 消息和上一句上下文。
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
