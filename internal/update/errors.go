package update

import (
	"errors"
	"fmt"
	"io"

	"boku/internal/domain"
)

// unavailableFrom keeps the distinction a caller needs when a failure comes
// from the injected updater: a bad response stays one, and everything else is
// an update that could not be had. No URL is quoted.
func unavailableFrom(reason string, err error) error {
	if errors.Is(err, domain.ErrProviderBadResponse) {
		return fmt.Errorf("%w: %s: %v", domain.ErrProviderBadResponse, reason, err)
	}
	return fmt.Errorf("%w: %s: %v", domain.ErrUpdateUnavailable, semantic(reason), err)
}

var _ = io.Discard
