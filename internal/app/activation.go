package app

import (
	"runtime"

	"github.com/egoist/mygo"
)

// syncActivationPolicy keeps the app's Dock and Cmd+Tab presence in step with
// the windows that are open.
//
// The tray app is an accessory: no Dock icon and no Cmd+Tab entry, which is
// what a menu-bar utility should be while it is only a tray icon. The settings
// and clipboard-history windows are ordinary windows, though: a user can
// switch away from them and expects Cmd+Tab to bring them back. While either
// is open the app is regular, so it is in the app switcher; when both are
// closed it returns to accessory.
//
// The quick window and the translation card are deliberately not in this
// list: they are transient and are not something to switch back to.
func (s *State) syncActivationPolicy() {
	if runtime.GOOS != "darwin" || s.app == nil {
		return
	}
	s.app.SetActivationPolicy(activationPolicyFor(s.settingsWindow != nil, s.historyWindow != nil))
}

// activationPolicyFor is the rule, separate from the windows so that "which
// windows put the app in Cmd+Tab" can be checked without a session.
func activationPolicyFor(settingsOpen, historyOpen bool) mygo.ActivationPolicy {
	if settingsOpen || historyOpen {
		return mygo.ActivationPolicyRegular
	}
	return mygo.ActivationPolicyAccessory
}
