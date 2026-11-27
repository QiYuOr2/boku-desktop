package diary

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// InlineRun 是一段带内联样式的文本，供渲染包含 Apple 日记 <c color> / <s size> 样式标签的段落。
type InlineRun struct {
	Text    string
	Bold    bool
	Italic  bool
	Strike  bool
	Code    bool
	LinkURL string
	Color   string // "", "gray", "red", "blue", "green", "orange", "purple"
	Size    string // "", "small", "large"
}

var (
	mdImageRe    = regexp.MustCompile(`!\[([^\]]*)\]\(\s*([^\s\)]+)(?:\s+"[^"]*")?\s*\)`)
	mdLinkRe     = regexp.MustCompile(`\[([^\]]+)\]\(\s*([^\s\)]+)(?:\s+"[^"]*")?\s*\)`)
	mdEmphasisRe = regexp.MustCompile(`(\*\*|__|\*|_|~~|` + "`" + `)`)
	mdListPrefRe = regexp.MustCompile(`^\s*(?:[-*+]|\d+\.)\s+`)
	mdSpaceRe    = regexp.MustCompile(`\s+`)
	styleTagRe   = regexp.MustCompile(`<(/?)([cs])(?:\s+([a-z]+))?>`)
)

var validStyleColors = map[string]bool{
	"gray": true, "red": true, "blue": true, "green": true, "orange": true, "purple": true,
}

var validStyleSizes = map[string]bool{
	"small": true, "large": true,
}

// Summary 生成日记卡片的纯文本摘要：跳过代码块、剥离 Markdown 与 <c>/<s> 样式标签。
func Summary(markdown string, limit int) string {
	if limit <= 0 {
		limit = 180
	}
	var visible []string
	inFence := false
	for _, raw := range strings.Split(NormalizeLineEndings(markdown), "\n") {
		trimmed := strings.TrimSpace(raw)
		if isFence(trimmed) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		visible = append(visible, raw)
	}
	text := strings.Join(visible, "\n")
	text = mdImageRe.ReplaceAllString(text, "$1")
	text = mdLinkRe.ReplaceAllString(text, "$1")
	text = mdEmphasisRe.ReplaceAllString(text, "")
	text = StripStyleTags(text)

	var lines []string
	for _, raw := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		line := trimmed
		if strings.HasPrefix(line, "#") {
			line = strings.TrimSpace(strings.TrimLeft(line, "#"))
		}
		if strings.HasPrefix(line, ">") {
			line = strings.TrimSpace(strings.TrimPrefix(line, ">"))
		}
		line = mdListPrefRe.ReplaceAllString(line, "")
		if line != "" {
			lines = append(lines, line)
		}
	}
	joined := strings.TrimSpace(mdSpaceRe.ReplaceAllString(strings.Join(lines, " "), " "))
	runes := []rune(joined)
	if len(runes) <= limit {
		return joined
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
}

// HasStyleTags 报告文本中是否包含至少一对合法的 <c color>...</c> 或 <s size>...</s> 标签。
func HasStyleTags(text string) bool {
	return len(matchStylePairs(text)) > 0
}

// StripStyleTags 移除成对合法的 <c color>...</c> 与 <s size></s>，未闭合或未知标签保留原文。
func StripStyleTags(text string) string {
	pairs := matchStylePairs(text)
	if len(pairs) == 0 {
		return text
	}
	skip := make([]bool, len(text))
	for _, p := range pairs {
		for i := p.openStart; i < p.openEnd; i++ {
			skip[i] = true
		}
		for i := p.closeStart; i < p.closeEnd; i++ {
			skip[i] = true
		}
	}
	var b strings.Builder
	b.Grow(len(text))
	for i := 0; i < len(text); i++ {
		if !skip[i] {
			b.WriteByte(text[i])
		}
	}
	return b.String()
}

