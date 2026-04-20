package testkit

import (
	"strings"
	"testing"
	"time"

	"engine/middleware"
)

func TestScenarioTrace_AssertSpanAndDuration(t *testing.T) {
	exp := middleware.NewInMemorySpanExporter()
	tid := strings.Repeat("f", 32)

	now := time.Now()
	exp.ExportSpan(middleware.ExportSpanData{
		TraceID:       tid,
		SpanID:        "s1",
		OperationName: "rpc.send",
		StartTime:     now,
		EndTime:       now.Add(3 * time.Millisecond),
	})
	exp.ExportSpan(middleware.ExportSpanData{
		TraceID:       tid,
		SpanID:        "s2",
		ParentSpanID:  "s1",
		OperationName: "actor.receive",
		StartTime:     now.Add(time.Millisecond),
		EndTime:       now.Add(5 * time.Millisecond),
	})

	NewScenario(t, "trace assertions").
		Setup(UseSpanExporter(exp)).
		Verify("rpc span exists", AssertSpan("rpc.send", tid)).
		Verify("two spans at least", AssertSpanCount(tid, 2)).
		Verify("duration range", AssertTraceDuration(tid, time.Millisecond, 50*time.Millisecond)).
		Run()
}

func TestScenarioTrace_MissingExporter(t *testing.T) {
	ctx := &ScenarioContext{values: map[string]interface{}{}}
	if err := AssertSpan("x", "")(ctx); err == nil {
		t.Errorf("expected error when exporter not bound")
	}
}

func TestScenarioTrace_AssertSpanNoMatch(t *testing.T) {
	exp := middleware.NewInMemorySpanExporter()
	ctx := &ScenarioContext{values: map[string]interface{}{}}
	_ = UseSpanExporter(exp)(ctx)
	if err := AssertSpan("missing", "")(ctx); err == nil {
		t.Errorf("expected error for missing op")
	}
}
