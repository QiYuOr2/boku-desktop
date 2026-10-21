// Package presentation contains the boundary shared by the application Shell
// and feature-specific UI packages.
package presentation

// Host is the small set of application-shell capabilities a feature UI may
// request. It deliberately contains callbacks rather than MyGo windows or an
// app.State pointer: feature UI must be reusable without importing the Shell.
//
// Every callback is optional so a feature view remains usable in isolated UI
// tests. A nil callback is a no-op.
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
