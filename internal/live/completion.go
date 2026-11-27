package live

import (
	"encoding/json"
	"strings"
)

// errNotTranslation 表示本次响应不能替换屏幕上已有的句子。
// 不记录响应体：它可能就是译文本身。
var errNotTranslation = errString("翻译没有结果")

// completionBody 构造一次对话请求：不带 system 消息，
// 因为该模型卡声明没有默认 system 提示，术语模板已是整条 user 消息。
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

// completionText 取 assistant 文本。错误文档和空响应都不算译文，此时保留屏幕上的上一句。
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
