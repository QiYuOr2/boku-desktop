// Package proc lists processes for the playtime recorder.
//
// The statistics themselves do not live here. This package only asks the
// operating system who is running, when that process was created, and, on
// Windows, when it exits. Other systems have no implementation: the recorder
// stays quiet there instead of inventing a session.
package proc

import (
	"errors"
	"time"
)

// ErrUnsupported is returned where the operating system cannot supply the
// snapshot the recorder needs.
var ErrUnsupported = errors.New("process snapshots are only available on windows")

// Process is one running process. Exe is empty when the process could not be
// opened; Name is still the file name from the process list.
type Process struct {
	PID       int
	PPID      int
	Name      string
	Exe       string
	CreatedAt time.Time
}

// Clocks is time since boot. Awake does not include sleep. Biased does.
// The recorder uses the difference to cut a session when the machine slept,
// instead of counting the hours the lid was closed.
type Clocks struct {
	Awake  time.Duration
	Biased time.Duration
}

// Exit is the moment a watched process left, as wall time.
type Exit struct {
	PID       int
	CreatedAt time.Time
	At        time.Time
}
