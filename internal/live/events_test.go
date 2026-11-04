package live

import "testing"

func TestParseLineKeepsTheEventText(t *testing.T) {
	kind, text, ok := ParseLine(`{"type":"partial","utterance_id":1,"text":"こんにちは","t0":1}`)
	if !ok || kind != "partial" || text != "こんにちは" {
		t.Fatalf("%s %q %v", kind, text, ok)
	}
	kind, text, ok = ParseLine(`{"type":"final","text":"こんにちは。"}`)
	if !ok || kind != "final" || text != "こんにちは。" {
		t.Fatalf("final %s %q", kind, text)
	}
	kind, _, ok = ParseLine(`{"type":"silence","t":1.2}`)
	if !ok || kind != "silence" {
		t.Fatalf("silence %s %v", kind, ok)
	}
}

func TestParseLineDropsAnythingElse(t *testing.T) {
	for _, line := range []string{"", "loading model", "{", `{"type":"word","text":"x"}`, `{"text":"x"}`} {
		if _, _, ok := ParseLine(line); ok {
			t.Fatalf("accepted %q", line)
		}
	}
}
