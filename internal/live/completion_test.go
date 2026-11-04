package live

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompletionBodyIsOneUserMessage(t *testing.T) {
	raw, err := completionBody("将以下文本翻译为中文，注意**只需要输出翻译后的结果，不要额外解释**：\n\nこんにちは")
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Temperature       float64 `json:"temperature"`
		TopP              float64 `json:"top_p"`
		TopK              int     `json:"top_k"`
		RepetitionPenalty float64 `json:"repetition_penalty"`
		MaxTokens         int     `json:"max_tokens"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Messages) != 1 || body.Messages[0].Role != "user" || !strings.Contains(body.Messages[0].Content, "こんにちは") {
		t.Fatalf("%s", raw)
	}
	if body.Temperature != 0.7 || body.TopP != 0.6 || body.TopK != 20 || body.RepetitionPenalty != 1.05 || body.MaxTokens != 256 {
		t.Fatalf("%s", raw)
	}
	if strings.Contains(string(raw), `"system"`) {
		t.Fatalf("system message in %s", raw)
	}
}

func TestCompletionTextIgnoresAnErrorDocument(t *testing.T) {
	if _, err := completionText([]byte(`{"error":{"message":"failed"}}`)); err == nil {
		t.Fatal("error json was accepted")
	}
	if _, err := completionText([]byte(`{"choices":[{"message":{"content":"  "}}]}`)); err == nil {
		t.Fatal("empty content was accepted")
	}
	text, err := completionText([]byte(`{"choices":[{"message":{"role":"assistant","content":"你好"}}]}`))
	if err != nil || text != "你好" {
		t.Fatalf("%q %v", text, err)
	}
}
