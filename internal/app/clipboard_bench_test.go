package app

import (
	"context"
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"

	"boku/internal/domain"
)

// A frame of each window, with a full page of history: what it costs to draw
// one. The number is not a budget, it is the evidence for whether a change
// made the window faster.
func BenchmarkHistoryFrame(b *testing.B) {
	device := &testDevice{}
	state := clipboardState(b, device, &testToolStore{
		settings: domain.ToolSettings{Clipboard: domain.ClipboardSettings{RegionVisible: true, Monitoring: true}},
	})
	// A full record range, so the frame is measured at the size the user can
	// ask for.
	size := state.clipboardHistorySize()
	ctx := context.Background()
	for i := 0; i < size; i++ {
		device.set(domain.ClipContent{
			Kind: domain.ClipKindText,
			Text: fmt.Sprintf("第 %d 条复制的内容，这是一行比较长的文字用来撑开宽度", i),
		})
		state.clipboard.Read(ctx)
	}
	if got := len(state.clipboardHistory()); got != size {
		b.Fatalf("the history holds %d entries, want %d", got, size)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ui.Render(state.historyView, historyWidth, historyHeight, 2)
	}
}

// The quick window's frame, with the clipboard line drawn.
func BenchmarkFlyoutFrame(b *testing.B) {
	device := &testDevice{}
	state := clipboardState(b, device, &testToolStore{
		settings: domain.ToolSettings{Clipboard: domain.ClipboardSettings{RegionVisible: true, Monitoring: true}},
	})
	device.set(domain.ClipContent{Kind: domain.ClipKindText, Text: "https://example.com/some/rather/long/path"})
	state.clipboard.Read(context.Background())

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ui.Render(state.View, panelWidth, panelHeight, 2)
	}
}

// Reading the history for a frame: a lock and a copy of the page.
func BenchmarkHistoryRead(b *testing.B) {
	device := &testDevice{}
	state := clipboardState(b, device, &testToolStore{
		settings: domain.ToolSettings{Clipboard: domain.ClipboardSettings{RegionVisible: true, Monitoring: true}},
	})
	ctx := context.Background()
	for i := 0; i < state.clipboardHistorySize(); i++ {
		device.set(domain.ClipContent{Kind: domain.ClipKindText, Text: fmt.Sprintf("copy %d", i)})
		state.clipboard.Read(ctx)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := len(state.clipboardHistory()); got == 0 {
			b.Fatal("no history")
		}
	}
}
