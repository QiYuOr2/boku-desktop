package live

import (
	"encoding/json"
	"strings"
)

// errNotTranslation is a response that must not replace the sentence already
// on screen. The body is not logged: it can be the translation itself.
var errNotTranslation = errString("翻译没有结果")

// completionBody is one chat request. There is no system message: the model
// card says it has no default system prompt, and the terminology template is
// already the whole user message.
func completionBody(prompt string) ([]byte, error) {
	return json.Marshal(map[string]any{
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"temperature":        0.7,
		"top_p":              0.6,
		"top_k":              20,
		"repetition_penalty": 1.05,
		"max_tokens":         256,
	})
}

// completionText is the assistant text. An error document is not a translation,
// and neither is an empty body: the previous sentence stays on screen.
func completionText(payload []byte) (string, error) {
	var body struct {
		Error   json.RawMessage `json:"error"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return "", err
	}
	if len(body.Error) != 0 && string(body.Error) != "null" {
		return "", errNotTranslation
	}
	if len(body.Choices) == 0 {
		return "", errNotTranslation
	}
	text := strings.TrimSpace(body.Choices[0].Message.Content)
	if text == "" {
		return "", errNotTranslation
	}
	return text, nil
}