// ParseInline 解析行内 Markdown（粗体、斜体、删除线、代码、链接）以及日记专用的 <c>/<s> 样式标签。
func ParseInline(text string) []InlineRun {
	return parseInlineStyled(text, InlineRun{})
}

func parseInlineStyled(text string, base InlineRun) []InlineRun {
	if text == "" {
		return nil
	}
	pairs := matchStylePairs(text)
	if len(pairs) > 0 {
		outer := outermostPairs(pairs)
		var runs []InlineRun
		pos := 0
		for _, p := range outer {
			if p.openStart > pos {
				runs = append(runs, parseMarkdownInline(text[pos:p.openStart], base)...)
			}
			inner := base
			if p.family == 'c' {
				inner.Color = p.arg
			} else {
				inner.Size = p.arg
			}
			runs = append(runs, parseInlineStyled(text[p.openEnd:p.closeStart], inner)...)
			pos = p.closeEnd
		}
		if pos < len(text) {
			runs = append(runs, parseMarkdownInline(text[pos:], base)...)
		}
		return runs
	}
	return parseMarkdownInline(text, base)
}

func parseMarkdownInline(text string, base InlineRun) []InlineRun {
	var runs []InlineRun
	var plain strings.Builder
	flushPlain := func() {
		if plain.Len() == 0 {
			return
		}
		r := base
		r.Text = plain.String()
		runs = append(runs, r)
		plain.Reset()
	}

	i := 0
	for i < len(text) {
		if text[i] == '`' {
			if end := strings.IndexByte(text[i+1:], '`'); end > 0 {
				flushPlain()
				r := base
				r.Text = text[i+1 : i+1+end]
				r.Code = true
				runs = append(runs, r)
				i += end + 2
				continue
			}
		}
		if text[i] == '[' {
			if loc := mdLinkRe.FindStringSubmatchIndex(text[i:]); loc != nil && loc[0] == 0 {
				label := text[i+loc[2] : i+loc[3]]
				rawURL := text[i+loc[4] : i+loc[5]]
				if clean, ok := ValidateWebURL(rawURL); ok {
					flushPlain()
					r := base
					r.Text = label
					r.LinkURL = clean
					runs = append(runs, r)
					i += loc[1]
					continue
				}
			}
		}
		if strings.HasPrefix(text[i:], "**") {
			if end := strings.Index(text[i+2:], "**"); end > 0 {
				flushPlain()
				inner := base
				inner.Bold = true
				runs = append(runs, parseMarkdownInline(text[i+2:i+2+end], inner)...)
				i += end + 4
				continue
			}
		}
		if strings.HasPrefix(text[i:], "~~") {
			if end := strings.Index(text[i+2:], "~~"); end > 0 {
				flushPlain()
				inner := base
				inner.Strike = true
				runs = append(runs, parseMarkdownInline(text[i+2:i+2+end], inner)...)
				i += end + 4
				continue
			}
		}
		if text[i] == '*' && !strings.HasPrefix(text[i:], "**") && i+1 < len(text) && text[i+1] != ' ' {
			if end := strings.IndexByte(text[i+1:], '*'); end > 0 && text[i+end] != ' ' {
				flushPlain()
				inner := base
				inner.Italic = true
				runs = append(runs, parseMarkdownInline(text[i+1:i+1+end], inner)...)
				i += end + 2
				continue
			}
		}
		_, size := utf8.DecodeRuneInString(text[i:])
		plain.WriteString(text[i : i+size])
		i += size
	}
	flushPlain()
	return runs
}

// FormatCommand 是编辑器格式工具栏支持的快捷格式命令。
type FormatCommand string

const (
	FormatBold    FormatCommand = "bold"
	FormatItalic  FormatCommand = "italic"
	FormatStrike  FormatCommand = "strike"
	FormatCode    FormatCommand = "code"
	FormatHeading FormatCommand = "heading"
	FormatBullet  FormatCommand = "bullet"
	FormatQuote   FormatCommand = "quote"
)

