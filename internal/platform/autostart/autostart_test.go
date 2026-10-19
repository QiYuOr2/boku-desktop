package autostart

import (
	"errors"
	"testing"
)

// These are the tests every platform runs: the parts of a login start that
// do not belong to one mechanism. The documents each platform writes are
// tested beside the code that writes them.

// The command line is what the system runs, so a path with spaces in it has
// to stay one argument — which is every macOS bundle and half the Windows
// installs — and the argument has to be after it, outside the quotes.
func TestCommandLineQuotesTheProgram(t *testing.T) {
	l := Launcher{Exe: "/Applications/Boku.app/Contents/MacOS/Boku", Arg: "--mygo-opened-at-login"}
	if got, want := l.CommandLine(), `"/Applications/Boku.app/Contents/MacOS/Boku" --mygo-opened-at-login`; got != want {
		t.Errorf("CommandLine = %q, want %q", got, want)
	}
	l.Arg = ""
	if got, want := l.CommandLine(), `"/Applications/Boku.app/Contents/MacOS/Boku"`; got != want {
		t.Errorf("CommandLine without an argument = %q, want %q", got, want)
	}
}

// A desktop with no login start answers every question with the same error,
// and Available reads that error as "do not draw the switch". This is the
// Linux and BSD path, so it runs on the machine that built the package.
func TestUnsupportedHasNoLoginStart(t *testing.T) {
	var manager Unsupported
	launcher := Launcher{Label: "com.boku.app", Name: "Boku", Exe: "/usr/local/bin/boku"}
	if _, err := manager.Read(launcher); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Read = %v, want ErrUnsupported", err)
	}
	if _, err := manager.Location(launcher); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Location = %v, want ErrUnsupported", err)
	}
	if err := manager.Start(launcher); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Start = %v, want ErrUnsupported", err)
	}
	if err := manager.Stop(launcher); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Stop = %v, want ErrUnsupported", err)
	}
	if Available(manager, launcher) {
		t.Error("Available said a desktop with no login start has one")
	}
}

// A manager that cannot answer must not be trusted to be drawn either: nil
// is what a state built without an assembly has, and it must not panic.
func TestAvailableNeedsAManager(t *testing.T) {
	if Available(nil, Launcher{}) {
		t.Error("Available said a nil manager has a login start")
	}
}

// The argument the login start passes is the framework's own marker: the
// application reads it to know nobody asked for the launch. It is written
// here once, so this is the only place it can go wrong.
func TestOpenedAtLoginArgument(t *testing.T) {
	if OpenedAtLoginArg != "--mygo-opened-at-login" {
		t.Errorf("OpenedAtLoginArg = %q, want the framework's own marker", OpenedAtLoginArg)
	}
}
