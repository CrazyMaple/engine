//go:build !otel

package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// --- 默认追踪器实现（无 OTel 依赖） ---
// 编译时未指定 -tags otel 时使用此实现。
// 提供完整的本地追踪能力（TraceID 传播、Span 采集和导出），
// 但不依赖 OpenTelemetry SDK。

// TracerConfig 追踪器配置
type TracerConfig struct {
	// ServiceName 服务名
	ServiceName string
	// Sampler 采样器（nil 表示全量采样）
	Sampler Sampler
	// Exporter 导出器（nil 表示不导出）
	Exporter SpanExporter
}

// NewTracer 创建追踪器
func NewTracer(cfg TracerConfig) Tracer {
	return &defaultTracer{
		serviceName: cfg.ServiceName,
		sampler:     cfg.Sampler,
		exporter:    cfg.Exporter,
	}
}

type defaultTracer struct {
	serviceName string
	sampler     Sampler
	exporter    SpanExporter
}

func (t *defaultTracer) Start(operationName string, opts ...SpanOption) (Span, TracePropagation) {
	cfg := applySpanOpts(opts)
	traceID := genHexID(16)
	spanID := genHexID(8)

	if t.sampler != nil {
		if t.sampler.ShouldSample(traceID, operationName) == SamplerDrop {
			return &noopSpan{ctx: SpanContextData{TraceID: traceID, SpanID: spanID}},
				TracePropagation{TraceParent: formatTraceParent(traceID, spanID)}
		}
	}

	s := &defaultSpan{
		tracer:    t,
		traceID:   traceID,
		spanID:    spanID,
		operation: operationName,
		kind:      cfg.Kind,
		startTime: time.Now(),
		attrs:     cfg.Attributes,
	}

	prop := TracePropagation{
		TraceParent: formatTraceParent(traceID, spanID),
	}
	return s, prop
}

func (t *defaultTracer) StartFromPropagation(operationName string, prop TracePropagation, opts ...SpanOption) (Span, TracePropagation) {
	cfg := applySpanOpts(opts)
	parentTraceID, parentSpanID := parseTraceParent(prop.TraceParent)

	traceID := parentTraceID
	if traceID == "" {
		traceID = genHexID(16)
	}
	spanID := genHexID(8)

	if t.sampler != nil {
		if t.sampler.ShouldSample(traceID, operationName) == SamplerDrop {
			return &noopSpan{ctx: SpanContextData{TraceID: traceID, SpanID: spanID, ParentID: parentSpanID}},
				TracePropagation{TraceParent: formatTraceParent(traceID, spanID)}
		}
	}

	s := &defaultSpan{
		tracer:    t,
		traceID:   traceID,
		spanID:    spanID,
		parentID:  parentSpanID,
		operation: operationName,
		kind:      cfg.Kind,
		startTime: time.Now(),
		attrs:     cfg.Attributes,
	}

	outProp := TracePropagation{
		TraceParent: formatTraceParent(traceID, spanID),
		TraceState:  prop.TraceState,
	}
	return s, outProp
}

func (t *defaultTracer) Shutdown() {
	if t.exporter != nil {
		t.exporter.Shutdown()
	}
}

// --- defaultSpan ---

type defaultSpan struct {
	tracer    *defaultTracer
	traceID   string
	spanID    string
	parentID  string
	operation string
	kind      SpanKind
	startTime time.Time
	endTime   time.Time
	status    SpanStatus
	statusDesc string
	attrs     map[string]interface{}
	events    []SpanEvent
	mu        sync.Mutex
	ended     bool
}

func (s *defaultSpan) End() {
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}
	s.ended = true
	s.endTime = time.Now()
	s.mu.Unlock()

	if s.tracer.exporter != nil {
		s.tracer.exporter.ExportSpan(ExportSpanData{
			TraceID:       s.traceID,
			SpanID:        s.spanID,
			ParentSpanID:  s.parentID,
			OperationName: s.operation,
			Kind:          s.kind,
			StartTime:     s.startTime,
			EndTime:       s.endTime,
			Status:        s.status,
			StatusDesc:    s.statusDesc,
			Attributes:    s.attrs,
			Events:        s.events,
		})
	}
}

func (s *defaultSpan) SetAttribute(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attrs == nil {
		s.attrs = make(map[string]interface{})
	}
	s.attrs[key] = value
}

func (s *defaultSpan) SetStatus(status SpanStatus, description string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
	s.statusDesc = description
}

