//go:build darwin

package autostart

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The macOS login start. These tests run on a Mac — `go test ./...` there is
// what proves the document launchd reads — but nothing in them needs a
// logged-in session: the manager writes into a temporary directory, and the
// document is decided by a pure function.

// launcher is the login start the tests use: an application inside a bundle,
// which is the only place a login start is ever written from.
func launcher() Launcher {
	return Launcher{
		Label: "com.boku.app",
		Name:  "Boku",
		Exe:   "/Applications/Boku.app/Contents/MacOS/Boku",
		Arg:   OpenedAtLoginArg,
	}
}

// agent returns a manager over a temporary directory, which is the folder a
// test may write in and the only one it does.
func agent(t *testing.T) *LaunchAgent {
	t.Helper()
	return &LaunchAgent{Dir: t.TempDir()}
}

// The document is the one launchd reads: a property list with the label, the
// program and its argument, and the flag that starts it at login.
func TestLaunchAgentDocument(t *testing.T) {
	doc, err := agent(t).Document(launcher())
	if err != nil {
		t.Fatalf("Document failed: %v", err)
	}
	if got, want := doc.Key, filepath.Join(agent(t).Dir, "com.boku.app.plist"); got != want {
		t.Errorf("the launch agent is %q, want %q", got, want)
	}
	// It has to parse as a property list, not just contain the right words.
	type plist struct {
		Dict struct {
			Keys   []string `xml:"key"`
			Arrays []struct {
				Strings []string `xml:"string"`
			} `xml:"array"`
			Strings []string `xml:"string"`
		} `xml:"dict"`
	}
	var parsed plist
	if err := xml.Unmarshal(doc.Contents, &parsed); err != nil {
		t.Fatalf("the document is not XML: %v\n%s", err, doc.Contents)
	}
	keys := strings.Join(parsed.Dict.Keys, ",")
	for _, want := range []string{"Label", "ProgramArguments", "RunAtLoad"} {
		if !strings.Contains(keys, want) {
			t.Errorf("the document has no %s key: %s", want, keys)
		}
	}
	program := parsed.Dict.Arrays[0].Strings
	if len(program) != 2 || program[0] != launcher().Exe || program[1] != OpenedAtLoginArg {
		t.Errorf("ProgramArguments = %q, want the program and its argument", program)
	}
}

// A path is allowed to contain characters that end an XML document early.
// launchd would refuse the file at the next login — silently.
func TestLaunchAgentEscapesTheProgram(t *testing.T) {
	l := launcher()
	l.Exe = "/Applications/A & B <Boku>.app/Contents/MacOS/Boku"
	doc, err := agent(t).Document(l)
	if err != nil {
		t.Fatalf("Document failed: %v", err)
	}
	var parsed struct{}
	if err := xml.Unmarshal(doc.Contents, &parsed); err != nil {
		t.Fatalf("the escaped document is not XML: %v\n%s", err, doc.Contents)
	}
	program, err := launchAgentProgram(doc.Contents)
	if err != nil {
		t.Fatalf("the document has no program: %v", err)
	}
	if program != l.Exe {
		t.Errorf("the program reads back as %q, want %q", program, l.Exe)
	}
}

// Off, then on, then off again: what the switch does, and what it says at
// each step.
func TestLaunchAgentStartStop(t *testing.T) {
	a := agent(t)
	l := launcher()
	if on, err := a.Read(l); err != nil || on {
		t.Fatalf("Read before Start = %v, %v; want false, nil", on, err)
	}
	if err := a.Start(l); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if on, err := a.Read(l); err != nil || !on {
		t.Fatalf("Read after Start = %v, %v; want true, nil", on, err)
	}
	if err := a.Stop(l); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if on, err := a.Read(l); err != nil || on {
		t.Fatalf("Read after Stop = %v, %v; want false, nil", on, err)
	}
}

// Stopping a start that was never written is the state the user asked for,
// not a failure: the switch is off either way.
func TestLaunchAgentStopWithoutAStart(t *testing.T) {
	if err := agent(t).Stop(launcher()); err != nil {
		t.Errorf("Stop without a start = %v, want nil", err)
	}
}

// An update moves the application, and the login start has to follow it: a
// document that names the old bundle is not a start of the new one.
func TestLaunchAgentReadsAnotherCopyAsOff(t *testing.T) {
	a := agent(t)
	if err := a.Start(launcher()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	moved := launcher()
	moved.Exe = "/Applications/Boku 2.app/Contents/MacOS/Boku"
	if on, err := a.Read(moved); err != nil || on {
		t.Fatalf("Read of a document naming another program = %v, %v; want false, nil", on, err)
	}
	// And writing it again is how it is brought up to date.
	if err := a.Start(moved); err != nil {
		t.Fatalf("Start after the move failed: %v", err)
	}
	if on, err := a.Read(moved); err != nil || !on {
		t.Fatalf("Read after rewriting = %v, %v; want true, nil", on, err)
	}
}

// The folder does not exist on a machine where nothing has ever registered
// a login start. Start has to create it — and only then.
func TestLaunchAgentCreatesItsFolder(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Library", "LaunchAgents")
	a := &LaunchAgent{Dir: dir}
	if err := a.Start(launcher()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "com.boku.app.plist")); err != nil {
		t.Errorf("the launch agent was not written: %v", err)
	}
}

// A file this application did not write — or one that was damaged — is not a
// start of this application, and reading it must not fail the page.
func TestLaunchAgentReadsAForeignDocumentAsOff(t *testing.T) {
	a := agent(t)
	path := filepath.Join(a.Dir, "com.boku.app.plist")
	if err := os.WriteFile(path, []byte("not a property list"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if on, err := a.Read(launcher()); err != nil || on {
		t.Errorf("Read of a damaged document = %v, %v; want false, nil", on, err)
	}
}

// A login start with no identifier, or no program, is refused before anything
// is written: a file named "" or a document with no ProgramArguments would be
// a start that does nothing.
func TestLaunchAgentRefusesAnIncompleteStart(t *testing.T) {
	a := agent(t)
	if _, err := a.Document(Launcher{Exe: "/bin/boku"}); err == nil {
		t.Error("Document accepted a start with no identifier")
	}
	if _, err := a.Document(Launcher{Label: "com.boku.app"}); err == nil {
		t.Error("Document accepted a start with no program")
	}
}

// The document is replaced in one step, so a crash cannot leave launchd with
// half of a property list. What a second Start leaves behind is one file.
func TestLaunchAgentRewritesInPlace(t *testing.T) {
	a := agent(t)
	l := launcher()
	if err := a.Start(l); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if err := a.Start(l); err != nil {
		t.Fatalf("the second Start failed: %v", err)
	}
	entries, err := os.ReadDir(a.Dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("the folder has %v, want one file", names)
	}
}
