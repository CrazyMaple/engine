package log

import (
	"context"
	"log/slog"
)

// slogAdapter 将 slog.Logger 适配为引擎 Logger 接口
type slogAdapter struct {
	l *slog.Logger
}

// FromSlog 将标准库 slog.Logger 适配为引擎 Logger 接口
func FromSlog(l *slog.Logger) Logger {
	return &slogAdapter{l: l}
}

func (a *slogAdapter) Debug(msg string, kvs ...interface{}) {
	a.l.Debug(msg, toSlogAttrs(kvs)...)
}

func (a *slogAdapter) Info(msg string, kvs ...interface{}) {
	a.l.Info(msg, toSlogAttrs(kvs)...)
}

func (a *slogAdapter) Warn(msg string, kvs ...interface{}) {
	a.l.Warn(msg, toSlogAttrs(kvs)...)
}

func (a *slogAdapter) Error(msg string, kvs ...interface{}) {
	a.l.Error(msg, toSlogAttrs(kvs)...)
}

func (a *slogAdapter) With(kvs ...interface{}) Logger {
	return &slogAdapter{l: a.l.With(toSlogAttrs(kvs)...)}
}

func toSlogAttrs(kvs []interface{}) []interface{} {
	return kvs // slog 原生支持 key-value 交替传入
}

// engineHandler 将引擎 Logger 适配为 slog.Handler
type engineHandler struct {
	logger Logger
	attrs  []interface{}
}

// ToSlogHandler 将引擎 Logger 适配为 slog.Handler，用于与 slog 生态集成
func ToSlogHandler(l Logger) slog.Handler {
	return &engineHandler{logger: l}
}

func (h *engineHandler) Enabled(_ context.Context, level slog.Level) bool {
	switch {
	case level < slog.LevelInfo:
		return Enabled(LevelDebug)
	case level < slog.LevelWarn:
		return Enabled(LevelInfo)
	case level < slog.LevelError:
		return Enabled(LevelWarn)
	default:
		return Enabled(LevelError)
	}
}

func (h *engineHandler) Handle(_ context.Context, r slog.Record) error {
	kvs := make([]interface{}, 0, len(h.attrs)+r.NumAttrs()*2)
	kvs = append(kvs, h.attrs...)
	r.Attrs(func(a slog.Attr) bool {
		kvs = append(kvs, a.Key, a.Value.Any())
		return true
	})

	l := h.logger
	switch {
	case r.Level < slog.LevelInfo:
		l.Debug(r.Message, kvs...)
	case r.Level < slog.LevelWarn:
		l.Info(r.Message, kvs...)
	case r.Level < slog.LevelError:
		l.Warn(r.Message, kvs...)
	default:
		l.Error(r.Message, kvs...)
	}
	return nil
}

func (h *engineHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]interface{}, len(h.attrs), len(h.attrs)+len(attrs)*2)
	copy(newAttrs, h.attrs)
	for _, a := range attrs {
		newAttrs = append(newAttrs, a.Key, a.Value.Any())
	}
	return &engineHandler{logger: h.logger, attrs: newAttrs}
}

func (h *engineHandler) WithGroup(name string) slog.Handler {
	// 简化实现：将 group 作为前缀
	return &engineHandler{logger: h.logger.With("group", name), attrs: h.attrs}
}
