// This file holds the one thing every store has to do before touching a file:
// answer a canceled context with the context's error.
//
// It is here rather than copied into each store because the rule is a rule —
// "a canceled context is answered with context.Canceled, not with a half-done
// write" — and four stores following it should be following one
// implementation of it.
package kit

import "context"

// ContextErr returns the error of a context that is done, or nil.
//
// A nil context is not done: the stores are called through interfaces that
// always pass one, and a test that passes none should not be refused.
func ContextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
