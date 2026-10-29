//go:build !windows

package playtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenOnThisSystemDoesNotCreateADatabase(t *testing.T) {
	dir := t.TempDir()
	recorder := Open(dir, nil)
	if recorder.Available() {
		t.Fatal("recording should stay off where process snapshots are unavailable")
	}
	if _, err := os.Stat(filepath.Join(dir, dbFile)); !os.IsNotExist(err) {
		t.Fatal("playtime.sqlite was created")
	}
	if _, err := os.Stat(filepath.Join(dir, legacyFile)); !os.IsNotExist(err) {
		t.Fatal("playtime.json was touched")
	}
}
