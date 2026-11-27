package kit

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// htmlBlockTags 是结束一行的标签。其余标签都按内联处理：
// 链接或带样式的 span 只贡献其文本。
var htmlBlockTags = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true,
	"caption": true, "dd": true, "details": true, "div": true, "dl": true,
	"dt": true, "fieldset": true, "figcaption": true, "figure": true,
	"footer": true, "form": true, "h1": true, "h2": true, "h3": true,
	"h4": true, "h5": true, "h6": true, "header": true, "hr": true,
	"li": true, "main": true, "nav": true, "ol": true, "p": true,
	"pre": true, "section": true, "table": true, "tbody": true,
	"td": true, "tfoot": true, "th": true, "thead": true, "tr": true,
	"ul": true, "br": true,
}

// htmlSkipTags 是内容为标记或代码、而非用户所读的元素，其文本整段丢弃。
var htmlSkipTags = map[string]bool{"script": true, "style": true, "head": true, "title": true}

// htmlNamedEntities 是带有超过拼写含义的实体：浏览器富文本复制里常见的字符。
// 非数字且不在表内的实体按原样保留，这是纯文本转换应有的做法，而非猜测。
var htmlNamedEntities = map[string]string{
	"amp": "&", "lt": "<", "gt": ">", "quot": "\"", "apos": "'",
	"nbsp": " ", "ensp": " ", "emsp": " ", "thinsp": " ",
	"copy": "©", "reg": "®", "trade": "™", "hellip": "…",
	"mdash": "—", "ndash": "–", "middot": "·", "bull": "•",
	"laquo": "«", "raquo": "»", "ldquo": "“", "rdquo": "”",
	"lsquo": "‘", "rsquo": "’", "deg": "°", "times": "×",
	"divide": "÷", "plusmn": "±", "micro": "µ", "euro": "€",
	"pound": "£", "yen": "¥", "cent": "¢", "sect": "§",
	"para": "¶", "dagger": "†", "Dagger": "‡", "permil": "‰",
	"frac12": "½", "frac14": "¼", "frac34": "¾", "sup2": "²",
	"sup3": "³", "larr": "←", "rarr": "→", "uarr": "↑", "darr": "↓",
	"harr": "↔", "ne": "≠", "le": "≤", "ge": "≥", "infin": "∞",
	"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ", "pi": "π",
	"omega": "ω", "lambda": "λ", "mu": "µ", "sigma": "σ", "tau": "τ",
}

// ExtractHTMLText 把复制的标记转成它显示的文字：去掉标签，块级元素变成换行，
// 解码实体，并把段间空行压到最多一个。
// 这样从浏览器、文字处理器或聊天窗口复制的内容读起来就是用户看到的句子，
// 而不是它的标记。
func ExtractHTMLText(markup string) string {
	if markup == "" {
		return ""
	}
	// 内部统一为一种换行约定：这样从 Windows 复制来的内容与别处复制来的读取一致。
	if strings.ContainsRune(markup, '\r') {
		markup = strings.ReplaceAll(markup, "\r\n", "\n")
		markup = strings.ReplaceAll(markup, "\r", "\n")
	}
	var out strings.Builder
	out.Grow(len(markup))

	pendingBreak := false
	lineHasText := false
	breakLine := func() {
		if !lineHasText {
			return
		}
		out.WriteByte('\n')
		lineHasText = false
	}
	writeText := func(text string) {
		if text == "" {
			return
		}
		if pendingBreak {
			breakLine()
			pendingBreak = false
		}
		for _, part := range splitRuns(text) {
			switch {
			case part.line:
				breakLine()
			case part.space:
				if !lineHasText {
					continue // 行首不放空格
				}
				out.WriteByte(' ')
			default:
				out.WriteString(part.text)
				lineHasText = true
			}
		}
	}

	for i := 0; i < len(markup); {
		next := strings.IndexByte(markup[i:], '<')
		if next < 0 {
			writeText(decodeHTMLEntities(markup[i:]))
			break
		}
		writeText(decodeHTMLEntities(markup[i : i+next]))
		i += next

		if strings.HasPrefix(markup[i:], "<!--") {
			if end := strings.Index(markup[i+4:], "-->"); end >= 0 {
				i += 4 + end + 3
				continue
			}
			break // 未闭合的注释：后面再无可读内容
		}
		end := strings.IndexByte(markup[i:], '>')
		if end < 0 {
			// 没有结尾的 `<` 不是标签，按文本保留。
			writeText("<")
			i++
			continue
		}
		tag := markup[i+1 : i+end]
		i += end + 1
		name, closing, selfClosing := parseTag(tag)

		if !closing && !selfClosing && htmlSkipTags[name] {
			// 丢弃元素内容：它是代码，不是用户所读的内容。
			if close := indexOfClosingTag(markup[i:], name); close >= 0 {
				skip := markup[i+close:]
				if stop := strings.IndexByte(skip, '>'); stop >= 0 {
					i += close + stop + 1
				} else {
					i = len(markup)
				}
			} else {
				i = len(markup)
			}
			pendingBreak = true
			continue
		}
		switch {
		case name == "br" || (name == "hr" && !closing):
			breakLine()
			pendingBreak = false
		case htmlBlockTags[name] && (closing || selfClosing):
			pendingBreak = true
		}
	}

	return collapseLines(out.String())
}

