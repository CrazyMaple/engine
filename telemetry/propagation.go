package telemetry

import "sync"

// Carrier 追踪上下文载体接口，便于跨不同传输（Envelope / RemoteMessage / HTTP header）
// 统一注入/提取。实现方提供 Get/Set 两个最小方法。
type Carrier interface {
	Get(key string) string
	Set(key, value string)
}

// Propagator W3C 风格上下文传播器。
// 默认使用 Key "traceparent"；实现 Inject 写入 Carrier，Extract 读取并解析。
type Propagator struct {
	HeaderKey string // 默认 "traceparent"
}

// DefaultPropagator 使用默认 Key 的传播器
func DefaultPropagator() *Propagator {
	return &Propagator{HeaderKey: "traceparent"}
}

// Inject 将 TraceContext 写入 Carrier。若 ctx 无效则不写入。
func (p *Propagator) Inject(tc TraceContext, c Carrier) {
	if !tc.IsValid() || c == nil {
		return
	}
	c.Set(p.key(), tc.TraceParent())
}

// Extract 从 Carrier 读取 TraceContext
func (p *Propagator) Extract(c Carrier) TraceContext {
	if c == nil {
		return TraceContext{}
	}
	return ParseTraceParent(c.Get(p.key()))
}

func (p *Propagator) key() string {
	if p.HeaderKey == "" {
		return "traceparent"
	}
	return p.HeaderKey
}

// MapCarrier 基于 map 的 Carrier 实现，适合存在 Envelope 或 Actor 消息中
// 且需要被 codec 序列化的场景。
type MapCarrier map[string]string

func (m MapCarrier) Get(key string) string {
	if m == nil {
		return ""
	}
	return m[key]
}

func (m MapCarrier) Set(key, value string) {
	if m == nil {
		return
	}
	m[key] = value
}

// --- 活跃上下文注册表 ---
// 进程内维护最近活跃 TraceID → TraceContext 的映射，
// 主要用于异步任务（timer / 回调）在脱离 Actor 消息链时仍能找回上下文。

// ActiveRegistry 活跃上下文注册表
type ActiveRegistry struct {
	mu       sync.RWMutex
	active   map[string]TraceContext
	maxSize  int
	order    []string
}

// NewActiveRegistry 创建上下文注册表
func NewActiveRegistry(maxSize int) *ActiveRegistry {
	if maxSize <= 0 {
		maxSize = 4096
	}
	return &ActiveRegistry{
		active:  make(map[string]TraceContext, 64),
		maxSize: maxSize,
	}
}

// Remember 记录上下文。达到上限时按 FIFO 淘汰。
func (r *ActiveRegistry) Remember(tc TraceContext) {
	if !tc.IsValid() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.active[tc.TraceID]; !ok {
		r.order = append(r.order, tc.TraceID)
		if len(r.order) > r.maxSize {
			drop := r.order[0]
			r.order = r.order[1:]
			delete(r.active, drop)
		}
	}
	r.active[tc.TraceID] = tc
}

// Lookup 查找已记录的上下文
func (r *ActiveRegistry) Lookup(traceID string) (TraceContext, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tc, ok := r.active[traceID]
	return tc, ok
}

// Size 返回当前注册表大小
func (r *ActiveRegistry) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.active)
}
