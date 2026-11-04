//go:build !darwin && !windows

package loopback

import "context"

// Start reports that this build has no capture. Linux is out of scope.
func Start(ctx context.Context, source string) (*Stream, error) {
	return nil, ErrUnsupported
}