func (s *defaultSpan) AddEvent(name string, attrs map[string]interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, SpanEvent{
		Name:       name,
		Timestamp:  time.Now(),
		Attributes: attrs,
	})
}

func (s *defaultSpan) SpanContext() SpanContextData {
	return SpanContextData{
		TraceID:  s.traceID,
		SpanID:   s.spanID,
		ParentID: s.parentID,
	}
}

// --- noopSpan ---

type noopSpan struct {
	ctx SpanContextData
}

func (s *noopSpan) End()                                              {}
func (s *noopSpan) SetAttribute(key string, value interface{})        {}
func (s *noopSpan) SetStatus(status SpanStatus, description string)   {}
func (s *noopSpan) AddEvent(name string, attrs map[string]interface{}) {}
func (s *noopSpan) SpanContext() SpanContextData                      { return s.ctx }

// --- 采样器实现 ---

// AlwaysSampler 全量采样
type AlwaysSampler struct{}

func (AlwaysSampler) ShouldSample(traceID, operationName string) SamplerDecision {
	return SamplerExport
}

// NeverSampler 全部丢弃
type NeverSampler struct{}

func (NeverSampler) ShouldSample(traceID, operationName string) SamplerDecision {
	return SamplerDrop
}

// RatioSampler 比例采样
type RatioSampler struct {
	Ratio float64 // 0.0 ~ 1.0
}

func (s RatioSampler) ShouldSample(traceID, operationName string) SamplerDecision {
	if s.Ratio >= 1.0 {
		return SamplerExport
	}
	if s.Ratio <= 0.0 {
		return SamplerDrop
	}
	// 基于 traceID 的确定性采样（同一 trace 始终得到相同决策）
	if len(traceID) >= 2 {
		b := traceID[len(traceID)-2:]
		var v int
		fmt.Sscanf(b, "%x", &v)
		if float64(v)/256.0 < s.Ratio {
			return SamplerExport
		}
		return SamplerDrop
	}
	return SamplerExport
}

// --- 导出器实现 ---

// LogSpanExporter 将 Span 数据输出到日志（开发调试用）
type LogSpanExporter struct {
	LogFn func(format string, args ...interface{})
}

func (e *LogSpanExporter) ExportSpan(data ExportSpanData) {
	if e.LogFn != nil {
		e.LogFn("[span] trace=%s span=%s parent=%s op=%s dur=%s",
			data.TraceID, data.SpanID, data.ParentSpanID,
			data.OperationName, data.EndTime.Sub(data.StartTime))
	}
}

func (e *LogSpanExporter) Shutdown() {}

// InMemorySpanExporter 内存导出器（测试用）
type InMemorySpanExporter struct {
	mu    sync.Mutex
	spans []ExportSpanData
}

func NewInMemorySpanExporter() *InMemorySpanExporter {
	return &InMemorySpanExporter{}
}

func (e *InMemorySpanExporter) ExportSpan(data ExportSpanData) {
	e.mu.Lock()
	e.spans = append(e.spans, data)
	e.mu.Unlock()
}

func (e *InMemorySpanExporter) Shutdown() {}

// GetSpans 返回已导出的 Span 数据
func (e *InMemorySpanExporter) GetSpans() []ExportSpanData {
	e.mu.Lock()
	defer e.mu.Unlock()
	result := make([]ExportSpanData, len(e.spans))
	copy(result, e.spans)
	return result
}

// Reset 清空已导出的 Span
func (e *InMemorySpanExporter) Reset() {
	e.mu.Lock()
	e.spans = e.spans[:0]
	e.mu.Unlock()
}

// --- W3C Trace Context 辅助函数 ---

func formatTraceParent(traceID, spanID string) string {
	return "00-" + traceID + "-" + spanID + "-01"
}

func parseTraceParent(tp string) (traceID, spanID string) {
	// 格式: 00-{traceID}-{spanID}-{flags}
	if len(tp) < 6 {
		return "", ""
	}
	parts := splitTraceParent(tp)
	if len(parts) >= 3 {
		return parts[1], parts[2]
	}
	return "", ""
}

func splitTraceParent(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '-' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func applySpanOpts(opts []SpanOption) SpanConfig {
	cfg := SpanConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

func genHexID(byteLen int) string {
	b := make([]byte, byteLen)
	rand.Read(b)
	return hex.EncodeToString(b)
}
