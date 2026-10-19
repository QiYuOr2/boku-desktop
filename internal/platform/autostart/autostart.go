package autostart

import (
	"errors"
)

// Launcher is one login start: the program it runs, the argument that tells
// the program the system started it, and the name the start is recorded under.
type Launcher struct {
	// Label is the application's identifier. macOS no longer names a login
	// start after it — the system login item is the application itself — but
	// it is still the label of the launch agent written by the previous
	// mechanism, which the manager removes on the first read.
	Label string
	// Name is the application's name as a person reads it.
	Name string
	// Exe is the program the start runs, as an absolute path. Windows names
	// a program; macOS registers the running application bundle and ignores
	// the path.
	Exe string
	// Arg is appended to the command line: the application's own way of
	// knowing that nobody asked for this start.
	Arg string
}

// CommandLine is what the system runs: the program, quoted, and the argument.
// Quoting is what lets a path with a space in it — which every macOS bundle
// has — stay one argument.
func (l Launcher) CommandLine() string {
	if l.Arg == "" {
		return `"` + l.Exe + `"`
	}
	return `"` + l.Exe + `" ` + l.Arg
}

// Document is where a Windows login start lives and what is stored there.
type Document struct {
	// Key is the full name of the Windows registry value.
	Key string
	// Contents is the command line in it.
	Contents []byte
}

// Manager is a platform's login start.
//
// Nothing here starts a process, and nothing touches the disk before it is
// asked: the manager can be built before the application is running, and
// every question is answered from the platform's own record rather than from
// what this process remembers.
type Manager interface {
	// Read reports whether this launcher's start is recorded and in effect.
	//
	// A start that names another program is not this one: an update that
	// moved the application leaves the old start behind, and the answer to
	// "does the application start at login" is then no until the switch is
	// pressed again.
	//
	// An error means the question could not be answered. ErrUnsupported is
	// the platform having no login start that Boku can register, and
	// ErrNeedsApproval is a start the system recorded but the user has to
	// allow in System Settings before it takes effect.
	Read(Launcher) (bool, error)
	// Location is where the start lives, in the platform's own terms: a
	// registry value, or the system settings page that lists login items. It
	// is what the settings page shows under the switch, and it is asked
	// rather than assumed so that the line and the platform cannot disagree.
	Location(Launcher) (string, error)
	// Start records the start. Asking again is also how a start the platform
	// switched off is switched back on — Task Manager's disabled entry on
	// Windows — and how a start that names an older copy of the application
	// is brought up to date.
	Start(Launcher) error
	// Stop removes it. A start that is not there is not an error: nothing is
	// left behind, which is what was asked for.
	Stop(Launcher) error
}

// The problems a manager reports, spelled so that the caller can tell a
// machine that cannot have a login start from one where the write failed.
var (
	// ErrUnsupported means this desktop has no login start that Boku can
	// register, and nothing is wrong with the machine.
	ErrUnsupported = errors.New("autostart: this desktop cannot start the application at login")
	// ErrNotInstalled means the platform can register a login start, but the
	// application is not running from a place it will still be in at the next
	// login — `go run`, a build directory, a copy that is about to be
	// replaced. The start would point at a program that is no longer there,
	// so it is refused rather than written.
	ErrNotInstalled = errors.New("autostart: the application is not running from a location it will still be in at the next login")
	// ErrNeedsApproval means the system has the login item, but the user
	// turned it off in System Settings (or the system still requires them to
	// allow it). It is not a failure of the write: the page says where to
	// allow it instead of claiming the switch is off.
	ErrNeedsApproval = errors.New("autostart: the login item needs approval in System Settings")
)

// OpenedAtLoginArg is the argument the Windows login start appends to the
// command line: the application's way of knowing that the system started it
// and nobody is in front of it. The framework removes it from its own command
// line before main sees it, and answers WasOpenedAtLogin from it, so the
// Windows manager and the framework have to agree on the string exactly —
// which is why it is written once, here.
//
// macOS's login item launches the application bundle without an argument; the
// application does not open a window on launch, so nothing depends on it
// there.
const OpenedAtLoginArg = "--mygo-opened-at-login"

// Available reports whether the manager has a login start at all, which is
// what tells the settings page whether to draw the switch: a switch that
// would do nothing is worse than no switch.
func Available(m Manager, l Launcher) bool {
	if m == nil {
		return false
	}
	_, err := m.Read(l)
	return !errors.Is(err, ErrUnsupported)
}
