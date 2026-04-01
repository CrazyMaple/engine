package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"engine/actor"
	"engine/log"
)

// TraceContext 追踪上下文
type TraceContext struct {
	TraceID   string
	SpanID    string
	ParentID  string
	StartTime time.Time
}

// NewTraceContext 创建新的追踪上下文
func NewTraceContext() *TraceContext {
	return &TraceContext{
		TraceID:   generateID(),
		SpanID:    generateID(),
		StartTime: time.Now(),
	}
}

// NewChildSpan 创建子 Span
func (tc *TraceContext) NewChildSpan() *TraceContext {
	return &TraceContext{
		TraceID:   tc.TraceID,
		SpanID:    generateID(),
		ParentID:  tc.SpanID,
		StartTime: time.Now(),
	}
}

// Traceable 可追踪消息接口
type Traceable interface {
	GetTrace() *TraceContext
}

// TracedMessage 包装任意消息添加追踪信息
type TracedMessage struct {
	Inner interface{}
	Trace *TraceContext
}

func (tm *TracedMessage) GetTrace() *TraceContext {
	return tm.Trace
}

// WithTrace 为消息附加追踪上下文
func WithTrace(msg interface{}, trace *TraceContext) *TracedMessage {
	return &TracedMessage{Inner: msg, Trace: trace}
}

type tracingActor struct {
	inner actor.Actor
}

// NewTracing 创建追踪中间件
func NewTracing() actor.ReceiverMiddleware {
	return func(next actor.Actor) actor.Actor {
		return &tracingActor{inner: next}
	}
}

func (a *tracingActor) Receive(ctx actor.Context) {
	msg := ctx.Message()
	if t, ok := msg.(Traceable); ok {
		trace := t.GetTrace()
		if trace != nil {
			log.Debug("[trace] traceID=%s spanID=%s parentID=%s actor=%s",
				trace.TraceID, trace.SpanID, trace.ParentID, ctx.Self())
		}
	}
	a.inner.Receive(ctx)
}

func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
