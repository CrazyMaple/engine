package middleware

import "engine/actor"

// Chain 将多个中间件组合为一个，第一个中间件在最外层
func Chain(mws ...actor.ReceiverMiddleware) actor.ReceiverMiddleware {
	return func(next actor.Actor) actor.Actor {
		for i := len(mws) - 1; i >= 0; i-- {
			next = mws[i](next)
		}
		return next
	}
}
