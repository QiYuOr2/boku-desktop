//go:build darwin

package autostart

import (
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LaunchAgent is the macOS Manager: a launch agent that starts the program at
// login.
//
// macOS has two ways to start an application when a user logs in, and this
// build uses the one that needs nothing from the user but the switch:
//
//   - A launch agent is a property list in ~/Library/LaunchAgents. launchd
//     reads the folder at login and starts what the documents there name. It
//     needs no dialog, no approval and no code signature, and it has been the
//     same file and the same format on every macOS this application runs on.
//   - A login item (SMAppService, macOS 13 and later) is the system's own
//     list in System Settings > General > Login Items. It is the friendlier
//     place — the user can see the application there and switch it off — but
//     only the system can write it, only from inside a signed and notarized
//     app bundle, and registering it the first time needs either a dialog or
//     a trip to that settings pane.
//
// The directory is a field rather than a lookup of the user's home on every
// call, so that a test writes into a temporary directory and never into the
// one it would then be logged in from.
type LaunchAgent struct {
	// Dir is where login starts live: ~/Library/LaunchAgents.
	Dir string
}

// NewLaunchAgent returns the manager over the user's LaunchAgents folder.
// Nothing is read or written until a switch is pressed.
func NewLaunchAgent() *LaunchAgent {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return &LaunchAgent{Dir: filepath.Join(home, "Library", "LaunchAgents")}
}

// Document is the launch agent: its file, and the property list launchd
// reads.
func (a *LaunchAgent) Document(l Launcher) (Document, error) {
	if strings.TrimSpace(l.Label) == "" {
		return Document{}, errors.New("autostart: the login start needs an identifier")
	}
	if strings.TrimSpace(l.Exe) == "" {
		return Document{}, errors.New("autostart: the login start needs a program")
	}
	return Document{
		Key:      filepath.Join(a.Dir, l.Label+".plist"),
		Contents: launchAgentPlist(l),
	}, nil
}

// Read reports whether the launch agent is there and names this program.
//
// A document that names another copy of the application reads as off: the
// start would launch a program that is not the one this login start was made
// for. launchd's own loaded state is not asked: the file is the
// configuration, and a file written in this session is loaded at the next
// login whether or not launchd has read it in this one.
func (a *LaunchAgent) Read(l Launcher) (bool, error) {
	doc, err := a.Document(l)
	if err != nil {
		return false, err
	}
	contents, err := os.ReadFile(doc.Key)
	if err != nil {
		if notExist(err) {
			return false, nil
		}
		return false, err
	}
	program, err := launchAgentProgram(contents)
	if err != nil {
		// A document this application did not write is not a login start of
		// this application, and it may be any shape at all.
		return false, nil
	}
	return program == l.Exe, nil
}

// Start writes the launch agent, replacing one that is already there:
// writing it again is how a document that names an older copy of the
// application is brought up to date.
func (a *LaunchAgent) Start(l Launcher) error {
	doc, err := a.Document(l)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(doc.Key), 0o755); err != nil {
		return fmt.Errorf("create the LaunchAgents folder: %w", err)
	}
	// 0644 is what launchd's own documents carry: it is read by launchd as
	// the user who logs in, which is the account that wrote it.
	return writeFileAtomically(doc.Key, doc.Contents, 0o644)
}

// Stop removes the launch agent. A document that is not there was never a
// start, so nothing is left behind — which is what the user asked for, and
// not a failure.
func (a *LaunchAgent) Stop(l Launcher) error {
	doc, err := a.Document(l)
	if err != nil {
		return err
	}
	if err := os.Remove(doc.Key); err != nil && !notExist(err) {
		return err
	}
	return nil
}

// BinaryPath is the program the launch agent runs.
//
// os.Executable is right here: inside a bundle it is Boku.app/Contents/MacOS/
// Boku, which is what a login start names and what an update replaces in
// place. A development build is the exception, and it is main that refuses
// it: it is not inside a bundle, and a login start naming a build directory
// would start something that will not be there.
func BinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate the executable: %w", err)
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return exe, nil
}

// launchAgentPlist is the document launchd reads.
//
// One program, one argument, started when the user logs in. The argument list
// is written one element per argument, so nothing in a path has to be escaped
// for a shell — which is what makes every macOS path safe, spaces and
// ampersands included.
//
// There is no KeepAlive: the agent is a login start, not a supervisor. An
// application the user quit stays quit until the next login.
func launchAgentPlist(l Launcher) []byte {
	var doc strings.Builder
	doc.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	doc.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	doc.WriteString(`<plist version="1.0">` + "\n")
	doc.WriteString("<dict>\n")
	writePlistString(&doc, "Label", l.Label)
	doc.WriteString("\t<key>ProgramArguments</key>\n")
	doc.WriteString("\t<array>\n")
	doc.WriteString("\t\t<string>" + escapeXML(l.Exe) + "</string>\n")
	if arg := strings.TrimSpace(l.Arg); arg != "" {
		doc.WriteString("\t\t<string>" + escapeXML(arg) + "</string>\n")
	}
	doc.WriteString("\t</array>\n")
	// RunAtLoad is a boolean in the property list, not the string "true":
	// launchd reads the value's type and does not start a document whose
	// RunAtLoad is a string. The document still reads as a start — the file
	// is there and names the program — so the switch would show on while the
	// next login did nothing.
	doc.WriteString("\t<key>RunAtLoad</key>\n")
	doc.WriteString("\t<true/>\n")
	// Interactive: the application draws a menu bar item, so launchd is told
	// it belongs to the user's session rather than to the background.
	writePlistString(&doc, "ProcessType", "Interactive")
	doc.WriteString("</dict>\n")
	doc.WriteString("</plist>\n")
	return []byte(doc.String())
}

// writePlistString writes one key and its string value, indented like the
// rest of the document.
func writePlistString(doc *strings.Builder, key, value string) {
	doc.WriteString("\t<key>" + escapeXML(key) + "</key>\n")
	doc.WriteString("\t<string>" + escapeXML(value) + "</string>\n")
}

// escapeXML escapes a value for the document. A path is allowed to contain an
// ampersand, and a name that closed a tag early would produce a document
// launchd refuses silently at the next login — not at this click.
func escapeXML(value string) string {
	var out strings.Builder
	_ = xml.EscapeText(&out, []byte(value))
	return out.String()
}

// launchAgentProgram reads the first program of a launch agent document:
// what launchd would run. A document this application did not write may be
// any shape, so a failed read is the caller's "not our start".
func launchAgentProgram(contents []byte) (string, error) {
	var document struct {
		Dict struct {
			Arrays []struct {
				Strings []string `xml:"string"`
			} `xml:"array"`
		} `xml:"dict"`
	}
	if err := xml.Unmarshal(contents, &document); err != nil {
		return "", err
	}
	for _, array := range document.Dict.Arrays {
		if len(array.Strings) > 0 {
			return array.Strings[0], nil
		}
	}
	return "", errNoProgram
}

// errNoProgram is a launch agent with no program in it.
var errNoProgram = errors.New("autostart: the launch agent has no program")

// writeFileAtomically replaces a file in one step: a temporary file beside
// it, then the rename. A property list half written at the moment of a crash
// is a start launchd cannot read, and the failure would show up as a login
// that silently did not happen.
func writeFileAtomically(path string, contents []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(contents); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		return err
	}
	return os.Rename(name, path)
}
