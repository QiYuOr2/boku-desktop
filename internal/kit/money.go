// Package kit 存放应用的纯函数：不涉及窗口、网络、数据库、MyGo 或文件。
//
// 唯一允许的依赖是 internal/domain，用于获取定义值含义的枚举与界限。该包本身是叶子，
// 因此这里无法通过它接触外部世界。
package kit

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// MoneyPrecision 是货币最小单位的十进制位数：金额以最小单位的整数存储，¥42.10 即 4210。
const MoneyPrecision = 2

// ErrInvalidMoney 是所有 ParseMoney 失败所包装的错误。
var ErrInvalidMoney = errors.New("invalid money amount")

// ParseMoney 解析十进制金额文本，返回最小单位。
//
// 全程不经过浮点数，保证精确。此处的金额不带符号（应用读取的接口报告的是余额而非债务）。
// "42.100" 仅在多余位为零时接受，任何非零的第三位小数都算精度错误，而不是四舍五入。
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

// FormatMoney 把最小单位格式化为金额：有符号的货币用符号，否则用货币代码。
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

// CurrencySymbol 返回货币符号；无可用符号时返回空串。
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
