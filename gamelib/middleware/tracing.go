package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"engine/actor"
	"engine/log"
)

// TraceContext 追踪上下文
type TraceContext struct {
	TraceID   string
	SpanID    string
	ParentID  string
	StartTime time.Time
}

// NewTraceContext 创建新的追踪上下文
func NewTraceContext() *TraceContext {
	return &TraceContext{
		TraceID:   generateID(),
		SpanID:    generateID(),
		StartTime: time.Now(),
	}
}

// NewChildSpan 创建子 Span
func (tc *TraceContext) NewChildSpan() *TraceContext {
	return &TraceContext{
		TraceID:   tc.TraceID,
		SpanID:    generateID(),
		ParentID:  tc.SpanID,
		StartTime: time.Now(),
	}
}

// Traceable 可追踪消息接口
type Traceable interface {
	GetTrace() *TraceContext
}

// TracedMessage 包装任意消息添加追踪信息
type TracedMessage struct {
	Inner interface{}
	Trace *TraceContext
}

func (tm *TracedMessage) GetTrace() *TraceContext {
	return tm.Trace
}

// WithTrace 为消息附加追踪上下文
func WithTrace(msg interface{}, trace *TraceContext) *TracedMessage {
	return &TracedMessage{Inner: msg, Trace: trace}
}

type tracingActor struct {
	inner actor.Actor
}

// NewTracing 创建追踪中间件
// 自动从 Envelope 的 TraceID 读取链路信息并记录日志
func NewTracing() actor.ReceiverMiddleware {
	return func(next actor.Actor) actor.Actor {
		return &tracingActor{inner: next}
	}
}

func (a *tracingActor) Receive(ctx actor.Context) {
	traceID := ctx.TraceID()

	// 也兼容旧的 Traceable 接口
	if traceID == "" {
		if t, ok := ctx.Message().(Traceable); ok {
			if tc := t.GetTrace(); tc != nil {
				traceID = tc.TraceID
			}
		}
	}

	if traceID != "" {
		log.Debug("[trace] traceID=%s actor=%s msg=%T",
			traceID, ctx.Self(), ctx.Message())
	}

	a.inner.Receive(ctx)
}

// ---- TraceRecord 存储 ----

// TraceRecord 追踪记录
type TraceRecord struct {
	TraceID   string    `json:"trace_id"`
	ActorPID  string    `json:"actor_pid"`
	MsgType   string    `json:"msg_type"`
	Timestamp time.Time `json:"timestamp"`
}

// TraceStore 追踪记录存储，支持按 TraceID 查询
type TraceStore struct {
	mu      sync.RWMutex
	records []TraceRecord
	maxSize int
}

// NewTraceStore 创建追踪存储
func NewTraceStore(maxSize int) *TraceStore {
	if maxSize <= 0 {
		maxSize = 10000
	}
	return &TraceStore{
		records: make([]TraceRecord, 0, 256),
		maxSize: maxSize,
	}
}

// Record 记录追踪信息
func (ts *TraceStore) Record(record TraceRecord) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if len(ts.records) >= ts.maxSize {
		// 淘汰最旧的 10%
		drop := ts.maxSize / 10
		if drop == 0 {
			drop = 1
		}
		ts.records = ts.records[drop:]
	}
	ts.records = append(ts.records, record)
}

// QueryByTraceID 按 TraceID 查询所有追踪记录
func (ts *TraceStore) QueryByTraceID(traceID string) []TraceRecord {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	var result []TraceRecord
	for _, r := range ts.records {
		if r.TraceID == traceID {
			result = append(result, r)
		}
	}
	return result
}

// Recent 返回最近的 N 条记录
func (ts *TraceStore) Recent(n int) []TraceRecord {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	if n <= 0 || len(ts.records) == 0 {
		return nil
	}
	start := len(ts.records) - n
	if start < 0 {
		start = 0
	}
	result := make([]TraceRecord, len(ts.records)-start)
	copy(result, ts.records[start:])
	return result
}

// NewTracingWithStore 创建带存储的追踪中间件，记录到 TraceStore
func NewTracingWithStore(store *TraceStore) actor.ReceiverMiddleware {
	return func(next actor.Actor) actor.Actor {
		return &tracingStoreActor{inner: next, store: store}
	}
}

type tracingStoreActor struct {
	inner actor.Actor
	store *TraceStore
}

func (a *tracingStoreActor) Receive(ctx actor.Context) {
	traceID := ctx.TraceID()

	if traceID == "" {
		if t, ok := ctx.Message().(Traceable); ok {
			if tc := t.GetTrace(); tc != nil {
				traceID = tc.TraceID
			}
		}
	}

	if traceID != "" {
		a.store.Record(TraceRecord{
			TraceID:   traceID,
			ActorPID:  ctx.Self().String(),
			MsgType:   msgTypeName(ctx.Message()),
			Timestamp: time.Now(),
		})
	}

	a.inner.Receive(ctx)
}

func msgTypeName(msg interface{}) string {
	if msg == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%T", msg)
}

func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
