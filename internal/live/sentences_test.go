package live

import (
	"slices"
	"testing"
)

func TestSplitSentences(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want []string
	}{
		{"Japanese", "今日は晴れです。部室に行きます！いいですか？", []string{"今日は晴れです。", "部室に行きます！", "いいですか？"}},
		{"empty", "", nil},
		{"whitespace", " \t\n\r ", nil},
		{"unfinished", "それではよろしくお願いします", []string{"それではよろしくお願いします"}},
		{"no_particle_cuts", "走ることができたので帰る", []string{"走ることができたので帰る"}},
		{"punctuation_group", "本当！？はい!!", []string{"本当！？", "はい!!"}},
		{"quoted_question", "「本当？」と聞きました。はい。", []string{"「本当？」と聞きました。", "はい。"}},
		{"quoted_apostrophe", "「It's fine?」と答えた。次です。", []string{"「It's fine?」と答えた。", "次です。"}},
		{"closing_quotes", "「こんにちは。」", []string{"「こんにちは。」"}},
		{"line_breaks", "一行目\r\n二行目\n", []string{"一行目", "二行目"}},
		{"URL_and_decimal", "example.com の 2.5 倍です。次です。", []string{"example.com の 2.5 倍です。", "次です。"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := splitSentences(tc.text); !slices.Equal(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
