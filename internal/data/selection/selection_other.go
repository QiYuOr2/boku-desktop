//go:build !windows

package selection

import (
	"context"

	"boku/internal/domain"
)

// New returns the device for a platform with no selection API. There is no
// such interface on the other two: on macOS the accessibility API needs cgo
// or an Objective-C bridge this build does not have, and on Linux the answer
// lives on an accessibility bus that is not always running. Both fall back to
// the clipboard, which is what the tool did before this package existed.
func New() domain.SelectionDevice { return unsupported{} }

// unsupported is the honest answer: this platform cannot say.
type unsupported struct{}

func (unsupported) SelectedText(context.Context) (string, error) {
	return "", domain.ErrSelectionUnavailable
}
