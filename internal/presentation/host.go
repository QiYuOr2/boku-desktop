// Package presentation 是应用 Shell 与各功能 UI 包共享的边界。
package presentation

// Host 是功能 UI 可以请求的一小组 Shell 能力。它刻意只包含回调，而不是 MyGo
// 窗口或 app.State 指针：功能 UI 必须在 import Shell 的情况下可复用。
//
// 每个回调都是可选的，这样功能视图在隔离的 UI 测试中仍可用；nil 回调为空操作。
type Host struct {
	RequestRefresh func()
	RunOnMain      func(func())
	FitQuickWindow func()
	SetStatus      func(string)
}

func (h Host) Refresh() {
	if h.RequestRefresh != nil {
		h.RequestRefresh()
	}
}

func (h Host) OnMain(fn func()) {
	if fn == nil {
		return
	}
	if h.RunOnMain != nil {
		h.RunOnMain(fn)
		return
	}
	fn()
}

func (h Host) FitQuick() {
	if h.FitQuickWindow != nil {
		h.FitQuickWindow()
	}
}

func (h Host) Status(text string) {
	if h.SetStatus != nil {
		h.SetStatus(text)
	}
}
