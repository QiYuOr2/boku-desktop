// Clipboard helpers: everything about a copied value that can be decided
// without a screen, a clipboard or a clock. The use case classifies and
// shortens content with these; the interface only draws what they return.

package kit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	"boku/internal/domain"
)

const (
	// ClipDisplayLimit is how much of one copied value the quick window's
	// clipboard line shows, in runes. A line is all there is room for; the
	// whole value is one click away in the history.
	ClipDisplayLimit = 120
	// ClipEllipsis marks a value that was shortened.
	ClipEllipsis = "…"
	// ClipHashPrefix names the digest, the way §7 of the domain model writes
	// a content hash.
	ClipHashPrefix = "sha256:"
)

// ClipContentHash is the digest that identifies a copied value: two copies
// with the same text are the same entry.
func ClipContentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return ClipHashPrefix + hex.EncodeToString(sum[:])
}

// ClipImageRef is the digest that identifies a copied picture: the hash of the
// bitmap's bytes, and the name it is kept under.
//
// The picture itself is not in the history file — a screenshot is megabytes and
// the file is written whole on every copy — so what identifies it has to be
// cheap to store and impossible to confuse with another picture. Two
// screenshots of the same size are two pictures; only their bytes say so.
func ClipImageRef(png []byte) string {
	sum := sha256.Sum256(png)
	return ClipHashPrefix + hex.EncodeToString(sum[:])
}

// ClipDisplay is the one line the interface shows for a copied value:
// whitespace folded into single spaces, then shortened to ClipDisplayLimit
// runes with an ellipsis.
//
// Folding first is what keeps a pasted file, a stack trace or a minified
// document from drawing a line wider than the window.
func ClipDisplay(text string) string {
	folded := strings.Join(strings.Fields(text), " ")
	if folded == "" {
		return ""
	}
	if utf8.RuneCountInString(folded) <= ClipDisplayLimit {
		return folded
	}
	runes := []rune(folded)
	return string(runes[:ClipDisplayLimit]) + ClipEllipsis
}

// ClipFileDisplay is the line for a list of copied files: the name of the
// first, and how many others came with it. The paths themselves are not
// shown — one line cannot hold them, and a path is not what the user
// recognizes.
func ClipFileDisplay(paths []string) string {
	names := make([]string, 0, len(paths))
	for _, path := range paths {
		name := ClipFileName(path)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	switch len(names) {
	case 0:
		return "[" + ClipImageName("") + "]"
	case 1:
		return "[" + names[0] + "]"
	default:
		return "[" + names[0] + " 等 " + strconv.Itoa(len(names)) + " 个文件]"
	}
}

// ClipFileName is what a path is called, without the directories: the name
// is what a person recognizes, and it is all one line has room for.
func ClipFileName(path string) string {
	cleaned := strings.TrimSpace(path)
	if cleaned == "" {
		return ""
	}
	// Accept both separators: a path copied on Windows may be read on a
	// machine that only knows the other one.
	cleaned = strings.ReplaceAll(cleaned, `\`, "/")
	cleaned = strings.TrimRight(cleaned, "/")
	if cleaned == "" {
		return ""
	}
	if index := strings.LastIndex(cleaned, "/"); index >= 0 {
		cleaned = cleaned[index+1:]
	}
	return strings.TrimSpace(cleaned)
}

// ClipImageName is the line for an image: its file name when the platform
// knows one, and a plain label when it does not.
func ClipImageName(name string) string {
	cleaned := ClipFileName(name)
	if cleaned == "" {
		return "图片"
	}
	return cleaned
}

// ClipText is a copied text as the history keeps it: whole, unless it is
// longer than MaxClipTextBytes, in which case the beginning is kept. The
// cut lands on a rune boundary so that no entry holds half a character.
func ClipText(text string) string {
	if len(text) <= domain.MaxClipTextBytes {
		return text
	}
	cut := domain.MaxClipTextBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

// ClassifyClip is the kind of a copied text: what the history says it is,
// which is what lets a URL be recognized among a hundred copies.
//
// Code is looked for first, because a snippet is several lines and may well
// open with a URL in a comment or a string; a one-line value never counts
// as code. JSON is only claimed when the whole value parses.
func ClassifyClip(text string) domain.ClipKind {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return domain.ClipKindText
	}
	if looksLikeCode(trimmed) {
		return domain.ClipKindCode
	}
	if looksLikeURL(trimmed) {
		return domain.ClipKindURL
	}
	if looksLikeJSON(trimmed) {
		return domain.ClipKindJSON
	}
	return domain.ClipKindText
}

// looksLikeURL accepts a single line that is one absolute http(s) address:
// nothing before the scheme, nothing after the address that belongs to
// another word, and a host to go to.
func looksLikeURL(text string) bool {
	if strings.ContainsAny(text, " \t\n\r") {
		return false
	}
	lower := strings.ToLower(text)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return false
	}
	rest := text[strings.Index(text, "://")+3:]
	if rest == "" {
		return false
	}
	if strings.ContainsAny(rest, "<>\"'") {
		return false // markup around a URL is not a URL
	}
	host := rest
	if index := strings.IndexAny(host, "/?#"); index >= 0 {
		host = host[:index]
	}
	return host != ""
}

// looksLikeJSON accepts text that parses as a JSON document or value, and
// that is not merely a number or a bare word — those are text.
func looksLikeJSON(text string) bool {
	first := text[0]
	if first != '{' && first != '[' && first != '"' {
		return false
	}
	var value any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		return false
	}
	return true
}

// codeMarkers are the signs of a source snippet. They are deliberately
// crude: the kind is a label in a list, not a language detector, and a wrong
// label costs nothing.
//
// Only markers that prose does not use are here: a sentence may well end on
// a semicolon, and Chinese prose is punctuated with full-width ones, which
// are different characters.
var codeMarkers = []string{
	"func ", "def ", "class ", "import ", "package ", "const ", "let ", "var ",
	"return ", "if (", "if(", "for (", "for(", "while (", "while(", "pub fn ",
	"#include", "<?php", "</", "/>", "=>", "::", "!=", "===", "&&", "||",
	"{}", "];", "});", "):", " = ", " := ",
	// Markup: a tag that closes on its own line, and the two attributes
	// that show up in almost every pasted document.
	">\n<", "class=\"", "class='",
}

// looksLikeCode accepts a multi-line value that reads like source: several
// lines, and several of the markers above in them.
func looksLikeCode(text string) bool {
	if !strings.Contains(text, "\n") {
		return false
	}
	lines := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			lines++
		}
	}
	if lines < 2 {
		return false
	}
	hits := 0
	for _, marker := range codeMarkers {
		if strings.Contains(text, marker) {
			hits++
		}
	}
	return hits >= 2
}
