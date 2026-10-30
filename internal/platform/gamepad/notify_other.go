//go:build !windows

package gamepad

func listenHID(func()) (stop func(), ok bool) { return nil, false }
