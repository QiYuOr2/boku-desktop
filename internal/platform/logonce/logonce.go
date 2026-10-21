// Package logonce keeps a repeated log line from filling the file.
//
// A failure that is noticed every frame — a shortcut that will not register,
// a clipboard write the system refuses — would otherwise write one record per
// frame, turning a small problem into a large file and a busy disk. This
// remembers the last record for each key and drops a repeat, so the first time
// is written and the same thing happening again is not.
//
// The rule is deliberately about the message and not about time: an identical
// record is the same fact, whether it happens twice in a millisecond or twice
// in an hour. A different error under the same key is a new fact and is
// written.
//
// It is a package of its own rather than a helper in the logging package,
// because the logging package is imported only by main.go (AGENTS.md): every
// other layer logs through the standard library's slog, and it would have to
// import main's way of reaching the file to get this. Here it depends on
// nothing but the standard library, which is what lets any layer use it — and
// the logging package itself must not, or a log that cannot be written would
// try to write about it.
package logonce

import "sync"

// seen holds the last record written under each key. A key is a short,
// fixed string the caller chooses — the name of the call site, not the error,
// so that every different failure of one call site replaces the one before.
var (
	mu   sync.Mutex
	seen = map[string]string{}
)

// Should reports whether key's message has changed since the last call, and
// remembers it. It is safe to call from several goroutines.
//
// A key that is written again after a different message — because the failure
// changed — is written again: what is dropped is the exact repetition.
func Should(key, message string) bool {
	mu.Lock()
	defer mu.Unlock()
	if previous, ok := seen[key]; ok && previous == message {
		return false
	}
	seen[key] = message
	return true
}

// Forget makes the next call for key write, whatever it says. It is for the
// caller that knows the situation resolved — a shortcut that registered, a
// clipboard that accepted a write — so that the same failure happening later
// is reported again rather than swallowed as a repeat.
func Forget(key string) {
	mu.Lock()
	defer mu.Unlock()
	delete(seen, key)
}

// Reset forgets every key. It is for tests, so that one case does not decide
// what another one sees.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	clear(seen)
}
