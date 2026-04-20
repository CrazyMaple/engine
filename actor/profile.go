package actor

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// --- 热点 Actor 自动画像 ---
// 每个 Actor 维护一个固定大小的环形耗时采样缓冲区，
// 按需计算 P50/P95/P99 并根据阈值自动识别热点 Actor。
// 与 dashboard/hotactor.go 的累计计数器、middleware/profiler_actor.go 的直方图桶互补：
// 此处关注"近期滑动窗口"的延迟分位数，用于迁移候选判断。

// HotActorProfilerConfig 热点 Actor 画像配置
type HotActorProfilerConfig struct {
	// WindowSize 每个 Actor 保留的最近样本数（ring buffer 大小）
	// 默认 512，越大越稳定但内存占用越高
	WindowSize int
	// HotP99Threshold 标记为热点 Actor 的 P99 阈值
	// 默认 50ms；P99 >= 阈值则 IsHot=true
	HotP99Threshold time.Duration
	// MinSamples 判定热点前要求的最小样本数
	// 默认 20；样本数不足时 IsHot 始终为 false
	MinSamples int
}

// HotActorProfileSnapshot 单个 Actor 的滑动窗口快照
type HotActorProfileSnapshot struct {
	PID        string        `json:"pid"`
	Samples    int           `json:"samples"`
	MsgCount   int64         `json:"msg_count"`
	LastActive time.Time     `json:"last_active"`
	MeanNs     float64       `json:"mean_ns"`
	P50Ns      float64       `json:"p50_ns"`
	P95Ns      float64       `json:"p95_ns"`
	P99Ns      float64       `json:"p99_ns"`
	MaxNs      int64         `json:"max_ns"`
	IsHot      bool          `json:"is_hot"`
	Window     time.Duration `json:"window_duration"`
}

type actorRing struct {
	mu       sync.Mutex
	samples  []int64
	next     int
	filled   bool
	msgCount int64
	first    time.Time
	last     time.Time
}

func newActorRing(size int) *actorRing {
	return &actorRing{samples: make([]int64, size)}
}

func (r *actorRing) record(ns int64, ts time.Time) {
	r.mu.Lock()
	r.samples[r.next] = ns
	r.next++
	if r.next >= len(r.samples) {
		r.next = 0
		r.filled = true
	}
	if r.msgCount == 0 {
		r.first = ts
	}
	r.last = ts
	r.msgCount++
	r.mu.Unlock()
}

func (r *actorRing) snapshot() (copyBuf []int64, msgCount int64, first, last time.Time) {
	r.mu.Lock()
	n := r.next
	if r.filled {
		n = len(r.samples)
	}
	copyBuf = make([]int64, n)
	if r.filled {
		copy(copyBuf, r.samples)
	} else {
		copy(copyBuf, r.samples[:n])
	}
	msgCount = r.msgCount
	first = r.first
	last = r.last
	r.mu.Unlock()
	return
}

// HotActorProfiler 热点 Actor 画像采集器
type HotActorProfiler struct {
	cfg     HotActorProfilerConfig
	mu      sync.RWMutex
	rings   map[string]*actorRing
	enabled int32
}

// NewHotActorProfiler 创建热点 Actor 画像采集器
func NewHotActorProfiler(cfg HotActorProfilerConfig) *HotActorProfiler {
	if cfg.WindowSize <= 0 {
		cfg.WindowSize = 512
	}
	if cfg.HotP99Threshold <= 0 {
		cfg.HotP99Threshold = 50 * time.Millisecond
	}
	if cfg.MinSamples <= 0 {
		cfg.MinSamples = 20
	}
	return &HotActorProfiler{cfg: cfg, rings: make(map[string]*actorRing)}
}

// Enable 启用采集
func (p *HotActorProfiler) Enable() { atomic.StoreInt32(&p.enabled, 1) }

// Disable 关闭采集
func (p *HotActorProfiler) Disable() { atomic.StoreInt32(&p.enabled, 0) }

// IsEnabled 是否启用
func (p *HotActorProfiler) IsEnabled() bool { return atomic.LoadInt32(&p.enabled) == 1 }

// Config 返回当前配置副本
func (p *HotActorProfiler) Config() HotActorProfilerConfig { return p.cfg }

// Record 记录一次 Actor 消息处理耗时
func (p *HotActorProfiler) Record(pid string, d time.Duration) {
	if atomic.LoadInt32(&p.enabled) == 0 {
		return
	}
	p.mu.RLock()
	r, ok := p.rings[pid]
	p.mu.RUnlock()
	if !ok {
		p.mu.Lock()
		if r, ok = p.rings[pid]; !ok {
			r = newActorRing(p.cfg.WindowSize)
			p.rings[pid] = r
		}
		p.mu.Unlock()
	}
	r.record(int64(d), time.Now())
}

