package tools

import (
	"strings"
	"testing"

	"boku/internal/domain"
)

func TestClipContentHashIsStableAndDiscriminating(t *testing.T) {
	first := ClipContentHash("hello")
	if !strings.HasPrefix(first, ClipHashPrefix) {
		t.Errorf("ClipContentHash = %q, want it to start with %q", first, ClipHashPrefix)
	}
	if first != ClipContentHash("hello") {
		t.Error("ClipContentHash is not stable for the same text")
	}
	if first == ClipContentHash("hello ") {
		t.Error("ClipContentHash does not tell two texts apart")
	}
	// The digest of the empty string is a digest, not an empty answer.
	if got := ClipContentHash(""); !strings.HasPrefix(got, ClipHashPrefix) || len(got) != len(first) {
		t.Errorf("ClipContentHash(\"\") = %q", got)
	}
}

func TestClipDisplayFoldsWhitespaceAndShortens(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain text is kept", "hello world", "hello world"},
		{"newlines become spaces", "one\ntwo\nthree", "one two three"},
		{"runs of spaces collapse", "one    two", "one two"},
		{"tabs and ideographic spaces collapse", "one\t\u3000two", "one two"},
		{"a value of spaces is empty", "   \n\t ", ""},
		{"the empty value is empty", "", ""},
	}
	for _, c := range cases {
		if got := ClipDisplay(c.in); got != c.want {
			t.Errorf("%s: ClipDisplay(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}

	short := strings.Repeat("a", ClipDisplayLimit)
	if got := ClipDisplay(short); got != short {
		t.Errorf("ClipDisplay shortened a value of exactly the limit: %d runes", len([]rune(got)))
	}
	over := strings.Repeat("a", ClipDisplayLimit+1)
	got := ClipDisplay(over)
	if !strings.HasSuffix(got, ClipEllipsis) {
		t.Errorf("ClipDisplay of an over-long value = %q, want an ellipsis", got)
	}
	if runes := []rune(got); len(runes) != ClipDisplayLimit+1 {
		t.Errorf("ClipDisplay of an over-long value is %d runes, want %d", len(runes), ClipDisplayLimit+1)
	}
	// Truncation counts runes, not bytes: a Chinese value of the limit
	// length is not shortened.
	cjk := strings.Repeat("中", ClipDisplayLimit)
	if got := ClipDisplay(cjk); got != cjk {
		t.Errorf("ClipDisplay shortened %d CJK runes: %q", ClipDisplayLimit, got)
	}
}

func TestClipFileNameKeepsTheNameOnly(t *testing.T) {
	cases := map[string]string{
		`C:\Users\me\Documents\report.pdf`: "report.pdf",
		"/home/me/notes.txt":               "notes.txt",
		"name.txt":                         "name.txt",
		`C:\dir\`:                          "dir",
		"":                                 "",
		"   ":                              "",
		"/tmp/":                            "tmp",
	}
	for path, want := range cases {
		if got := ClipFileName(path); got != want {
			t.Errorf("ClipFileName(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestClipFileDisplay(t *testing.T) {
	cases := []struct {
		name  string
		paths []string
		want  string
	}{
		{"one file", []string{`C:\dir\a.txt`}, "[a.txt]"},
		{"two files", []string{"/tmp/a.txt", "/tmp/b.txt"}, "[a.txt 等 2 个文件]"},
		{"three files", []string{"a.png", "b.png", "c.png"}, "[a.png 等 3 个文件]"},
		{"names are skipped when blank", []string{"  ", "/tmp/b.txt", "c.txt"}, "[b.txt 等 2 个文件]"},
		{"nothing usable", []string{" ", ""}, "[图片]"},
		{"no files at all", nil, "[图片]"},
	}
	for _, c := range cases {
		if got := ClipFileDisplay(c.paths); got != c.want {
			t.Errorf("%s: ClipFileDisplay(%q) = %q, want %q", c.name, c.paths, got, c.want)
		}
	}
}

func TestClipImageName(t *testing.T) {
	if got := ClipImageName(""); got != "图片" {
		t.Errorf("ClipImageName(\"\") = %q, want 图片", got)
	}
	if got := ClipImageName(`C:\shots\截图.png`); got != "截图.png" {
		t.Errorf("ClipImageName = %q, want 截图.png", got)
	}
}

func TestClipTextTruncatesToTheLimit(t *testing.T) {
	if got := ClipText("short"); got != "short" {
		t.Errorf("ClipText shortened a short value: %q", got)
	}
	exact := strings.Repeat("a", domain.MaxClipTextBytes)
	if got := ClipText(exact); got != exact {
		t.Errorf("ClipText shortened a value of exactly the limit: %d bytes", len(got))
	}
	over := strings.Repeat("a", domain.MaxClipTextBytes) + "tail"
	got := ClipText(over)
	if len(got) != domain.MaxClipTextBytes {
		t.Errorf("ClipText kept %d bytes, want %d", len(got), domain.MaxClipTextBytes)
	}

	// The cut lands on a rune boundary: three bytes per rune means the last
	// rune may only be kept whole.
	cjk := strings.Repeat("中", domain.MaxClipTextBytes)
	cut := ClipText(cjk)
	if len(cut)%3 != 0 {
		t.Errorf("ClipText cut %d bytes of a 3-byte rune sequence", len(cut))
	}
	if !strings.HasPrefix(cjk, cut) {
		t.Error("ClipText did not keep the beginning of the value")
	}
}

func TestClassifyClip(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want domain.ClipKind
	}{
		{"empty is text", "", domain.ClipKindText},
		{"spaces are text", "   \n  ", domain.ClipKindText},
		{"a sentence is text", "今天天气不错，出去走走。", domain.ClipKindText},
		{"a bare number is text", "4210", domain.ClipKindText},
		{"a word is text", "hello", domain.ClipKindText},

		{"an https url", "https://example.com/a?b=1#c", domain.ClipKindURL},
		{"an http url with a port", "http://127.0.0.1:8080/", domain.ClipKindURL},
		{"a url with surrounding space", "  https://example.com  ", domain.ClipKindURL},
		{"a url cannot have spaces", "https://example.com /path", domain.ClipKindText},
		{"a scheme without a host is text", "https://", domain.ClipKindText},
		{"a url with markup after it is text", "https://example.com/<b>", domain.ClipKindText},
		{"three urls on one line are text", "https://x.dev https://y.dev", domain.ClipKindText},

		{"a json object", `{"a":1}`, domain.ClipKindJSON},
		{"a json array", "[1, 2, 3]", domain.ClipKindJSON},
		{"a json string", `"hello"`, domain.ClipKindJSON},
		{"broken json is text", `{"a": }`, domain.ClipKindText},
		{"an object-looking brace is text", "{not json}", domain.ClipKindText},

		{"go source", "package main\n\nfunc main() {\n\treturn\n}", domain.ClipKindCode},
		{"python source", "def run():\n    return 1\n", domain.ClipKindCode},
		{"javascript source", "const f = (a) => a && a.x;\nreturn f;", domain.ClipKindCode},
		{"html source", "<p class=\"a\">one</p>\n<div>two</div>\n<span>three</span>", domain.ClipKindCode},
		{"markup on two lines is enough", "<p class=\"a\">one</p>\nsecond line", domain.ClipKindCode},
		{"one line of code is text", "func main() {}", domain.ClipKindText},
		{"two lines without markers are text", "first line\nsecond line", domain.ClipKindText},
		{"english prose with a semicolon is text", "first clause; and a second one.\nA new sentence follows.", domain.ClipKindText},
		{"chinese prose with punctuation is text", "第一句；第二句。\n换行再写一句。", domain.ClipKindText},
		{"two urls on two lines are text", "https://x.dev\nhttps://y.dev", domain.ClipKindText},
		{"a path list is text", "a/b/c\nlist", domain.ClipKindText},
		{"a two-line list is text", "- milk\n- bread", domain.ClipKindText},
	}
	for _, c := range cases {
		if got := ClassifyClip(c.in); got != c.want {
			t.Errorf("%s: ClassifyClip(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}
