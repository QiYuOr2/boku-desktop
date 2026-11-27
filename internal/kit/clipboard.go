// 剪贴板纯函数：不依赖屏幕、剪贴板或时钟即可完成的判断。
// 用例层用它们分类与截断内容，界面只负责绘制结果。
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
	// ClipDisplayLimit 是快捷窗口一行最多显示的字符数（按 rune 计）。
	ClipDisplayLimit = 120
	// ClipEllipsis 标记被截短的值。
	ClipEllipsis = "…"
	// ClipHashPrefix 是内容哈希的前缀，与领域模型 §7 的写法一致。
	ClipHashPrefix = "sha256:"
)

// ClipContentHash 是标识一条复制内容的摘要：文本相同的两次复制视为同一条目。
func ClipContentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return ClipHashPrefix + hex.EncodeToString(sum[:])
}

// ClipImageRef 是标识一张复制图片的摘要：点位图字节的哈希，也用作存储名。
// 图片本身不进历史文件（截图有数兆字节，且每次复制都会重写整份文件），
// 故标识必须存储成本低、且不会与另一张图片混淆：同为相同尺寸的两张截图，
// 只有字节能区分。
func ClipImageRef(png []byte) string {
	sum := sha256.Sum256(png)
	return ClipHashPrefix + hex.EncodeToString(sum[:])
}

// ClipDisplay 返回界面为一条复制内容显示的单独一行：先把空白折叠为单个空格，
// 再截到 ClipDisplayLimit 个 rune 并加省略号。
// 先折叠是为了避免粘贴的文件、堆栈或压缩文档把一行撑得比窗口还宽。
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

// ClipFileDisplay 返回一列复制文件的展示行：第一个文件名加其余文件数量。
// 不显示完整路径——一行放不下，且路径不是用户辨认的依据。
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

// ClipFileName 返回路径中的文件名（不含目录）：文件名才是用户辨认的依据，
// 也是一行放得下的部分。
func ClipFileName(path string) string {
	cleaned := strings.TrimSpace(path)
	if cleaned == "" {
		return ""
	}
	// 同时接受两种分隔符：在 Windows 复制的路径可能被只认识另一种的机器读取。
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

// ClipImageName 返回图片的展示行：平台知道文件名时用文件名，否则用普通标签。
func ClipImageName(name string) string {
	cleaned := ClipFileName(name)
	if cleaned == "" {
		return "图片"
	}
	return cleaned
}

// ClipText 返回历史中保存的复制文本：完整保留，除非长于 MaxClipTextBytes，
// 此时只留开头。切割落在 rune 边界上，任何条目都不会存半个字符。
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

// ClassifyClip 返回一条复制文本的类型，历史据此在大量条目中认出 URL。
// 先判断代码：代码片段常有多行，且可能以注释或字符串里的 URL 开头；
// 单行值永远不算代码。JSON 只在整段可解析时才认定。
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

// looksLikeURL 接受单独一行、且是一个完整 http(s) 地址的文本：
// scheme 前无内容、地址后没有属于别的词的字符、且存在主机名。
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
		return false // URL 周围的标记说明它不是纯 URL
	}
	host := rest
	if index := strings.IndexAny(host, "/?#"); index >= 0 {
		host = host[:index]
	}
	return host != ""
}

// looksLikeJSON 接受能解析为 JSON 文档或值的文本，但不包括纯数字或裸词——那些算文本。
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

// codeMarkers 是源码片段的特征标记。判断刻意粗糙：类型只是列表里的一个标签，
// 不是语言检测器，标错也无妨。
// 这里只放散文不会用的标记：句子可能以分号结尾，而中文散文用的是全角标点，
// 那是不同的字符。
var codeMarkers = []string{
	"func ", "def ", "class ", "import ", "package ", "const ", "let ", "var ",
	"return ", "if (", "if(", "for (", "for(", "while (", "while(", "pub fn ",
	"#include", "<?php", "</", "/>", "=>", "::", "!=", "===", "&&", "||",
	"{}", "];", "});", "):", " = ", " := ",
	// 标记：独占一行闭合的标签，以及几乎每个粘贴文档里都会出现的两个属性。
	">\n<", "class=\"", "class='",
}

// looksLikeCode 接受看起来像源码的多行值：至少两行，且命中上面多个标记。
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
