package playtime

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// parseVDF reads the text Valve KeyValue files Steam writes for libraries
// and manifests. A value is either a string or a map of the same. Binary
// KeyValues, such as shortcuts.vdf, are not this format and are not read.
func parseVDF(text string) (map[string]any, error) {
	p := parser{text: text}
	value, err := p.object()
	if err != nil {
		return nil, err
	}
	p.skip()
	if p.i < len(p.text) {
		return nil, fmt.Errorf("vdf: trailing input at %d", p.i)
	}
	return value, nil
}

type parser struct {
	text string
	i    int
}

func (p *parser) object() (map[string]any, error) {
	out := map[string]any{}
	for {
		p.skip()
		if p.i >= len(p.text) || p.text[p.i] == '}' {
			return out, nil
		}
		key, err := p.quoted()
		if err != nil {
			return nil, err
		}
		p.skip()
		if p.i < len(p.text) && p.text[p.i] == '{' {
			p.i++
			child, err := p.object()
			if err != nil {
				return nil, err
			}
			p.skip()
			if p.i >= len(p.text) || p.text[p.i] != '}' {
				return nil, fmt.Errorf("vdf: object %q is not closed", key)
			}
			p.i++
			out[key] = child
			continue
		}
		value, err := p.quoted()
		if err != nil {
			return nil, err
		}
		out[key] = value
	}
}

func (p *parser) quoted() (string, error) {
	p.skip()
	if p.i >= len(p.text) || p.text[p.i] != '"' {
		return "", fmt.Errorf("vdf: expected a string at %d", p.i)
	}
	p.i++
	var b strings.Builder
	for p.i < len(p.text) {
		ch := p.text[p.i]
		if ch == '"' {
			p.i++
			return b.String(), nil
		}
		if ch == '\\' && p.i+1 < len(p.text) {
			p.i++
			b.WriteByte(p.text[p.i])
			p.i++
			continue
		}
		r, size := utf8.DecodeRuneInString(p.text[p.i:])
		b.WriteRune(r)
		p.i += size
	}
	return "", fmt.Errorf("vdf: a string runs off the end")
}

func (p *parser) skip() {
	for p.i < len(p.text) {
		r, size := utf8.DecodeRuneInString(p.text[p.i:])
		if unicode.IsSpace(r) {
			p.i += size
			continue
		}
		if strings.HasPrefix(p.text[p.i:], "//") {
			for p.i < len(p.text) && p.text[p.i] != '\n' {
				p.i++
			}
			continue
		}
		return
	}
}
