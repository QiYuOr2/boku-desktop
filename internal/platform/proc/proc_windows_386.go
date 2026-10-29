//go:build windows && !amd64

package proc

import "time"

func Snapshot() ([]Process, error)              { return nil, ErrUnsupported }
func ReadClocks() (Clocks, error)               { return Clocks{}, ErrUnsupported }
func ImagePath(int) (string, error)             { return "", ErrUnsupported }
func SteamAppID(int) (string, error)            { return "", ErrUnsupported }
func SteamRoot() (string, error)                { return "", ErrUnsupported }
func WatchExit(int, time.Time) error            { return ErrUnsupported }
func TakeExit(int, time.Time) (time.Time, bool) { return time.Time{}, false }
