package tools

import (
	"strings"
	"testing"
)

func TestExtractHTMLText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain text is kept", "just text", "just text"},
		{"entities are decoded", "a &amp; b &lt;c&gt;", "a & b <c>"},
		{"numeric entities are decoded", "&#65;&#x42;", "AB"},
		{"an unknown entity is left alone", "&notreal;", "&notreal;"},
		{"an unterminated entity is left alone", "100&", "100&"},

		{
			"paragraphs become lines",
			"<p>first</p><p>second</p>",
			"first\nsecond",
		},
		{
			"inline markup is stripped",
			`<p>a <strong>bold</strong> and <a href="https://example.com">a link</a></p>`,
			"a bold and a link",
		},
		{
			"breaks split lines",
			"one<br>two<br/>three",
			"one\ntwo\nthree",
		},
		{
			"block ends make lines",
			"<div>alpha</div><div>beta</div>",
			"alpha\nbeta",
		},
		{
			"list items become lines",
			"<ul><li>one</li><li>two</li></ul>",
			"one\ntwo",
		},
		{
			"tables become lines",
			"<table><tr><td>a</td><td>b</td></tr><tr><td>c</td></tr></table>",
			"a\nb\nc",
		},
		{
			"whitespace collapses and never starts a line",
			"<p>  hello   world  </p>\n<p>\n  again\n</p>",
			"hello world\nagain",
		},
		{
			"blank lines at the ends go away",
			"<p><br>text<br></p>",
			"text",
		},
		{
			"script content is dropped",
			"<p>before</p><script>var a = 1;\nalert(a)</script><p>after</p>",
			"before\nafter",
		},
		{
			"style content is dropped",
			"<style>p { color: red }</style><p>text</p>",
			"text",
		},
		{
			"an unterminated script drops the rest",
			"<p>text</p><script>never closed",
			"text",
		},
		{
			"a comment is dropped",
			"before<!-- hidden -->after",
			"beforeafter",
		},
		{
			"an unterminated comment drops the rest",
			"before<!-- never closed",
			"before",
		},
		{
			"a bare angle bracket is text",
			"a < b",
			"a < b",
		},
		{
			"an unterminated tag is text",
			"a <span",
			"a <span",
		},
		{
			"nbsp becomes a space",
			"a&nbsp;b",
			"a b",
		},
		{
			"attributes do not become text",
			`<p class="x" data-y='z'>text</p>`,
			"text",
		},
		{
			"a full document becomes its text",
			"<!doctype html><html><head><title>t</title></head><body><h1>Title</h1><p>Body text</p></body></html>",
			"Title\nBody text",
		},
		{
			"cjk text is kept whole",
			"<p>你好，世界</p>",
			"你好，世界",
		},
		{
			"a line break in the markup stays a break",
			"<p>one\r\ntwo</p>",
			"one\ntwo",
		},
		{
			"a pasted document keeps its paragraphs",
			"<!doctype html><html><body><h1>Title</h1>\n<p>Body text</p></body></html>",
			"Title\nBody text",
		},
	}
	for _, c := range cases {
		if got := ExtractHTMLText(c.in); got != c.want {
			t.Errorf("%s: ExtractHTMLText(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestExtractHTMLTextIsStableAndBounded(t *testing.T) {
	markup := "<p>" + strings.Repeat("word ", 2000) + "</p>"
	first := ExtractHTMLText(markup)
	if first != ExtractHTMLText(markup) {
		t.Error("ExtractHTMLText is not stable for the same markup")
	}
	if strings.Contains(first, "\n") {
		t.Error("a single paragraph was broken into several lines")
	}
	// A tag soup must not grow in the conversion.
	tags := strings.Repeat(`<span style="color:red">`, 100) + "text"
	if got := ExtractHTMLText(tags); got != "text" {
		t.Errorf("ExtractHTMLText(%d tags) = %q, want %q", 100, got, "text")
	}
}

func TestDecodeHTMLEntities(t *testing.T) {
	cases := map[string]string{
		"&amp;":       "&",
		"&#39;":       "'",
		"&#x27;":      "'",
		"&hellip;":    "…",
		"&nbsp;":      " ",
		"&unknown;":   "&unknown;",
		"plain":       "plain",
		"&#0;":        "&#0;", // an invalid code point is left alone
		"&#99999999;": "&#99999999;",
	}
	for in, want := range cases {
		if got := decodeHTMLEntities(in); got != want {
			t.Errorf("decodeHTMLEntities(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseTag(t *testing.T) {
	cases := []struct {
		tag         string
		name        string
		closing     bool
		selfClosing bool
	}{
		{"p", "p", false, false},
		{"/p", "p", true, false},
		{"br/", "br", false, true},
		{"br /", "br", false, true},
		{"  DIV  ", "div", false, false},
		{`a href="x"`, "a", false, false},
		{"", "", false, false},
	}
	for _, c := range cases {
		name, closing, selfClosing := parseTag(c.tag)
		if name != c.name || closing != c.closing || selfClosing != c.selfClosing {
			t.Errorf("parseTag(%q) = %q, %v, %v; want %q, %v, %v",
				c.tag, name, closing, selfClosing, c.name, c.closing, c.selfClosing)
		}
	}
}
