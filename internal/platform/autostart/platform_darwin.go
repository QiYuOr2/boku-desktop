//go:build darwin

package autostart

// New returns this desktop's login start. It is the one place main asks
// which mechanism the platform has, and the answer is a value rather than a
// behaviour switch in the assembly.
//
// It is the application's own login item, not a launch agent: only the login
// item appears in System Settings > General > Login Items & Extensions, which
// is where a user looks for what starts at login.
func New() Manager { return NewSMAppService() }
