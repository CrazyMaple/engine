package testkit

import (
	"fmt"
	"time"

	"gamelib/middleware"
)

// --- Trace 相关场景断言 ---
// 依赖 ScenarioContext 存储一个 *middleware.InMemorySpanExporter，Key 固定为 "span_exporter"

const spanExporterKey = "span_exporter"

// UseSpanExporter 将 InMemorySpanExporter 绑定到场景上下文，供后续 AssertSpan/AssertTraceDuration 使用
func UseSpanExporter(exp *middleware.InMemorySpanExporter) func(*ScenarioContext) error {
	return func(ctx *ScenarioContext) error {
		if exp == nil {
			return fmt.Errorf("span exporter is nil")
		}
		ctx.Set(spanExporterKey, exp)
		return nil
	}
}

func lookupExporter(ctx *ScenarioContext) (*middleware.InMemorySpanExporter, error) {
	v, ok := ctx.Get(spanExporterKey)
	if !ok {
		return nil, fmt.Errorf("span exporter not bound; call UseSpanExporter first")
	}
	exp, ok := v.(*middleware.InMemorySpanExporter)
	if !ok {
		return nil, fmt.Errorf("span_exporter has wrong type %T", v)
	}
	return exp, nil
}

// AssertSpan 断言至少存在一个匹配操作名（和可选 TraceID）的 Span
// traceID 为空字符串时仅按 operationName 过滤
func AssertSpan(operationName, traceID string) func(*ScenarioContext) error {
	return func(ctx *ScenarioContext) error {
		exp, err := lookupExporter(ctx)
		if err != nil {
			return err
		}
		for _, s := range exp.GetSpans() {
			if s.OperationName != operationName {
				continue
			}
			if traceID != "" && s.TraceID != traceID {
				continue
			}
			return nil
		}
		return fmt.Errorf("no span matched operation=%q trace_id=%q (total spans=%d)",
			operationName, traceID, len(exp.GetSpans()))
	}
}

// AssertSpanCount 断言指定 TraceID 下的 Span 数量至少为 min
func AssertSpanCount(traceID string, min int) func(*ScenarioContext) error {
	return func(ctx *ScenarioContext) error {
		exp, err := lookupExporter(ctx)
		if err != nil {
			return err
		}
		count := 0
		for _, s := range exp.GetSpans() {
			if s.TraceID == traceID {
				count++
			}
		}
		if count < min {
			return fmt.Errorf("trace %q: expected >=%d spans, got %d", traceID, min, count)
		}
		return nil
	}
}

// AssertTraceDuration 断言指定 TraceID 的总耗时（最晚 EndTime - 最早 StartTime）落在 [min, max] 区间
// max 为 0 表示不限制上界
func AssertTraceDuration(traceID string, min, max time.Duration) func(*ScenarioContext) error {
	return func(ctx *ScenarioContext) error {
		exp, err := lookupExporter(ctx)
		if err != nil {
			return err
		}
		var start, end time.Time
		found := false
		for _, s := range exp.GetSpans() {
			if s.TraceID != traceID {
				continue
			}
			if !found || s.StartTime.Before(start) {
				start = s.StartTime
			}
			if !found || s.EndTime.After(end) {
				end = s.EndTime
			}
			found = true
		}
		if !found {
			return fmt.Errorf("trace %q not found in exporter", traceID)
		}
		dur := end.Sub(start)
		if dur < min {
			return fmt.Errorf("trace %q duration %v < min %v", traceID, dur, min)
		}
		if max > 0 && dur > max {
			return fmt.Errorf("trace %q duration %v > max %v", traceID, dur, max)
		}
		return nil
	}
}
