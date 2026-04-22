// Package telemetry 提供引擎级的链路追踪基础设施。
//
// 与 middleware/tracing.go 的关系：
//   - middleware/ 是 Actor 层的 Tracer/Span 抽象（OTel 适配）；
//   - telemetry/ 是跨 Actor、跨节点的"元"上下文传播约定，只关心
//     TraceID/SpanID 在 Envelope 与 RemoteMessage 间的注入/提取，
//     不绑定任何导出器实现。
//
// 选择独立包的原因：remote/ 不应依赖 middleware/（middleware 已依赖 actor），
// 把 Trace 传播契约抽成独立包，让 actor/、remote/、log/、dashboard/ 都能共享。
package telemetry

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// TraceContext 跨 Actor / 跨节点传播的追踪上下文。
// 序列化为 W3C traceparent 字符串：00-{trace_id}-{span_id}-{flags}
type TraceContext struct {
	TraceID string
	SpanID  string
	Flags   byte // 低位 1 = sampled
}

// IsValid 判断上下文是否可用于传播
func (tc TraceContext) IsValid() bool {
	return len(tc.TraceID) == 32 && len(tc.SpanID) == 16
}

// Sampled 是否采样
func (tc TraceContext) Sampled() bool {
	return tc.Flags&0x01 == 0x01
}

// TraceParent 序列化为 W3C traceparent 字符串
func (tc TraceContext) TraceParent() string {
	if !tc.IsValid() {
		return ""
	}
	flags := "00"
	if tc.Sampled() {
		flags = "01"
	}
	return "00-" + tc.TraceID + "-" + tc.SpanID + "-" + flags
}

// NewRoot 生成一条新的根追踪上下文（采样开启）
func NewRoot() TraceContext {
	return TraceContext{
		TraceID: genHex(16),
		SpanID:  genHex(8),
		Flags:   0x01,
	}
}

// NewChild 从父上下文派生新的子 Span（复用 TraceID，新 SpanID）
func (tc TraceContext) NewChild() TraceContext {
	if tc.TraceID == "" {
		return NewRoot()
	}
	return TraceContext{
		TraceID: tc.TraceID,
		SpanID:  genHex(8),
		Flags:   tc.Flags,
	}
}

// ParseTraceParent 解析 W3C traceparent 格式，失败返回零值
func ParseTraceParent(tp string) TraceContext {
	if tp == "" {
		return TraceContext{}
	}
	parts := strings.Split(tp, "-")
	if len(parts) != 4 {
		return TraceContext{}
	}
	if len(parts[1]) != 32 || len(parts[2]) != 16 {
		return TraceContext{}
	}
	tc := TraceContext{TraceID: parts[1], SpanID: parts[2]}
	if len(parts[3]) >= 2 {
		// 仅解析第一字节
		b, err := hex.DecodeString(parts[3][:2])
		if err == nil && len(b) == 1 {
			tc.Flags = b[0]
		}
	}
	return tc
}

// ParseShort 在只有 TraceID 或同时包含 SpanID 的场景下做宽松解析
// 支持：
//   - "{traceid}"       →  TraceID=xxx, SpanID="" (主要用于日志关联)
//   - "{traceid}/{spanid}" → 同时含父 SpanID
//   - W3C traceparent：自动识别并委派给 ParseTraceParent
func ParseShort(s string) TraceContext {
	if strings.Count(s, "-") >= 3 {
		return ParseTraceParent(s)
	}
	if i := strings.Index(s, "/"); i > 0 {
		return TraceContext{TraceID: s[:i], SpanID: s[i+1:]}
	}
	return TraceContext{TraceID: s}
}

func genHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// 极端场景的兜底：使用 n 字节零值，调用方可通过 IsValid 识别
		return strings.Repeat("00", n)
	}
	return hex.EncodeToString(buf)
}
