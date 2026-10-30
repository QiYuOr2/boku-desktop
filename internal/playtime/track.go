package playtime

import (
	"sort"
	"strings"
	"time"

	"boku/internal/domain"
)

// sleepGap is how far the sleep clock has to run ahead of awake time before
// a gap is treated as sleep. A poll of a couple of seconds moves both clocks
// together; the noise between them is well under this.
const sleepGap = 2 * time.Second

// Tracker turns samples into sessions.
//
// It keeps every session it has accepted. Closing one does not drop it:
// the file and the charts read the same list.
type Tracker struct {
	sessions []Session
	at       time.Time
	awake    time.Duration
	biased   time.Duration
	seen     bool
}

// Restore replaces the sessions with ones read from disk.
func (t *Tracker) Restore(sessions []Session) {
	t.sessions = append([]Session(nil), sessions...)
}

// Sessions returns a copy of every session, open ones included.
func (t *Tracker) Sessions() []Session {
	return append([]Session(nil), t.sessions...)
}

// CloseAbandoned ends sessions that were still open when the process
// stopped. Each one ends at its own last look, not at the time of this
// call: the hours between a crash and the next launch are not play.
func (t *Tracker) CloseAbandoned() bool {
	changed := false
	for i := range t.sessions {
		if !t.sessions[i].EndedAt.IsZero() {
			continue
		}
		end := t.sessions[i].LastSeenAt.UTC().Truncate(time.Second)
		if end.IsZero() || end.Before(t.sessions[i].StartedAt) {
			end = t.sessions[i].StartedAt
		}
		t.sessions[i].EndedAt = end
		changed = true
	}
	return changed
}

// Touch moves the last look of every open session forward.
//
// The caller saves on its own heartbeat. Observe does not, or a game left
// open would rewrite the file on every poll.
func (t *Tracker) Touch(at time.Time) bool {
	at = at.UTC().Truncate(time.Second)
	moved := false
	for i := range t.sessions {
		if !t.sessions[i].EndedAt.IsZero() {
			continue
		}
		t.sessions[i].LastSeenAt = at
		moved = true
	}
	return moved
}

// Observe applies one sample. The bool is whether a session opened, closed
// or was split, which is when the file has to be written.
func (t *Tracker) Observe(sample Sample) (bool, error) {
	at := sample.At.UTC().Truncate(time.Second)
	if at.IsZero() {
		return false, nil
	}
	changed := false
	if t.seen && sample.Awake >= t.awake && sample.Biased >= t.biased {
		asleep := (sample.Biased - t.biased) - (sample.Awake - t.awake)
		if asleep >= sleepGap {
			cut := t.at.Add(sample.Awake - t.awake).UTC().Truncate(time.Second)
			if cut.Before(t.at) {
				cut = t.at
			}
			if t.closeAll(cut) {
				changed = true
			}
		}
	}

	for _, stop := range sample.Stopped {
		if t.closeApp(stop.AppID, stopAt(stop.At, at)) {
			changed = true
		}
	}

	open := map[string]int{}
	for i := range t.sessions {
		if t.sessions[i].EndedAt.IsZero() {
			open[t.sessions[i].AppID] = i
		}
	}
	present := map[string]Game{}
	for _, game := range sample.Games {
		if game.AppID == "" {
			continue
		}
		if prev, ok := present[game.AppID]; ok {
			if prev.Name == "" {
				prev.Name = game.Name
			}
			if !game.RunningSince.IsZero() && (prev.RunningSince.IsZero() || game.RunningSince.Before(prev.RunningSince)) {
				prev.RunningSince = game.RunningSince
			}
			present[game.AppID] = prev
			continue
		}
		present[game.AppID] = game
	}
	for id, game := range present {
		if i, ok := open[id]; ok {
			t.sessions[i].LastSeenAt = at
			if t.sessions[i].Name == "" && game.Name != "" {
				t.sessions[i].Name = game.Name
			}
			if mergeDevices(&t.sessions[i], sample.Devices) {
				changed = true
			}
			delete(open, id)
			continue
		}
		start := at
		if since := game.RunningSince.UTC().Truncate(time.Second); !since.IsZero() && !since.After(at) {
			last := t.lastEnd(id)
			if last.IsZero() || !since.Before(last) {
				start = since
			}
		}
		ident, err := domain.NewIDAt(start)
		if err != nil {
			return changed, err
		}
		session := Session{
			ID:         ident,
			AppID:      id,
			Name:       game.Name,
			StartedAt:  start,
			LastSeenAt: at,
		}
		mergeDevices(&session, sample.Devices)
		t.sessions = append(t.sessions, session)
		changed = true
	}
	for _, i := range open {
		t.sessions[i].EndedAt = at
		t.sessions[i].LastSeenAt = at
		changed = true
	}
	t.at = at
	t.awake = sample.Awake
	t.biased = sample.Biased
	t.seen = true
	return changed, nil
}

// deviceOrder is the order a session stores controller kinds in.
var deviceOrder = []string{"dualsense", "switch-pro", "xbox"}

func mergeDevices(session *Session, extra []string) bool {
	have := splitDevices(session.Devices)
	changed := false
	for _, name := range extra {
		if name == "" || have[name] {
			continue
		}
		have[name] = true
		changed = true
	}
	if !changed {
		return false
	}
	session.Devices = joinDevices(have)
	return true
}

func splitDevices(text string) map[string]bool {
	have := map[string]bool{}
	for _, name := range strings.Split(text, ",") {
		if name != "" {
			have[name] = true
		}
	}
	return have
}

func joinDevices(have map[string]bool) string {
	parts := make([]string, 0, len(have))
	for _, name := range deviceOrder {
		if have[name] {
			parts = append(parts, name)
			delete(have, name)
		}
	}
	rest := make([]string, 0, len(have))
	for name := range have {
		rest = append(rest, name)
	}
	sort.Strings(rest)
	return strings.Join(append(parts, rest...), ",")
}

func stopAt(exit, sample time.Time) time.Time {
	exit = exit.UTC().Truncate(time.Second)
	if exit.IsZero() {
		return sample
	}
	return exit
}

func (t *Tracker) closeAll(at time.Time) bool {
	closed := false
	for i := range t.sessions {
		if !t.sessions[i].EndedAt.IsZero() {
			continue
		}
		end := at
		if end.Before(t.sessions[i].StartedAt) {
			end = t.sessions[i].StartedAt
		}
		t.sessions[i].EndedAt = end
		t.sessions[i].LastSeenAt = end
		closed = true
	}
	return closed
}

func (t *Tracker) closeApp(appID string, at time.Time) bool {
	if appID == "" {
		return false
	}
	for i := range t.sessions {
		if t.sessions[i].AppID != appID || !t.sessions[i].EndedAt.IsZero() {
			continue
		}
		end := at
		if end.Before(t.sessions[i].StartedAt) {
			end = t.sessions[i].StartedAt
		}
		t.sessions[i].EndedAt = end
		t.sessions[i].LastSeenAt = end
		return true
	}
	return false
}

func (t *Tracker) lastEnd(appID string) time.Time {
	var last time.Time
	for _, session := range t.sessions {
		if session.AppID != appID || session.EndedAt.IsZero() {
			continue
		}
		if session.EndedAt.After(last) {
			last = session.EndedAt
		}
	}
	return last
}
