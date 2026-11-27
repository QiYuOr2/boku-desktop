package logging

import (
	"context"
	"log/slog"

	"boku/internal/kit"
)

// redacting 是防止凭据进入文件的处理器。
//
// 安全设计 §5 规定凭据绝不进入日志，AGENTS.md 要求先经 redact()。仅靠调用点自觉不够：
// 一处 `slog.Warn("…", "error", err)` 带上厂商的错误消息就够了，因此在写盘路径上统一
// 脱敏一次。凭据长什么样由 kit 决定，不在这里另维护一份清单（同一件事不应在两处描述）。
// 因此日志器只依赖纯函数包，而不依赖最早需要同一规则的 HTTP 客户端。
type redacting struct{ next slog.Handler }

func (h *redacting) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *redacting) Handle(ctx context.Context, record slog.Record) error {
	return h.next.Handle(ctx, redactRecord(record))
}

// WithAttrs 也脱敏挂在日志器上的属性：它们不经过 Handle，只能在这里拦截。
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

// redactRecord 复制一条记录，脱敏其消息与属性。
func redactRecord(record slog.Record) slog.Record {
	cleaned := slog.NewRecord(record.Time, record.Level, kit.RedactText(record.Message), record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		cleaned.AddAttrs(redactAttr(attr))
		return true
	})
	return cleaned
}

// redactAttr 脱敏单个属性：以凭据为名的字符串、内部含凭据的字符串、消息可能引用凭据的
// error，以及 group 的成员。
//
// 其它类型（数字、时长、布尔）保持原样：只有文本才可能携带凭据。
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
		// `"error", err` 是常见情形：进入文件的是错误消息，所以须脱敏的是它。
		if err, ok := attr.Value.Any().(error); ok && err != nil {
			attr.Value = slog.StringValue(kit.RedactText(err.Error()))
		}
	}
	return attr
}
