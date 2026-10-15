package logging

import (
	"context"
	"log/slog"

	"boku/internal/kit"
)

// redacting is the handler that keeps credentials out of the file.
//
// docs/security.md §5 says a credential never reaches a log, and AGENTS.md
// says it must pass `redact()` first. Trusting the call sites is not enough —
// one `slog.Warn("…", "error", err)` carrying a provider's own message is all
// it takes — so the scrubbing happens here, once, on the way to disk. What a
// credential looks like is kit's answer, not a second list kept here:
// "同一件事不允许在两地描述". The logger therefore depends on a pure-function
// package and not on the HTTP client that first needed the same rule.
type redacting struct{ next slog.Handler }

func (h *redacting) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *redacting) Handle(ctx context.Context, record slog.Record) error {
	return h.next.Handle(ctx, redactRecord(record))
}

// WithAttrs scrubs attributes attached to a logger as well: they never pass
// through Handle, so this is the only place they can be caught.
func (h *redacting) WithAttrs(attrs []slog.Attr) slog.Handler {
	cleaned := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		cleaned = append(cleaned, redactAttr(attr))
	}
	return &redacting{next: h.next.WithAttrs(cleaned)}
}

func (h *redacting) WithGroup(name string) slog.Handler {
	return &redacting{next: h.next.WithGroup(name)}
}

// redactRecord copies a record with its message and attributes scrubbed.
func redactRecord(record slog.Record) slog.Record {
	cleaned := slog.NewRecord(record.Time, record.Level, kit.RedactText(record.Message), record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		cleaned.AddAttrs(redactAttr(attr))
		return true
	})
	return cleaned
}

// redactAttr scrubs one attribute: a string under a name that means a
// credential, a string that carries one inside it, an error whose message may
// quote one, and the members of a group.
//
// Anything else — a number, a duration, a boolean — is left alone: only text
// can carry a credential.
func redactAttr(attr slog.Attr) slog.Attr {
	switch attr.Value.Kind() {
	case slog.KindString:
		attr.Value = slog.StringValue(kit.RedactField(attr.Key, attr.Value.String()))
	case slog.KindGroup:
		members := attr.Value.Group()
		cleaned := make([]slog.Attr, 0, len(members))
		for _, member := range members {
			cleaned = append(cleaned, redactAttr(member))
		}
		attr.Value = slog.GroupValue(cleaned...)
	case slog.KindAny:
		// `"error", err` is the common case: what reaches the file is the
		// error's message, so that is what has to be scrubbed.
		if err, ok := attr.Value.Any().(error); ok && err != nil {
			attr.Value = slog.StringValue(kit.RedactText(err.Error()))
		}
	}
	return attr
}
