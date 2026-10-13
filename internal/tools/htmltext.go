package tools

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// htmlBlockTags are the tags that end a line of text. Everything else is
// treated as inline: a link or a styled span contributes its text and
// nothing else.
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

// htmlSkipTags are the elements whose content is markup or code rather than
// what the user read, so their text is dropped entirely.
var htmlSkipTags = map[string]bool{"script": true, "style": true, "head": true, "title": true}

// htmlNamedEntities are the entities that carry more than spelling: the
// characters a rich-text copy from a browser is full of. Anything else that
// is not numeric is left as written, which is what a plain-text conversion
// should do rather than guess.
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

// ExtractHTMLText turns copied markup into the text it shows: tags go away,
// block elements become line breaks, entities are decoded, and the result is
// collapsed to at most one blank line between paragraphs.
//
// This is what makes a copy from a browser, a word processor or a chat
// window read as the sentence the user saw rather than as its markup.
func ExtractHTMLText(markup string) string {
	if markup == "" {
		return ""
	}
	// One line-break convention inside: what a copy brought from Windows
	// then reads the same as one from anywhere else.
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
					continue // a line never starts with a space
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
			break // an unterminated comment: nothing readable follows
		}
		end := strings.IndexByte(markup[i:], '>')
		if end < 0 {
			// A `<` with no end is not a tag; keep it as text.
			writeText("<")
			i++
			continue
		}
		tag := markup[i+1 : i+end]
		i += end + 1
		name, closing, selfClosing := parseTag(tag)

		if !closing && !selfClosing && htmlSkipTags[name] {
			// Drop the element's content: it is code, not what the user
			// read.
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

// splitRuns is one piece of text as runs of the same kind: a line break, a
// run of other whitespace, or ordinary text. Treating them separately is
// what lets a space be dropped at a line start while the line itself is
// kept.
type textRun struct {
	text  string
	space bool
	line  bool
}

func splitRuns(text string) []textRun {
	runs := make([]textRun, 0, 4)
	start := 0
	kind := byte(0) // 0 text, 1 space, 2 line
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

// parseTag reads a tag's name and the two things that decide what it does:
// whether it closes an element, and whether it closes itself.
func parseTag(tag string) (name string, closing, selfClosing bool) {
	body := strings.TrimSpace(tag)
	if strings.HasPrefix(body, "/") {
		closing = true
		body = strings.TrimSpace(body[1:])
	}
	selfClosing = strings.HasSuffix(body, "/")
	body = strings.TrimSuffix(body, "/")
	name = strings.ToLower(strings.TrimSpace(body))
	// A tag may carry attributes: the name is the first word.
	if index := strings.IndexAny(name, " \t\n\r"); index >= 0 {
		name = name[:index]
	}
	return name, closing, selfClosing
}

// indexOfClosingTag returns where an element's closing tag starts, or -1.
func indexOfClosingTag(text, name string) int {
	needle := "</" + name
	lower := strings.ToLower(text)
	return strings.Index(lower, needle)
}

// collapseLines trims each line and drops the empty ones at either end,
// keeping one blank line where the markup had one.
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

// decodeHTMLEntities replaces the entities a copied document uses. An entity
// it does not know is left as it was written, which is more honest than
// guessing what it meant.
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

// decodeEntity decodes one entity name or number, without its delimiters.
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

// printableRune reports whether a code point is worth putting on the screen:
// a control character would end up in the clipboard line as an invisible
// character, and half a surrogate pair is not a character at all.
func printableRune(r rune) bool {
	if r == '\t' || r == '\n' || r == '\r' {
		return true
	}
	if r < 0x20 || r == 0x7f {
		return false
	}
	return r <= utf8.MaxRune && !(r >= 0xd800 && r <= 0xdfff)
}
