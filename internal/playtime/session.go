// Package playtime records how long each Steam game stayed running.
//
// A session is one continuous run of one game: the instant it started and
// the instant the last of its processes left. A day, an hour and a minute
// are not stored. They are slices of these sessions, so a chart can ask for
// any of them later without the recorder having sampled that often.
package playtime

import "time"

// Session is one continuous run of one game.
//
// EndedAt is zero while the game is still running. LastSeenAt is how far
// the recorder is willing to claim after a crash: the next start closes an
// abandoned session there, and does not stretch it to "now".
type Session struct {
	ID         string
	AppID      string
	Name       string
	StartedAt  time.Time
	EndedAt    time.Time
	LastSeenAt time.Time
	// Devices lists the controllers connected while this run was open.
	// Empty means none were seen. A pad that disconnects stays: this is what
	// was used, not what is plugged in at the end.
	Devices string
}

// Game is a Steam game the sampler can see right now.
//
// RunningSince is the earliest creation time of the processes that make up
// the game. The tracker uses it to start a session at the real creation
// time when it notices the game late, and refuses it when that time falls
// inside a session already closed — a crash hole, or a sleep, stays empty.
type Game struct {
	AppID        string
	Name         string
	RunningSince time.Time
}

// Stop is the last process of a game leaving. At is the exit itself when
// the operating system reported it, and zero when the recorder only noticed
// the process missing at the next look.
type Stop struct {
	AppID string
	At    time.Time
}

// Sample is one look at the machine.
//
// Awake and Biased share an origin and count since boot. Awake pauses while
// the machine is asleep; Biased does not. Both zero means this system could
// not say, and the tracker then does not try to cut sleep out.
type Sample struct {
	At      time.Time
	Awake   time.Duration
	Biased  time.Duration
	Games   []Game
	Stopped []Stop
	// Devices is the controllers connected at this look. The tracker keeps
	// every distinct kind it sees on an open session.
	Devices []string
}
