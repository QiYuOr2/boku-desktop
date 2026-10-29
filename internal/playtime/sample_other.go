//go:build !windows

package playtime

import "boku/internal/platform/proc"

func canRecord() bool { return false }

func newSampler() (Sampler, error) { return nil, proc.ErrUnsupported }
