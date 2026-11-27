package ui

import (
	"boku/internal/clipboard"
	"boku/internal/presentation"
	"time"
)

// State 持有剪贴板展示状态（含临时的行内反馈）。原生历史窗口由 Shell 持有，
// 并通过 openHistory 回调提供打开动作。
type State struct {
	clipboard       *clipboard.Clipboard
	design          presentation.ClipboardDesign
	host            presentation.Host
	openHistory     func()
	copiedEntry     string
	copiedAt        time.Time
	clipboardStatus string
}

func New(service *clipboard.Clipboard, host presentation.Host, open func()) *State {
	return &State{clipboard: service, host: host, openHistory: open}
}
