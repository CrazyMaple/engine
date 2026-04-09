package middleware

import "time"

// --- OpenTelemetry 追踪适配层 ---
// 定义引擎内部的 OTel 抽象接口。
// 默认 (无 build tag) 使用 NoOp 实现；
// 编译时添加 -tags otel 切换为真实 OTel SDK 适配。

// SpanKind Span 类型
type SpanKind int

const (
	SpanKindInternal SpanKind = iota
	SpanKindServer
	SpanKindClient
	SpanKindProducer
	SpanKindConsumer
)

// SpanStatus Span 状态
type SpanStatus int

const (
	SpanStatusUnset SpanStatus = iota
	SpanStatusOK
	SpanStatusError
)

// Span 追踪 Span 接口
type Span interface {
	// End 结束 Span
	End()
	// SetAttribute 设置属性
	SetAttribute(key string, value interface{})
	// SetStatus 设置状态
	SetStatus(status SpanStatus, description string)
	// AddEvent 添加事件
	AddEvent(name string, attrs map[string]interface{})
	// SpanContext 返回 Span 上下文（TraceID, SpanID）
	SpanContext() SpanContextData
}

// SpanContextData Span 上下文数据
type SpanContextData struct {
	TraceID  string
	SpanID   string
	ParentID string
}

// Tracer 追踪器接口
type Tracer interface {
	// Start 开始一个新的 Span
	Start(operationName string, opts ...SpanOption) (Span, TracePropagation)
	// StartFromPropagation 从传播上下文中恢复并开始子 Span
	StartFromPropagation(operationName string, prop TracePropagation, opts ...SpanOption) (Span, TracePropagation)
	// Shutdown 关闭追踪器
	Shutdown()
}

// SpanOption Span 配置选项
type SpanOption func(*SpanConfig)

// SpanConfig Span 配置
type SpanConfig struct {
	Kind       SpanKind
	Attributes map[string]interface{}
}

// WithSpanKind 设置 Span 类型
func WithSpanKind(kind SpanKind) SpanOption {
	return func(cfg *SpanConfig) {
		cfg.Kind = kind
	}
}

// WithAttributes 设置 Span 属性
func WithAttributes(attrs map[string]interface{}) SpanOption {
	return func(cfg *SpanConfig) {
		cfg.Attributes = attrs
	}
}

// TracePropagation 追踪上下文传播载体（W3C Trace Context 格式）
type TracePropagation struct {
	TraceParent string // W3C traceparent: 00-{traceID}-{spanID}-{flags}
	TraceState  string // W3C tracestate（可选）
}

// IsEmpty 检查传播上下文是否为空
func (tp TracePropagation) IsEmpty() bool {
	return tp.TraceParent == ""
}

// --- 采样策略 ---

// SamplerDecision 采样决策
type SamplerDecision int

const (
	SamplerDrop    SamplerDecision = iota // 丢弃
	SamplerRecord                         // 仅记录不导出
	SamplerExport                         // 记录并导出
)

// Sampler 采样器接口
type Sampler interface {
	ShouldSample(traceID string, operationName string) SamplerDecision
}

// --- 导出器接口 ---

// SpanExporter Span 导出器接口
type SpanExporter interface {
	// ExportSpan 导出已完成的 Span
	ExportSpan(data ExportSpanData)
	// Shutdown 关闭导出器
	Shutdown()
}

// ExportSpanData 导出用 Span 数据
type ExportSpanData struct {
	TraceID       string                 `json:"trace_id"`
	SpanID        string                 `json:"span_id"`
	ParentSpanID  string                 `json:"parent_span_id,omitempty"`
	OperationName string                 `json:"operation_name"`
	Kind          SpanKind               `json:"kind"`
	StartTime     time.Time              `json:"start_time"`
	EndTime       time.Time              `json:"end_time"`
	Status        SpanStatus             `json:"status"`
	StatusDesc    string                 `json:"status_desc,omitempty"`
	Attributes    map[string]interface{} `json:"attributes,omitempty"`
	Events        []SpanEvent            `json:"events,omitempty"`
}

// SpanEvent Span 事件
type SpanEvent struct {
	Name       string                 `json:"name"`
	Timestamp  time.Time              `json:"timestamp"`
	Attributes map[string]interface{} `json:"attributes,omitempty"`
}
