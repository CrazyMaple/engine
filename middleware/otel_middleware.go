package middleware

import "engine/actor"

// NewOTelTracing 创建基于 OTel 追踪器的 Actor 中间件
// 为每条消息创建 Span，并通过 Envelope TraceID 传播上下文
func NewOTelTracing(tracer Tracer) actor.ReceiverMiddleware {
	return func(next actor.Actor) actor.Actor {
		return &otelTracingActor{inner: next, tracer: tracer}
	}
}

type otelTracingActor struct {
	inner  actor.Actor
	tracer Tracer
}

func (a *otelTracingActor) Receive(ctx actor.Context) {
	operationName := msgTypeName(ctx.Message())

	var span Span
	traceID := ctx.TraceID()
	if traceID != "" {
		// 从传播上下文恢复
		prop := TracePropagation{TraceParent: traceID}
		span, _ = a.tracer.StartFromPropagation(operationName, prop,
			WithSpanKind(SpanKindServer),
			WithAttributes(map[string]interface{}{
				"actor.pid": ctx.Self().String(),
			}),
		)
	} else {
		span, _ = a.tracer.Start(operationName,
			WithSpanKind(SpanKindServer),
			WithAttributes(map[string]interface{}{
				"actor.pid": ctx.Self().String(),
			}),
		)
	}

	defer func() {
		if r := recover(); r != nil {
			span.SetStatus(SpanStatusError, "panic")
			span.End()
			panic(r) // 重新抛出 panic
		}
		span.SetStatus(SpanStatusOK, "")
		span.End()
	}()

	if sender := ctx.Sender(); sender != nil {
		span.SetAttribute("actor.sender", sender.String())
	}

	a.inner.Receive(ctx)
}