// splitRuns 把一段文本拆成同类 run 的片段：一个换行、一段其他空白，或普通文本。
// 分开处理才能做到行首丢弃空格而保留行本身。
type textRun struct {
	text  string
	space bool
	line  bool
}

func splitRuns(text string) []textRun {
	runs := make([]textRun, 0, 4)
	start := 0
	kind := byte(0) // 0 文本，1 空白，2 换行
	kindOf := func(r rune) byte {
		switch r {
		case '\n', '\r', '\v', '\f':
			return 2
		case ' ', '\t':
			return 1
		}
		if r == '\u00a0' || r == '\u2009' || r == '\u202f' || r == '\u3000' {
			return 1
		}
		return 0
	}
	for index, r := range text {
		current := kindOf(r)
		if current == kind {
			continue
		}
		if index > start {
			runs = append(runs, textRun{text: text[start:index], space: kind == 1, line: kind == 2})
		}
		start, kind = index, current
	}
	if start < len(text) {
		runs = append(runs, textRun{text: text[start:], space: kind == 1, line: kind == 2})
	}
	return runs
}

// parseTag 读取标签名以及决定其行为的两个标志：是否闭合元素、是否自闭合。
func parseTag(tag string) (name string, closing, selfClosing bool) {
	body := strings.TrimSpace(tag)
	if strings.HasPrefix(body, "/") {
		closing = true
		body = strings.TrimSpace(body[1:])
	}
	selfClosing = strings.HasSuffix(body, "/")
	body = strings.TrimSuffix(body, "/")
	name = strings.ToLower(strings.TrimSpace(body))
	// 标签可能带属性：名字是第一个词。
	if index := strings.IndexAny(name, " \t\n\r"); index >= 0 {
		name = name[:index]
	}
	return name, closing, selfClosing
}

// indexOfClosingTag 返回元素闭合标签的起始位置，没有则返回 -1。
func indexOfClosingTag(text, name string) int {
	needle := "</" + name
	lower := strings.ToLower(text)
	return strings.Index(lower, needle)
}

// collapseLines 逐行去空白，去掉首尾空行，并在标记原本有空行处保留一个空行。
func collapseLines(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, strings.TrimSpace(line))
	}
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

// decodeHTMLEntities 替换复制文档里用到的实体。不认识的实体按原样保留，
// 这比猜测其含义更诚实。
func decodeHTMLEntities(text string) string {
	if !strings.Contains(text, "&") {
		return text
	}
	var out strings.Builder
	out.Grow(len(text))
	for i := 0; i < len(text); {
		if text[i] != '&' {
			out.WriteByte(text[i])
			i++
			continue
		}
		end := strings.IndexByte(text[i:], ';')
		if end < 0 || end > 32 {
			out.WriteByte(text[i])
			i++
			continue
		}
		entity := text[i+1 : i+end]
		if decoded, ok := decodeEntity(entity); ok {
			out.WriteString(decoded)
			i += end + 1
			continue
		}
		out.WriteByte(text[i])
		i++
	}
	return out.String()
}

// decodeEntity 解码单个实体名或数字（不含分隔符）。
func decodeEntity(entity string) (string, bool) {
	if entity == "" {
		return "", false
	}
	if entity[0] == '#' {
		digits, base := entity[1:], 10
		if len(digits) > 0 && (digits[0] == 'x' || digits[0] == 'X') {
			digits, base = digits[1:], 16
		}
		code, err := strconv.ParseInt(digits, base, 32)
		if err != nil || !printableRune(rune(code)) {
			return "", false
		}
		return string(rune(code)), true
	}
	decoded, ok := htmlNamedEntities[entity]
	return decoded, ok
}

// printableRune 判断一个码点是否值得上屏：控制字符会在剪贴板行里变成不可见字符，
// 半个代理对也根本不是字符。
func printableRune(r rune) bool {
	if r == '\t' || r == '\n' || r == '\r' {
		return true
	}
	if r < 0x20 || r == 0x7f {
		return false
	}
	return r <= utf8.MaxRune && !(r >= 0xd800 && r <= 0xdfff)
}
