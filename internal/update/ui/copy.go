// Package ui owns user-facing copy derived from updater state.
package ui

import (
	"strconv"
	"strings"

	"boku/internal/domain"
)

func DownloadingLine(state domain.UpdateState) string {
	head := "正在下载"
	if version := Version(state.Version); version != "" {
		head += " " + version
	}
	percent, known := state.Percent()
	if !known || percent <= 0 {
		return head + "…"
	}
	return head + "… " + strconv.Itoa(percent) + "%"
}
func MenuLabel(state domain.UpdateState) (string, bool) {
	if state.Status != domain.UpdateReady {
		return "", false
	}
	if version := Version(state.Version); version != "" {
		return "重启并更新到 " + version, true
	}
	return "重启并更新", true
}
func Notice(state domain.UpdateState) (string, string) {
	title := "Boku 有新版本可以用了"
	if version := Version(state.Version); version != "" {
		return title, "新版本 " + version + " 已经下载好，重启 Boku 就会装上它。"
	}
	return title, "重启 Boku 就会装上新版本。"
}
func Version(version string) string { return strings.TrimPrefix(strings.TrimSpace(version), "v") }
