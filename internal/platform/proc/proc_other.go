//go:build !windows

package proc

import "time"

// Snapshot is unavailable off Windows.
func Snapshot() ([]Process, error) { return nil, ErrUnsupported }

// ReadClocks is unavailable off Windows.
func ReadClocks() (Clocks, error) { return Clocks{}, ErrUnsupported }

// ImagePath is unavailable off Windows.
func ImagePath(pid int) (string, error) { return "", ErrUnsupported }

// SteamAppID is unavailable off Windows.
func SteamAppID(pid int) (string, error) { return "", ErrUnsupported }

// SteamRoot is unavailable off Windows.
func SteamRoot() (string, error) { return "", ErrUnsupported }

// WatchExit is unavailable off Windows.
func WatchExit(pid int, created time.Time) error { return ErrUnsupported }

// TakeExit is unavailable off Windows.
func TakeExit(pid int, created time.Time) (time.Time, bool) { return time.Time{}, false }
