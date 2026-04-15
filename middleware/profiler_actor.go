package middleware

import (
	"sync"
	"sync/atomic"
	"time"

	"engine/actor"
)

// ActorProfileStats 单个 Actor 的消息处理耗时统计
type ActorProfileStats struct {
	PID          string  `json:"pid"`
	MessageCount int64   `json:"message_count"`
	TotalNs      int64   `json:"total_ns"`
	MinNs        int64   `json:"min_ns"`
	MaxNs        int64   `json:"max_ns"`
	AvgNs        float64 `json:"avg_ns"`
	// 耗时分布直方图桶：<1ms, 1-5ms, 5-10ms, 10-50ms, 50-100ms, >100ms
	Buckets [6]int64 `json:"buckets"`
}

// BucketLabels 直方图桶标签
var BucketLabels = [6]string{"<1ms", "1-5ms", "5-10ms", "10-50ms", "50-100ms", ">100ms"}

// ActorProfiler Actor 级别 Profiling
type ActorProfiler struct {
	mu         sync.RWMutex
	actorStats map[string]*ActorProfileStats
	enabled    int32
}

// NewActorProfiler 创建 Actor Profiler
func NewActorProfiler() *ActorProfiler {
	return &ActorProfiler{
		actorStats: make(map[string]*ActorProfileStats),
	}
}

// Enable 启用 Actor Profiling
func (ap *ActorProfiler) Enable() {
	atomic.StoreInt32(&ap.enabled, 1)
}

// Disable 关闭 Actor Profiling
func (ap *ActorProfiler) Disable() {
	atomic.StoreInt32(&ap.enabled, 0)
}

// IsEnabled 是否启用
func (ap *ActorProfiler) IsEnabled() bool {
	return atomic.LoadInt32(&ap.enabled) == 1
}

// Record 记录一次消息处理耗时
func (ap *ActorProfiler) Record(pid string, duration time.Duration) {
	if atomic.LoadInt32(&ap.enabled) == 0 {
		return
	}

	ns := duration.Nanoseconds()
	bucket := durationToBucket(duration)

	ap.mu.Lock()
	stats, ok := ap.actorStats[pid]
	if !ok {
		stats = &ActorProfileStats{
			PID:   pid,
			MinNs: ns,
		}
		ap.actorStats[pid] = stats
	}
	stats.MessageCount++
	stats.TotalNs += ns
	if ns < stats.MinNs {
		stats.MinNs = ns
	}
	if ns > stats.MaxNs {
		stats.MaxNs = ns
	}
	stats.Buckets[bucket]++
	ap.mu.Unlock()
}

// Stats 返回所有 Actor 的 Profiling 统计
func (ap *ActorProfiler) Stats() map[string]*ActorProfileStats {
	ap.mu.RLock()
	defer ap.mu.RUnlock()

	result := make(map[string]*ActorProfileStats, len(ap.actorStats))
	for pid, s := range ap.actorStats {
		cp := *s
		if cp.MessageCount > 0 {
			cp.AvgNs = float64(cp.TotalNs) / float64(cp.MessageCount)
		}
		result[pid] = &cp
	}
	return result
}

// StatsFor 返回指定 Actor 的统计
func (ap *ActorProfiler) StatsFor(pid string) *ActorProfileStats {
	ap.mu.RLock()
	defer ap.mu.RUnlock()

	s, ok := ap.actorStats[pid]
	if !ok {
		return nil
	}
	cp := *s
	if cp.MessageCount > 0 {
		cp.AvgNs = float64(cp.TotalNs) / float64(cp.MessageCount)
	}
	return &cp
}

// Reset 清空所有采集数据
func (ap *ActorProfiler) Reset() {
	ap.mu.Lock()
	ap.actorStats = make(map[string]*ActorProfileStats)
	ap.mu.Unlock()
}

// NewActorProfilingMiddleware 创建 Actor Profiling 中间件
func NewActorProfilingMiddleware(ap *ActorProfiler) actor.ReceiverMiddleware {
	return func(next actor.Actor) actor.Actor {
		return &actorProfilingActor{inner: next, profiler: ap}
	}
}

type actorProfilingActor struct {
	inner    actor.Actor
	profiler *ActorProfiler
}

func (a *actorProfilingActor) Receive(ctx actor.Context) {
	if !a.profiler.IsEnabled() {
		a.inner.Receive(ctx)
		return
	}
	start := time.Now()
	a.inner.Receive(ctx)
	a.profiler.Record(ctx.Self().String(), time.Since(start))
}

// durationToBucket 将耗时映射到直方图桶索引
func durationToBucket(d time.Duration) int {
	ms := d.Milliseconds()
	switch {
	case ms < 1:
		return 0 // <1ms
	case ms < 5:
		return 1 // 1-5ms
	case ms < 10:
		return 2 // 5-10ms
	case ms < 50:
		return 3 // 10-50ms
	case ms < 100:
		return 4 // 50-100ms
	default:
		return 5 // >100ms
	}
}
