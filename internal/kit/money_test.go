package kit

import (
	"errors"
	"testing"
)

func TestParseMoney(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"42.10", 4210},
		{"42.1", 4210},
		{"42", 4200},
		{"0.01", 1},
		{"0", 0},
		{"0.00", 0},
		{"42.100", 4210},
		{"  42.10  ", 4210},
		{"007.50", 750},
		{"1234.56", 123456},
		// A value beyond float64's exact integers must survive the parse.
		{"9007199254740993.00", 900719925474099300},
	}
	for _, c := range cases {
		got, err := ParseMoney(c.in)
		if err != nil {
			t.Errorf("ParseMoney(%q) failed: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseMoney(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseMoneyRejects(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"abc",
		"-1.00",
		"+1.00",
		"1.2.3",
		"1.234",
		".",
		"1.",
		".5",
		"1,000.00",
		"1e3",
		"99999999999999999999",
		"99999999999999999999999999.99",
	}
	for _, in := range cases {
		if got, err := ParseMoney(in); err == nil {
			t.Errorf("ParseMoney(%q) = %d, want an error", in, got)
		} else if !errors.Is(err, ErrInvalidMoney) {
			t.Errorf("ParseMoney(%q) error %v does not wrap ErrInvalidMoney", in, err)
		}
	}
}

func TestFormatMoney(t *testing.T) {
	cases := []struct {
		minor     int64
		currency  string
		precision int
		want      string
	}{
		{4210, "CNY", 2, "¥42.10"},
		{0, "CNY", 2, "¥0.00"},
		{5, "CNY", 2, "¥0.05"},
		{-123, "CNY", 2, "-¥1.23"},
		{123456, "USD", 2, "$1234.56"},
		{1234, "JPY", 0, "¥1234"},
		{840, "XYZ", 2, "8.40 XYZ"},
		{10, "", 2, "0.10"},
	}
	for _, c := range cases {
		if got := FormatMoney(c.minor, c.currency, c.precision); got != c.want {
			t.Errorf("FormatMoney(%d, %q, %d) = %q, want %q", c.minor, c.currency, c.precision, got, c.want)
		}
	}
}
