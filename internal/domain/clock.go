package domain

import "time"

// Clock is where the application reads the time, so that a test can decide
// it.
//
// It is a port and it lives with the other ports: two features need it — the
// clipboard's history stamps what it records, and the translation tool stamps
// what it answers — and neither of them should have to import the other to
// get one.
type Clock interface {
	Now() time.Time
}

// SystemClock is the clock the application runs on: wall time, in UTC, the
// way §11 of the domain model stores it.
type SystemClock struct{}

// Now returns the current time in UTC. An implementation that rounds it does
// so for its own reasons; this one does not.
func (SystemClock) Now() time.Time { return time.Now().UTC() }
