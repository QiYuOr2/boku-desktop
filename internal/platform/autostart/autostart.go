package autostart

import (
	"errors"
	"io/fs"
)

// Launcher is one login start: the program it runs, the argument that tells
// the program the system started it, and the name the start is recorded under.
type Launcher struct {
	// Label is the application's identifier: the name of the Windows value
	// and the name of the launch agent's file. It is what the start is
	// recorded under, and it does not change when the application is renamed
	// for people.
	Label string
	// Name is the application's name as a person reads it.
	Name string
	// Exe is the program the start runs, as an absolute path. On macOS it is
	// the executable inside the app bundle, because a login start names a
	// program rather than a bundle.
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

// Document is where a start lives and what is stored there.
type Document struct {
	// Key is where the start lives, in the platform's own terms: the full
	// name of the Windows registry value, the path of the launch agent's
	// file. It is what a message about the start calls it, and what the log
	// records as where the start is.
	Key string
	// Contents is what that place holds: a command line on Windows, a
	// property list on macOS, a desktop entry on a Linux desktop.
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
	// the platform having no login start that Boku can register.
	Read(Launcher) (bool, error)
	// Document is what Start writes, or what would have to be removed to
	// stop it. It is what a message or a test can show without writing.
	Document(Launcher) (Document, error)
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
)

// OpenedAtLoginArg is the argument a login start appends to the command line:
// the application's way of knowing that the system started it and nobody is
// in front of it. The framework removes it from its own command line before
// main sees it, and answers WasOpenedAtLogin from it, so Windows, macOS and
// the framework have to agree on the string exactly — which is why it is
// written once, here.
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

// notExist reports whether err is "there is no such file", which is how an
// absent login start reads.
func notExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }
