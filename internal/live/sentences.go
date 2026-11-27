package live

import "strings"

// splitSentences 只处理最终文本：识别器修正句子时，partial（未定稿）末尾的标点
// 会移动。
func splitSentences(text string) []string {
	var sentences []string
	start := 0
	ended := false
	depth := 0
	for at, char := range text {
		terminal := depth == 0 && strings.ContainsRune("。！？!?\n\r", char)
		closing := strings.ContainsRune("」』】）〕〉》”’\"'", char)
		if ended && !terminal && !closing {
			if sentence := strings.TrimSpace(text[start:at]); sentence != "" {
				sentences = append(sentences, sentence)
			}
			start = at
			ended = false
		}
		if strings.ContainsRune("「『【（〔〈《“‘", char) {
			depth++
		} else if strings.ContainsRune("」』】）〕〉》”’", char) && depth > 0 {
			depth--
		}
		ended = ended || terminal
	}
	if sentence := strings.TrimSpace(text[start:]); sentence != "" {
		sentences = append(sentences, sentence)
	}
	return sentences
}
