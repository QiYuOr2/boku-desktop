//go:build windows

package autostart

import (
	"os"
	"path/filepath"
	"strings"
)

// ApplicationName is the name the application is installed as — the name of
// the Start Menu shortcut, the name in the Run key, and the name a person
// reads in Task Manager.
//
// It is the running program's own name, without its extension, rather than a
// constant: the development build is called "Boku Dev", and a login start for
// it must not share the installed application's Run value.
func ApplicationName() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	name := filepath.Base(exe)
	return strings.TrimSuffix(name, filepath.Ext(name))
}
