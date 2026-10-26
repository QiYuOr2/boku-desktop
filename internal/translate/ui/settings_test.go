package ui

import "testing"

func TestIsFreeTranslator(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"free.microsoft", true},
		{"free.bilibili", true},
		{"deepseek", false},
		{"custom", false},
		{"custom-2", false},
		{"openai", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			if got := isFreeTranslator(tc.id); got != tc.want {
				t.Errorf("isFreeTranslator(%q) = %v, want %v", tc.id, got, tc.want)
			}
		})
	}
}
