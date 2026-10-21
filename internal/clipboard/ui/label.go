// Package ui contains presentation helpers owned by clipboard history.
package ui

import (
	"strconv"

	"boku/internal/domain"
)

func Label(entry domain.ClipboardEntry) string {
	if entry.Kind == domain.ClipKindImage && entry.ImageBytes > 0 {
		return "图片 " + ByteSize(int(entry.ImageBytes))
	}
	return KindLabel(entry.Kind)
}

func KindLabel(kind domain.ClipKind) string {
	switch kind {
	case domain.ClipKindURL:
		return "链接"
	case domain.ClipKindJSON:
		return "JSON"
	case domain.ClipKindCode:
		return "代码"
	case domain.ClipKindFile:
		return "文件"
	case domain.ClipKindImage:
		return "图片"
	default:
		return ""
	}
}

func ByteSize(n int) string {
	switch {
	case n >= 1<<20:
		return strconv.Itoa(n/(1<<20)) + " MB"
	case n >= 1<<10:
		return strconv.Itoa(n/(1<<10)) + " KB"
	default:
		return strconv.Itoa(n) + " B"
	}
}
