package live

import "strings"

// splitSentences only runs on final text. A partial's trailing punctuation
// can move as the recognizer revises the sentence.
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
