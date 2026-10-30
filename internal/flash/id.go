package flash

import (
	"time"

	"boku/internal/domain"
)

func newID(now time.Time) (string, error) {
	return domain.NewIDAt(now)
}
