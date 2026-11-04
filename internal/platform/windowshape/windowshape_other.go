//go:build !windows

package windowshape

func clipTop(uintptr, int) bool { return true }
