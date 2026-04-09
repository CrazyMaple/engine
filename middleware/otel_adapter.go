//go:build otel

package middleware

// --- OpenTelemetry SDK 适配器 ---
// 此文件仅在 -tags otel 时编译。
// 需要项目 go.mod 中添加 OpenTelemetry 依赖：
//   go.opentelemetry.io/otel v1.x
//   go.opentelemetry.io/otel/sdk v1.x
//   go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.x
//
// 示例用法：
//   tracer := middleware.NewOTelTracer(middleware.OTelTracerConfig{
//       ServiceName: "game-server",
//       Endpoint:    "localhost:4317",    // OTLP gRPC endpoint
//       Sampler:     middleware.RatioSampler{Ratio: 0.1},
//   })

import (
	"time"
)

// OTelTracerConfig OTel 追踪器配置
type OTelTracerConfig struct {
	// ServiceName 服务名称
	ServiceName string
	// Endpoint OTLP 导出端点（gRPC）
	Endpoint string
	// Sampler 采样策略（nil 全量采样）
	Sampler Sampler
	// Insecure 是否使用不安全连接（开发环境）
	Insecure bool
}

// NewOTelTracer 创建 OTel 追踪器
// 当前为桥接实现：使用引擎内置 Tracer + OTLP 导出器
// 后续可替换为直接使用 OTel SDK 的 TracerProvider
func NewOTelTracer(cfg OTelTracerConfig) Tracer {
	// 使用引擎内置追踪器 + OTel 导出器桥接
	// 实际 OTel SDK 集成需要以下步骤：
	// 1. 创建 OTLP Exporter
	// 2. 创建 TracerProvider
	// 3. 将引擎 Span 映射到 OTel Span
	//
	// 目前使用 OTLPSpanExporter 作为桥接层
	return &defaultTracer{
		serviceName: cfg.ServiceName,
		sampler:     cfg.Sampler,
		exporter:    NewOTLPSpanExporter(cfg.Endpoint, cfg.Insecure),
	}
}

// OTLPSpanExporter 将 Span 数据通过 OTLP 协议导出
// 编译时需要 OTel SDK 依赖，此处为接口预留
type OTLPSpanExporter struct {
	endpoint string
	insecure bool
	spans    chan ExportSpanData
	done     chan struct{}
}

// NewOTLPSpanExporter 创建 OTLP 导出器
func NewOTLPSpanExporter(endpoint string, insecure bool) *OTLPSpanExporter {
	e := &OTLPSpanExporter{
		endpoint: endpoint,
		insecure: insecure,
		spans:    make(chan ExportSpanData, 1024),
		done:     make(chan struct{}),
	}
	go e.exportLoop()
	return e
}

func (e *OTLPSpanExporter) ExportSpan(data ExportSpanData) {
	select {
	case e.spans <- data:
	default:
		// 队列满时丢弃（生产中应记录指标）
	}
}

func (e *OTLPSpanExporter) Shutdown() {
	close(e.done)
}

func (e *OTLPSpanExporter) exportLoop() {
	batch := make([]ExportSpanData, 0, 64)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case span := <-e.spans:
			batch = append(batch, span)
			if len(batch) >= 64 {
				e.flush(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				e.flush(batch)
				batch = batch[:0]
			}
		case <-e.done:
			// 刷新剩余 span
			for {
				select {
				case span := <-e.spans:
					batch = append(batch, span)
				default:
					if len(batch) > 0 {
						e.flush(batch)
					}
					return
				}
			}
		}
	}
}

func (e *OTLPSpanExporter) flush(batch []ExportSpanData) {
	// TODO: 当引入 OTel SDK 后，此处将 batch 转换为 OTLP protobuf 格式
	// 并通过 gRPC 发送到 e.endpoint
	//
	// 伪代码：
	// conn, _ := grpc.Dial(e.endpoint)
	// client := otlptracegrpc.NewClient(conn)
	// for _, span := range batch {
	//     otelSpan := convertToOTelSpan(span)
	//     client.UploadTraces(ctx, []*tracepb.ResourceSpans{otelSpan})
	// }
	_ = batch
}
