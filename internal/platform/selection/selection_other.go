//go:build !windows && !darwin

package selection

import (
	"context"

	"boku/internal/domain"
)

// New returns the device for a platform with no selection API. Linux's answer
// lives on an accessibility bus that is not always running, so it falls back
// to the clipboard, which is what the tool did before this package existed.
//
// The clipboard in the options is not used here: the copy fallback needs a
// platform key press and a clipboard write counter, and this build has
// neither.
func New(Options) domain.SelectionDevice { return unsupported{} }

// unsupported is the honest answer: this platform cannot say.
type unsupported struct{}

func (unsupported) SelectedText(context.Context) (string, error) {
	return "", domain.ErrSelectionUnavailable
}
