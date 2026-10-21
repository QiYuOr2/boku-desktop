package presentation

import "testing"

func TestHostRunsCallbacksAndIsSafeWhenEmpty(t *testing.T) {
	var calls []string
	host := Host{
		RequestRefresh: func() { calls = append(calls, "refresh") },
		RunOnMain: func(fn func()) {
			calls = append(calls, "main")
			fn()
		},
		FitQuickWindow: func() { calls = append(calls, "fit") },
		SetStatus:      func(text string) { calls = append(calls, text) },
	}
	host.Refresh()
	host.OnMain(func() { calls = append(calls, "work") })
	host.FitQuick()
	host.Status("status")
	if got, want := len(calls), 5; got != want {
		t.Fatalf("callbacks = %v, want %d calls", calls, want)
	}
	Host{}.Refresh()
	Host{}.OnMain(nil)
	Host{}.FitQuick()
	Host{}.Status("ignored")
}
