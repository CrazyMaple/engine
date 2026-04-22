package dashboard

import (
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"engine/actor"
)

// ActorStats Actor 统计数据
type ActorStats struct {
	PID         string    `json:"pid"`
	MsgCount    int64     `json:"msg_count"`
	TotalLatNs  int64     `json:"total_latency_ns"`
	AvgLatNs    int64     `json:"avg_latency_ns"`
	LastMsgTime time.Time `json:"last_msg_time"`
}

// actorCounter 单个 Actor 的原子计数器
type actorCounter struct {
	msgCount   int64
	totalLatNs int64
	lastMsg    int64 // UnixNano
}

// HotActorTracker 热点 Actor 追踪器
type HotActorTracker struct {
	counters map[string]*actorCounter
	mu       sync.RWMutex
}

// NewHotActorTracker 创建热点 Actor 追踪器
func NewHotActorTracker() *HotActorTracker {
	return &HotActorTracker{
		counters: make(map[string]*actorCounter),
	}
}

// Record 记录一次 Actor 消息处理
func (t *HotActorTracker) Record(pid string, latency time.Duration) {
	counter := t.getOrCreate(pid)
	atomic.AddInt64(&counter.msgCount, 1)
	atomic.AddInt64(&counter.totalLatNs, int64(latency))
	atomic.StoreInt64(&counter.lastMsg, time.Now().UnixNano())
}

func (t *HotActorTracker) getOrCreate(pid string) *actorCounter {
	t.mu.RLock()
	c, ok := t.counters[pid]
	t.mu.RUnlock()
	if ok {
		return c
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if c, ok := t.counters[pid]; ok {
		return c
	}
	c = &actorCounter{}
	t.counters[pid] = c
	return c
}

// TopN 返回消息量最多的前 N 个 Actor
func (t *HotActorTracker) TopN(n int) []*ActorStats {
	t.mu.RLock()
	stats := make([]*ActorStats, 0, len(t.counters))
	for pid, c := range t.counters {
		count := atomic.LoadInt64(&c.msgCount)
		lat := atomic.LoadInt64(&c.totalLatNs)
		lastMsg := atomic.LoadInt64(&c.lastMsg)
		avgLat := int64(0)
		if count > 0 {
			avgLat = lat / count
		}
		stats = append(stats, &ActorStats{
			PID:         pid,
			MsgCount:    count,
			TotalLatNs:  lat,
			AvgLatNs:    avgLat,
			LastMsgTime: time.Unix(0, lastMsg),
		})
	}
	t.mu.RUnlock()

	sort.Slice(stats, func(i, j int) bool {
		return stats[i].MsgCount > stats[j].MsgCount
	})

	if n > 0 && len(stats) > n {
		stats = stats[:n]
	}
	return stats
}

// Reset 重置所有统计
func (t *HotActorTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.counters = make(map[string]*actorCounter)
}

// hotActorMiddlewareActor 热点追踪中间件
type hotActorMiddlewareActor struct {
	inner   actor.Actor
	tracker *HotActorTracker
}

// NewHotActorMiddleware 创建热点 Actor 追踪中间件
func NewHotActorMiddleware(tracker *HotActorTracker) actor.ReceiverMiddleware {
	return func(next actor.Actor) actor.Actor {
		return &hotActorMiddlewareActor{inner: next, tracker: tracker}
	}
}

func (a *hotActorMiddlewareActor) Receive(ctx actor.Context) {
	// 系统生命周期消息不追踪
	switch ctx.Message().(type) {
	case *actor.Started, *actor.Stopping, *actor.Stopped, *actor.Restarting:
		a.inner.Receive(ctx)
		return
	}

	_ = reflect.TypeOf(ctx.Message()).String() // 确保类型名已解析
	start := time.Now()
	a.inner.Receive(ctx)
	elapsed := time.Since(start)

	a.tracker.Record(ctx.Self().String(), elapsed)
}
