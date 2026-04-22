package middleware

import (
	"sync"
	"time"

	"engine/actor"
	"engine/log"
)

// RateLimitAction 限流动作
type RateLimitAction int

const (
	// RateLimitDrop 丢弃超限消息
	RateLimitDrop RateLimitAction = iota
	// RateLimitLog 记录日志但仍然处理
	RateLimitLog
)

// RateLimitConfig 限流配置
type RateLimitConfig struct {
	// Rate 每秒允许的消息数
	Rate float64
	// Burst 令牌桶容量（突发允许量）
	Burst int
	// Action 超限时的动作
	Action RateLimitAction
}

// TokenBucket 令牌桶限流器
type TokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	maxBurst float64
	rate     float64 // tokens per second
	lastTime time.Time
}

// NewTokenBucket 创建令牌桶
func NewTokenBucket(rate float64, burst int) *TokenBucket {
	return &TokenBucket{
		tokens:   float64(burst),
		maxBurst: float64(burst),
		rate:     rate,
		lastTime: time.Now(),
	}
}

// Allow 检查是否允许通过（消耗一个令牌）
func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastTime).Seconds()
	tb.lastTime = now

	// 补充令牌
	tb.tokens += elapsed * tb.rate
	if tb.tokens > tb.maxBurst {
		tb.tokens = tb.maxBurst
	}

	if tb.tokens < 1 {
		return false
	}

	tb.tokens--
	return true
}

// NewRateLimiter 创建 per-Actor 限流中间件
func NewRateLimiter(cfg RateLimitConfig) actor.ReceiverMiddleware {
	return func(next actor.Actor) actor.Actor {
		return &rateLimitActor{
			inner:  next,
			bucket: NewTokenBucket(cfg.Rate, cfg.Burst),
			action: cfg.Action,
		}
	}
}

type rateLimitActor struct {
	inner  actor.Actor
	bucket *TokenBucket
	action RateLimitAction
}

func (a *rateLimitActor) Receive(ctx actor.Context) {
	// 系统消息（生命周期）始终放行
	switch ctx.Message().(type) {
	case *actor.Started, *actor.Stopping, *actor.Stopped, *actor.Restarting:
		a.inner.Receive(ctx)
		return
	}

	if a.bucket.Allow() {
		a.inner.Receive(ctx)
		return
	}

	switch a.action {
	case RateLimitDrop:
		log.Debug("[ratelimit] dropped message %T for actor %s", ctx.Message(), ctx.Self())
	case RateLimitLog:
		log.Warn("[ratelimit] rate exceeded for actor %s, msg=%T", ctx.Self(), ctx.Message())
		a.inner.Receive(ctx)
	}
}

// ---- Gate 连接级限流 ----

// ConnectionRateLimiter 连接级限流器，每个连接独立限流
type ConnectionRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*TokenBucket
	rate    float64
	burst   int
}

// NewConnectionRateLimiter 创建连接级限流器
func NewConnectionRateLimiter(rate float64, burst int) *ConnectionRateLimiter {
	return &ConnectionRateLimiter{
		buckets: make(map[string]*TokenBucket),
		rate:    rate,
		burst:   burst,
	}
}

// Allow 检查指定连接 ID 是否允许通过
func (cl *ConnectionRateLimiter) Allow(connID string) bool {
	cl.mu.Lock()
	bucket, ok := cl.buckets[connID]
	if !ok {
		bucket = NewTokenBucket(cl.rate, cl.burst)
		cl.buckets[connID] = bucket
	}
	cl.mu.Unlock()

	return bucket.Allow()
}

// Remove 移除连接的限流器（连接关闭时调用）
func (cl *ConnectionRateLimiter) Remove(connID string) {
	cl.mu.Lock()
	delete(cl.buckets, connID)
	cl.mu.Unlock()
}

// Count 返回当前跟踪的连接数
func (cl *ConnectionRateLimiter) Count() int {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	return len(cl.buckets)
}
