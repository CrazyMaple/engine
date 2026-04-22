package log

import (
	"strings"
	"sync"
	"time"
)

// RingBufferSink 环形缓冲 Sink，保留最近 N 条日志驻留内存
// 用于 Dashboard 实时查询，无需依赖外部日志系统
type RingBufferSink struct {
	mu       sync.RWMutex
	buf      []LogEntry
	cap      int
	head     int   // 下一个写入位置
	count    int   // 当前实际数量
	totalSeq int64 // 累计接收数量（用于估计丢弃量）
}

// NewRingBufferSink 创建容量为 capacity 的环形缓冲 Sink
func NewRingBufferSink(capacity int) *RingBufferSink {
	if capacity <= 0 {
		capacity = 1024
	}
	return &RingBufferSink{
		buf: make([]LogEntry, capacity),
		cap: capacity,
	}
}

func (r *RingBufferSink) Write(entry LogEntry) error {
	r.mu.Lock()
	r.buf[r.head] = entry
	r.head = (r.head + 1) % r.cap
	if r.count < r.cap {
		r.count++
	}
	r.totalSeq++
	r.mu.Unlock()
	return nil
}

func (r *RingBufferSink) Flush() error { return nil }
func (r *RingBufferSink) Close() error { return nil }

// Len 当前缓冲条数
func (r *RingBufferSink) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.count
}

// TotalReceived 累计接收日志数（用于估算溢写丢弃量）
func (r *RingBufferSink) TotalReceived() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.totalSeq
}

// Snapshot 返回当前缓冲全部日志（按时间升序）
func (r *RingBufferSink) Snapshot() []LogEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snapshotLocked()
}

func (r *RingBufferSink) snapshotLocked() []LogEntry {
	if r.count == 0 {
		return nil
	}
	out := make([]LogEntry, 0, r.count)
	start := (r.head - r.count + r.cap) % r.cap
	for i := 0; i < r.count; i++ {
		out = append(out, r.buf[(start+i)%r.cap])
	}
	return out
}

// QueryFilter 日志查询条件，所有非零字段同时满足才匹配
type QueryFilter struct {
	TraceID   string    // 精确匹配（空则不限）
	Actor     string    // 子串包含（空则不限）
	NodeID    string    // 精确匹配（空则不限）
	MinLevel  Level     // 大于等于该级别才匹配
	MsgSubstr string    // msg 字段子串包含（空则不限）
	Since     time.Time // 时间下限（IsZero 则不限）
	Until     time.Time // 时间上限（IsZero 则不限）
	Limit     int       // 最多返回多少条（0 表示无限制）
}

// Query 按过滤条件查询日志
func (r *RingBufferSink) Query(f QueryFilter) []LogEntry {
	r.mu.RLock()
	all := r.snapshotLocked()
	r.mu.RUnlock()

	out := make([]LogEntry, 0, 32)
	for _, e := range all {
		if !matchEntry(e, f) {
			continue
		}
		out = append(out, e)
	}
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[len(out)-f.Limit:] // 优先保留最新
	}
	return out
}

func matchEntry(e LogEntry, f QueryFilter) bool {
	if f.TraceID != "" && e.TraceID != f.TraceID {
		return false
	}
	if f.NodeID != "" && e.NodeID != f.NodeID {
		return false
	}
	if f.Actor != "" && !strings.Contains(e.Actor, f.Actor) {
		return false
	}
	if e.Level < f.MinLevel {
		return false
	}
	if f.MsgSubstr != "" && !strings.Contains(e.Msg, f.MsgSubstr) {
		return false
	}
	if !f.Since.IsZero() && e.Time.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && e.Time.After(f.Until) {
		return false
	}
	return true
}
