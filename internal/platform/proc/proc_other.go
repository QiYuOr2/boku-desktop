//go:build !windows

package proc

import "time"

func Snapshot() ([]Process, error) { return nil, ErrUnsupported }

func ReadClocks() (Clocks, error) { return Clocks{}, ErrUnsupported }

func ImagePath(pid int) (string, error) { return "", ErrUnsupported }

func SteamAppID(pid int) (string, error) { return "", ErrUnsupported }

func SteamRoot() (string, error) { return "", ErrUnsupported }

func WatchExit(pid int, created time.Time) error { return ErrUnsupported }

func TakeExit(pid int, created time.Time) (time.Time, bool) { return time.Time{}, false }
