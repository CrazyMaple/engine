package middleware

import (
	"fmt"
	"io"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"engine/actor"
)

// Metrics 指标收集器，线程安全，可跨多个 actor 共享
type Metrics struct {
	mu           sync.RWMutex
	msgCount     map[string]*int64 // 消息类型 → 计数
	totalLatency map[string]*int64 // 消息类型 → 累计纳秒
	registry     *MetricsRegistry  // 可选的指标注册中心
}

// MetricsSnapshot 指标快照
type MetricsSnapshot struct {
	MsgCount     map[string]int64
	TotalLatency map[string]int64
}

// NewMetrics 创建指标收集器
func NewMetrics() *Metrics {
	return &Metrics{
		msgCount:     make(map[string]*int64),
		totalLatency: make(map[string]*int64),
	}
}

func (m *Metrics) getCounters(typeName string) (*int64, *int64) {
	m.mu.RLock()
	count, ok1 := m.msgCount[typeName]
	latency, ok2 := m.totalLatency[typeName]
	m.mu.RUnlock()

	if ok1 && ok2 {
		return count, latency
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.msgCount[typeName]; !ok {
		v := int64(0)
		m.msgCount[typeName] = &v
	}
	if _, ok := m.totalLatency[typeName]; !ok {
		v := int64(0)
		m.totalLatency[typeName] = &v
	}
	return m.msgCount[typeName], m.totalLatency[typeName]
}

// SetRegistry 设置指标注册中心，record 时同步写入 registry
func (m *Metrics) SetRegistry(r *MetricsRegistry) {
	m.mu.Lock()
	m.registry = r
	m.mu.Unlock()
}

func (m *Metrics) record(typeName string, elapsed time.Duration) {
	count, latency := m.getCounters(typeName)
	atomic.AddInt64(count, 1)
	atomic.AddInt64(latency, int64(elapsed))

	// 同步写入 registry
	m.mu.RLock()
	reg := m.registry
	m.mu.RUnlock()
	if reg != nil {
		labels := map[string]string{"type": typeName}
		reg.IncCounter("engine_actor_message_total",
			"Total messages processed by type", labels, 1)
		reg.IncCounter("engine_actor_message_duration_nanoseconds",
			"Total processing duration in nanoseconds by type", labels, int64(elapsed))
	}
}

// Snapshot 返回当前指标快照
func (m *Metrics) Snapshot() MetricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snap := MetricsSnapshot{
		MsgCount:     make(map[string]int64, len(m.msgCount)),
		TotalLatency: make(map[string]int64, len(m.totalLatency)),
	}
	for k, v := range m.msgCount {
		snap.MsgCount[k] = atomic.LoadInt64(v)
	}
	for k, v := range m.totalLatency {
		snap.TotalLatency[k] = atomic.LoadInt64(v)
	}
	return snap
}

// Reset 重置所有指标
func (m *Metrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgCount = make(map[string]*int64)
	m.totalLatency = make(map[string]*int64)
}

// WritePrometheus 输出 Prometheus 文本格式指标
func (m *Metrics) WritePrometheus(w io.Writer) {
	snap := m.Snapshot()

	types := make([]string, 0, len(snap.MsgCount))
	for t := range snap.MsgCount {
		types = append(types, t)
	}
	sort.Strings(types)

	fmt.Fprintln(w, "# HELP actor_message_count Total messages processed by type")
	fmt.Fprintln(w, "# TYPE actor_message_count counter")
	for _, t := range types {
		fmt.Fprintf(w, "actor_message_count{type=%q} %d\n", t, snap.MsgCount[t])
	}

	fmt.Fprintln(w, "# HELP actor_message_latency_ns Total latency in nanoseconds by type")
	fmt.Fprintln(w, "# TYPE actor_message_latency_ns counter")
	for _, t := range types {
		fmt.Fprintf(w, "actor_message_latency_ns{type=%q} %d\n", t, snap.TotalLatency[t])
	}
}

type metricsActor struct {
	inner   actor.Actor
	metrics *Metrics
}

// NewMetricsMiddleware 创建指标中间件
func NewMetricsMiddleware(m *Metrics) actor.ReceiverMiddleware {
	return func(next actor.Actor) actor.Actor {
		return &metricsActor{inner: next, metrics: m}
	}
}

func (a *metricsActor) Receive(ctx actor.Context) {
	typeName := reflect.TypeOf(ctx.Message()).String()
	start := time.Now()
	a.inner.Receive(ctx)
	a.metrics.record(typeName, time.Since(start))
}