func FormatColor(name string) FormatCommand { return FormatCommand("color:" + name) }
func FormatSize(name string) FormatCommand  { return FormatCommand("size:" + name) }

// ApplyFormat 在按 rune 索引表示的选区 [start, end) 上套用格式，返回新文本与新选区。
func ApplyFormat(cmd FormatCommand, text string, start, end int) (string, int, int) {
	runes := []rune(text)
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if end > len(runes) {
		end = len(runes)
	}
	if start > len(runes) {
		start = len(runes)
	}

	wrap := func(open, closeTag, placeholder string) (string, int, int) {
		selected := string(runes[start:end])
		inner := selected
		if inner == "" {
			inner = placeholder
		}
		inserted := open + inner + closeTag
		out := string(runes[:start]) + inserted + string(runes[end:])
		openLen := utf8.RuneCountInString(open)
		innerLen := utf8.RuneCountInString(inner)
		return out, start + openLen, start + openLen + innerLen
	}

	prefixLine := func(prefix string) (string, int, int) {
		lineStart := start
		for lineStart > 0 && runes[lineStart-1] != '\n' {
			lineStart--
		}
		out := string(runes[:lineStart]) + prefix + string(runes[lineStart:])
		shift := utf8.RuneCountInString(prefix)
		return out, start + shift, end + shift
	}

	switch cmd {
	case FormatBold:
		return wrap("**", "**", "粗体")
	case FormatItalic:
		return wrap("*", "*", "斜体")
	case FormatStrike:
		return wrap("~~", "~~", "删除线")
	case FormatCode:
		return wrap("`", "`", "代码")
	case FormatHeading:
		return prefixLine("## ")
	case FormatBullet:
		return prefixLine("- ")
	case FormatQuote:
		return prefixLine("> ")
	}
	if color, ok := strings.CutPrefix(string(cmd), "color:"); ok && validStyleColors[color] {
		return wrap("<c "+color+">", "</c>", "文字")
	}
	if size, ok := strings.CutPrefix(string(cmd), "size:"); ok && validStyleSizes[size] {
		return wrap("<s "+size+">", "</s>", "文字")
	}
	return text, start, end
}

type styleToken struct {
	start   int
	end     int
	family  byte
	arg     string
	isClose bool
}

type stylePair struct {
	openStart  int
	openEnd    int
	closeStart int
	closeEnd   int
	family     byte
	arg        string
}

func matchStylePairs(text string) []stylePair {
	matches := styleTagRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return nil
	}
	var stack []styleToken
	var pairs []stylePair
	for _, m := range matches {
		isClose := m[3] > m[2]
		family := text[m[4]]
		arg := ""
		if m[6] >= 0 && m[7] >= 0 {
			arg = text[m[6]:m[7]]
		}
		if isClose {
			if arg != "" {
				continue
			}
			if n := len(stack); n > 0 && stack[n-1].family == family {
				open := stack[n-1]
				stack = stack[:n-1]
				pairs = append(pairs, stylePair{
					openStart:  open.start,
					openEnd:    open.end,
					closeStart: m[0],
					closeEnd:   m[1],
					family:     family,
					arg:        open.arg,
				})
			}
			continue
		}
		if family == 'c' && !validStyleColors[arg] {
			continue
		}
		if family == 's' && !validStyleSizes[arg] {
			continue
		}
		stack = append(stack, styleToken{start: m[0], end: m[1], family: family, arg: arg})
	}
	return pairs
}

func outermostPairs(pairs []stylePair) []stylePair {
	var out []stylePair
	for i, p := range pairs {
		nested := false
		for j, other := range pairs {
			if i == j {
				continue
			}
			if other.openStart < p.openStart && other.closeEnd > p.closeEnd {
				nested = true
				break
			}
		}
		if !nested {
			out = append(out, p)
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].openStart < out[j-1].openStart; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func isFence(trimmed string) bool {
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}
