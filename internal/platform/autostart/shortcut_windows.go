//go:build windows

package autostart

import (
	"os"
	"path/filepath"
	"strings"
)

// The shortcut the installer made is how Windows starts the application: a
// .lnk in the Start Menu or on the desktop whose target is the program the
// version's files live with. A login start has to name that program — not
// whatever copy happened to be running — or a start from the shortcut and a
// start at login are two installations, and an update reaches only one.

// CleanShortcutTarget trims what a shortcut stores around its target: the
// quotes around a path with spaces in it, and the arguments after it. The
// path itself is not otherwise rewritten.
func CleanShortcutTarget(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	if strings.HasPrefix(target, `"`) {
		rest := strings.TrimPrefix(target, `"`)
		if end := strings.Index(rest, `"`); end >= 0 {
			return strings.TrimSpace(rest[:end])
		}
		return strings.TrimSpace(rest)
	}
	// An unquoted command line ends its program at the first " -", which is
	// how a shortcut writes an argument onto a path with no space in it.
	if index := strings.Index(target, " -"); index >= 0 {
		target = target[:index]
	}
	return strings.TrimSpace(target)
}

// ShortcutTarget reads a .lnk file and returns the program it points at, or
// "" when it cannot be read. A shortcut this build does not understand — a
// damaged file, one naming a network path, one written by a shell this
// reader has never seen — answers "", and the caller falls back to the
// running program rather than writing a start it cannot account for.
func ShortcutTarget(link string) string {
	data, err := os.ReadFile(link)
	if err != nil {
		return ""
	}
	target, err := lnkTarget(data)
	if err != nil {
		return ""
	}
	return target
}

// applicationShortcutPath finds the shortcut the installer made: the Start
// Menu's Programs folder or the desktop, for this user or for everyone, in
// the order a person would look in them.
//
// The lookup is by the application's own name rather than by a path written
// down here: a person finds the application by the name they installed it
// under, and so does this.
func applicationShortcutPath() string {
	name := ApplicationName()
	if name == "" {
		return ""
	}
	for _, folder := range shortcutFolders() {
		path := filepath.Join(folder, name+".lnk")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// shortcutFolders are the places an installer puts a shortcut: the Start
// Menu's Programs folder and the desktop, for the current user and for every
// user of the machine.
//
// The four are spelled out from the environment rather than asked of the
// shell's known-folder API, which this build has no binding for; the paths
// are the same on every Windows since Vista, and the system's language does
// not change them even though it changes what the folders are called.
func shortcutFolders() []string {
	candidates := []string{
		filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs"),
		filepath.Join(os.Getenv("USERPROFILE"), "Desktop"),
		filepath.Join(os.Getenv("PROGRAMDATA"), "Microsoft", "Windows", "Start Menu", "Programs"),
		filepath.Join(os.Getenv("PUBLIC"), "Desktop"),
	}
	folders := make([]string, 0, len(candidates))
	for _, folder := range candidates {
		// An unset variable leaves filepath.Join with the rest of the path
		// alone, which would name a folder relative to the working
		// directory. Only a rooted folder is one of these four.
		if !filepath.IsAbs(folder) {
			continue
		}
		if !folderExists(folder) || containsFolder(folders, folder) {
			continue
		}
		folders = append(folders, folder)
	}
	return folders
}

// folderExists reports whether a folder is there.
func folderExists(folder string) bool {
	info, err := os.Stat(folder)
	return err == nil && info.IsDir()
}

// containsFolder reports whether a folder is already in the list, ignoring
// case as Windows does: the desktop of this user and of every user are the
// same folder on a machine with one account.
func containsFolder(folders []string, folder string) bool {
	for _, existing := range folders {
		if strings.EqualFold(existing, folder) {
			return true
		}
	}
	return false
}
