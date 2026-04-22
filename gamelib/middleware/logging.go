package middleware

import (
	"fmt"
	"reflect"
	"time"

	"engine/actor"
	"engine/log"
)

type loggingActor struct {
	inner actor.Actor
}

// NewLogging 创建日志中间件，记录消息类型和处理耗时
func NewLogging() actor.ReceiverMiddleware {
	return func(next actor.Actor) actor.Actor {
		return &loggingActor{inner: next}
	}
}

func (a *loggingActor) Receive(ctx actor.Context) {
	msg := ctx.Message()
	msgType := fmt.Sprintf("%s", reflect.TypeOf(msg))

	// 跳过生命周期系统消息的详细日志
	switch msg.(type) {
	case *actor.Started, *actor.Stopping, *actor.Stopped, *actor.Restarting:
		log.Debug("[middleware] actor=%s msg=%s", ctx.Self(), msgType)
		a.inner.Receive(ctx)
		return
	}

	start := time.Now()
	a.inner.Receive(ctx)
	elapsed := time.Since(start)

	log.Debug("[middleware] actor=%s msg=%s elapsed=%v", ctx.Self(), msgType, elapsed)
}
