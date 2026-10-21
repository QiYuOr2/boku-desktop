package ui

import (
	"boku/internal/clipboard"
	"boku/internal/presentation"
	"time"
)

// State owns clipboard presentation, including transient row feedback.
// The Shell owns the native history window and supplies its open action.
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
