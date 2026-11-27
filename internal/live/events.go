package live

import (
	"encoding/json"
	"strings"
)

const (
	eventPartial = "partial"
	eventFinal   = "final"
	eventSilence = "silence"
)

// ParseLine 解析一行 CrispASR --stream-json 输出。
//
// 不是 partial、final 或 silence 的行一律忽略。partial.text 已是当前完整句子，
// 而 final.text 是唯一会被翻译的字符串。
func ParseLine(line string) (kind, text string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || line[0] != '{' {
		return "", "", false
	}
	var event struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return "", "", false
	}
	switch event.Type {
	case eventPartial, eventFinal, eventSilence:
		return event.Type, event.Text, true
	default:
		return "", "", false
	}
}
