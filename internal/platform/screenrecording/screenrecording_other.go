//go:build !darwin

package screenrecording

// granted and request report the permission as granted: these platforms
// require none for reading the screen.
func granted() bool { return true }
func request() bool { return true }

// settingsURL is empty: these platforms have no Screen Recording switch.
func settingsURL() string { return "" }
