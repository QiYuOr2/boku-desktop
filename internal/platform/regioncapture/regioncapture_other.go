//go:build !windows && !darwin

package regioncapture

import (
	"context"
	"fmt"
	"image"
)

func (*Device) Capture(context.Context, image.Rectangle) ([]byte, error) {
	return nil, fmt.Errorf("region capture: unsupported platform")
}