// Reset 清空采集数据
func (p *HotActorProfiler) Reset() {
	p.mu.Lock()
	p.rings = make(map[string]*actorRing)
	p.mu.Unlock()
}

// SnapshotFor 返回指定 PID 的快照；PID 不存在时返回 nil
func (p *HotActorProfiler) SnapshotFor(pid string) *HotActorProfileSnapshot {
	p.mu.RLock()
	r, ok := p.rings[pid]
	p.mu.RUnlock()
	if !ok {
		return nil
	}
	return p.buildSnapshot(pid, r)
}

// Snapshots 返回所有 Actor 的快照
func (p *HotActorProfiler) Snapshots() []HotActorProfileSnapshot {
	p.mu.RLock()
	rings := make(map[string]*actorRing, len(p.rings))
	for k, v := range p.rings {
		rings[k] = v
	}
	p.mu.RUnlock()

	out := make([]HotActorProfileSnapshot, 0, len(rings))
	for pid, r := range rings {
		out = append(out, *p.buildSnapshot(pid, r))
	}
	return out
}

// TopN 返回按 P99 降序排序的前 N 个 Actor 快照
// onlyHot=true 时仅返回 IsHot=true 的 Actor
func (p *HotActorProfiler) TopN(n int, onlyHot bool) []HotActorProfileSnapshot {
	snaps := p.Snapshots()
	if onlyHot {
		filtered := snaps[:0]
		for _, s := range snaps {
			if s.IsHot {
				filtered = append(filtered, s)
			}
		}
		snaps = filtered
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].P99Ns > snaps[j].P99Ns })
	if n > 0 && len(snaps) > n {
		snaps = snaps[:n]
	}
	return snaps
}

// MigrationCandidates 返回迁移候选 Actor 列表（所有 IsHot=true 的 Actor，按 P99 降序）
// 为上层集群管理器提供迁移决策的原始数据；是否真正迁移由调用方决定
func (p *HotActorProfiler) MigrationCandidates() []HotActorProfileSnapshot {
	return p.TopN(0, true)
}

func (p *HotActorProfiler) buildSnapshot(pid string, r *actorRing) *HotActorProfileSnapshot {
	buf, msgCount, first, last := r.snapshot()
	snap := &HotActorProfileSnapshot{
		PID:        pid,
		Samples:    len(buf),
		MsgCount:   msgCount,
		LastActive: last,
	}
	if !first.IsZero() && !last.IsZero() {
		snap.Window = last.Sub(first)
	}
	if len(buf) == 0 {
		return snap
	}
	sorted := make([]int64, len(buf))
	copy(sorted, buf)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var sum int64
	for _, v := range sorted {
		sum += v
	}
	snap.MeanNs = float64(sum) / float64(len(sorted))
	snap.MaxNs = sorted[len(sorted)-1]
	snap.P50Ns = percentileAt(sorted, 0.50)
	snap.P95Ns = percentileAt(sorted, 0.95)
	snap.P99Ns = percentileAt(sorted, 0.99)
	if snap.Samples >= p.cfg.MinSamples && snap.P99Ns >= float64(p.cfg.HotP99Threshold.Nanoseconds()) {
		snap.IsHot = true
	}
	return snap
}

// percentileAt 对已排序样本在 [0,1] 分位数处做线性插值
func percentileAt(sorted []int64, q float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return float64(sorted[0])
	}
	pos := q * float64(n-1)
	lo := int(pos)
	hi := lo + 1
	if hi >= n {
		return float64(sorted[n-1])
	}
	frac := pos - float64(lo)
	return float64(sorted[lo]) + frac*float64(sorted[hi]-sorted[lo])
}

// NewHotActorProfilingMiddleware 创建一个接收端中间件，自动记录每条消息的处理耗时
// 系统生命周期消息（Started/Stopping/Stopped/Restarting）不计入
func NewHotActorProfilingMiddleware(p *HotActorProfiler) ReceiverMiddleware {
	return func(next Actor) Actor {
		return &hotActorProfilingActor{inner: next, profiler: p}
	}
}

type hotActorProfilingActor struct {
	inner    Actor
	profiler *HotActorProfiler
}

func (a *hotActorProfilingActor) Receive(ctx Context) {
	if !a.profiler.IsEnabled() {
		a.inner.Receive(ctx)
		return
	}
	switch ctx.Message().(type) {
	case *Started, *Stopping, *Stopped, *Restarting:
		a.inner.Receive(ctx)
		return
	}
	start := time.Now()
	a.inner.Receive(ctx)
	a.profiler.Record(ctx.Self().String(), time.Since(start))
}
