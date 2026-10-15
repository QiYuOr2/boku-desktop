// Package atomicfile writes a file the way a configuration file has to be
// written: through a temporary file in the same directory, renamed into
// place.
//
// A reader therefore sees either the previous contents or the new ones, never
// half of either — which is what a settings file, a clipboard history or a
// credential file all need, since a crash mid-write must not cost the user
// what was there before.
//
// It holds no state and knows nothing about what is being written: the stores
// that use it name themselves in the error they wrap around it.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	// FileMode is what a written file is restricted to. On Windows this
	// permission bit does not decide access — an ACL does — so it is a
	// request rather than a boundary; see §4 of the security design.
	FileMode = 0o600
	// DirMode is what a directory created for a written file gets.
	DirMode = 0o700
)

// Write writes data to path, creating the directory if it is not there.
//
// The temporary file is created beside the destination rather than in the
// system temporary directory: a rename across file systems is a copy, and a
// copy is exactly the half-written file this exists to avoid.
func Write(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, DirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	temp, err := os.CreateTemp(dir, ".atomic-*.tmp")
	if err != nil {
		return fmt.Errorf("create a temporary file in %s: %w", dir, err)
	}
	name := temp.Name()
	defer func() {
		if name != "" {
			_ = os.Remove(name)
		}
	}()
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := temp.Chmod(FileMode); err != nil {
		_ = temp.Close()
		return fmt.Errorf("restrict %s: %w", name, err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("flush %s: %w", name, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", name, err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("move %s into place: %w", name, err)
	}
	name = ""
	return nil
}
