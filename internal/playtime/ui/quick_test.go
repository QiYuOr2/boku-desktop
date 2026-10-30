package ui

import "testing"

func TestShortSeconds(t *testing.T) {
	tests := []struct {
		seconds int64
		want    string
	}{
		{seconds: 0, want: ""},
		{seconds: -30, want: ""},
		{seconds: 1, want: "1min"},
		{seconds: 59, want: "1min"},
		{seconds: 60, want: "1min"},
		{seconds: 30 * 60, want: "30min"},
		{seconds: 59 * 60, want: "59min"},
		{seconds: 3600, want: "1h"},
		{seconds: 3600 + 2*60, want: "1h"},
		{seconds: 3600 + 3*60, want: "1.1h"},
		{seconds: 2*3600 + 4*60, want: "2.1h"},
		{seconds: 4*3600 + 23*60, want: "4.4h"},
	}
	for _, test := range tests {
		if got := shortSeconds(test.seconds); got != test.want {
			t.Errorf("shortSeconds(%d) = %q, want %q", test.seconds, got, test.want)
		}
	}
}
