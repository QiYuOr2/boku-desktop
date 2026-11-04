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

// ParseLine reads one CrispASR --stream-json line.
//
// A line that is not one of partial, final or silence is ignored. partial.text
// is already the whole open sentence, and final.text is the only string that
// is translated.
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
