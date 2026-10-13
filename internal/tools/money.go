// Package tools holds the application's pure functions: no window, no
// network, no database, no MyGo. Everything here can be tested by calling
// it, which is why the arithmetic the application depends on lives here.
package tools

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// MoneyPrecision is how many decimal places a currency's minor unit has: an
// amount is stored as a whole number of them. ¥42.10 is 4210.
const MoneyPrecision = 2

// ErrInvalidMoney is what every ParseMoney failure wraps.
var ErrInvalidMoney = errors.New("invalid money amount")

// ParseMoney reads a decimal amount as text and returns it in minor units.
//
// The amount never passes through a float: "42.10" becomes exactly 4210,
// however large it grows. Amounts are not signed here — the interfaces the
// application reads report balances, not debts — and "42.100" is accepted
// only because its extra digits are zero; any other third decimal is a
// precision error rather than a rounding.
func ParseMoney(s string) (int64, error) {
	text := strings.TrimSpace(s)
	if text == "" {
		return 0, fmt.Errorf("%w: it is empty", ErrInvalidMoney)
	}
	whole, fraction, hasPoint := strings.Cut(text, ".")
	if !isDigits(whole) {
		return 0, fmt.Errorf("%w: %q is not a number", ErrInvalidMoney, text)
	}
	if hasPoint {
		if !isDigits(fraction) {
			return 0, fmt.Errorf("%w: %q is not a number", ErrInvalidMoney, text)
		}
		if len(fraction) > MoneyPrecision {
			if strings.Trim(fraction[MoneyPrecision:], "0") != "" {
				return 0, fmt.Errorf("%w: %q has more than %d decimal places", ErrInvalidMoney, text, MoneyPrecision)
			}
			fraction = fraction[:MoneyPrecision]
		}
	}
	units, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q is out of range", ErrInvalidMoney, text)
	}
	cents := int64(0)
	if fraction != "" {
		padded := fraction + strings.Repeat("0", MoneyPrecision-len(fraction))
		cents, err = strconv.ParseInt(padded, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%w: %q is out of range", ErrInvalidMoney, text)
		}
	}
	if units > (math.MaxInt64-cents)/100 {
		return 0, fmt.Errorf("%w: %q is out of range", ErrInvalidMoney, text)
	}
	return units*100 + cents, nil
}

// FormatMoney shows minor units as an amount, with the currency's symbol
// when it has one and its code otherwise: 4210, "CNY" and 2 give "¥42.10",
// and 840, "XYZ" and 2 give "8.40 XYZ".
func FormatMoney(minor int64, currency string, precision int) string {
	if precision < 0 {
		precision = MoneyPrecision
	}
	negative := minor < 0
	digits := strconv.FormatInt(abs(minor), 10)
	for len(digits) <= precision {
		digits = "0" + digits
	}
	whole := digits[:len(digits)-precision]
	number := whole
	if precision > 0 {
		number += "." + digits[len(digits)-precision:]
	}
	sign := ""
	if negative {
		sign = "-"
	}
	symbol := CurrencySymbol(currency)
	switch {
	case symbol != "":
		return sign + symbol + number
	case currency != "":
		return sign + number + " " + currency
	default:
		return sign + number
	}
}

// CurrencySymbol returns the sign of a currency, or "" when it has none
// worth guessing.
func CurrencySymbol(currency string) string {
	switch strings.ToUpper(strings.TrimSpace(currency)) {
	case "CNY", "RMB", "JPY":
		return "¥"
	case "USD":
		return "$"
	case "EUR":
		return "€"
	case "GBP":
		return "£"
	case "KRW":
		return "₩"
	case "HKD":
		return "HK$"
	case "TWD":
		return "NT$"
	case "SGD", "CAD", "AUD":
		return "S$"
	default:
		return ""
	}
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
