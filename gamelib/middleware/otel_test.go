package middleware

import (
	"testing"
	"time"
)

func TestTracer_StartEnd(t *testing.T) {
	exporter := NewInMemorySpanExporter()
	tracer := NewTracer(TracerConfig{
		ServiceName: "test-service",
		Exporter:    exporter,
	})
	defer tracer.Shutdown()

	span, prop := tracer.Start("test-operation", WithSpanKind(SpanKindServer))
	if prop.IsEmpty() {
		t.Error("expected non-empty propagation")
	}

	span.SetAttribute("key", "value")
	span.SetStatus(SpanStatusOK, "")
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 exported span, got %d", len(spans))
	}

	s := spans[0]
	if s.OperationName != "test-operation" {
		t.Errorf("expected 'test-operation', got %s", s.OperationName)
	}
	if s.Kind != SpanKindServer {
		t.Errorf("expected SpanKindServer, got %d", s.Kind)
	}
	if s.TraceID == "" {
		t.Error("expected non-empty trace ID")
	}
	if s.SpanID == "" {
		t.Error("expected non-empty span ID")
	}
	if s.Attributes["key"] != "value" {
		t.Errorf("expected attribute 'key'='value', got %v", s.Attributes["key"])
	}
}

func TestTracer_Propagation(t *testing.T) {
	exporter := NewInMemorySpanExporter()
	tracer := NewTracer(TracerConfig{
		ServiceName: "test",
		Exporter:    exporter,
	})
	defer tracer.Shutdown()

	// Start parent span
	parentSpan, prop := tracer.Start("parent")
	parentCtx := parentSpan.SpanContext()

	// Start child span from propagation
	childSpan, _ := tracer.StartFromPropagation("child", prop)
	childCtx := childSpan.SpanContext()

	// Same trace ID
	if parentCtx.TraceID != childCtx.TraceID {
		t.Errorf("expected same trace ID, parent=%s child=%s", parentCtx.TraceID, childCtx.TraceID)
	}

	// Different span IDs
	if parentCtx.SpanID == childCtx.SpanID {
		t.Error("expected different span IDs")
	}

	// Child's parent is parent's span
	if childCtx.ParentID != parentCtx.SpanID {
		t.Errorf("expected child parent=%s, got %s", parentCtx.SpanID, childCtx.ParentID)
	}

	childSpan.End()
	parentSpan.End()

	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}
}

func TestRatioSampler(t *testing.T) {
	// RatioSampler at 0.0 should drop all
	s := RatioSampler{Ratio: 0.0}
	if s.ShouldSample("abc123ff", "op") != SamplerDrop {
		t.Error("ratio 0.0 should always drop")
	}

	// RatioSampler at 1.0 should export all
	s = RatioSampler{Ratio: 1.0}
	if s.ShouldSample("abc123ff", "op") != SamplerExport {
		t.Error("ratio 1.0 should always export")
	}
}

func TestAlwaysSampler(t *testing.T) {
	s := AlwaysSampler{}
	if s.ShouldSample("any", "op") != SamplerExport {
		t.Error("always sampler should export")
	}
}

func TestNeverSampler(t *testing.T) {
	s := NeverSampler{}
	if s.ShouldSample("any", "op") != SamplerDrop {
		t.Error("never sampler should drop")
	}
}

func TestTracer_WithSampler_Drop(t *testing.T) {
	exporter := NewInMemorySpanExporter()
	tracer := NewTracer(TracerConfig{
		ServiceName: "test",
		Sampler:     NeverSampler{},
		Exporter:    exporter,
	})
	defer tracer.Shutdown()

	span, _ := tracer.Start("should-be-dropped")
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 0 {
		t.Fatalf("expected 0 spans with NeverSampler, got %d", len(spans))
	}
}

func TestTracePropagation_Format(t *testing.T) {
	tp := formatTraceParent("abcdef1234567890abcdef1234567890", "1234567890abcdef")
	expected := "00-abcdef1234567890abcdef1234567890-1234567890abcdef-01"
	if tp != expected {
		t.Errorf("expected %s, got %s", expected, tp)
	}

	traceID, spanID := parseTraceParent(tp)
	if traceID != "abcdef1234567890abcdef1234567890" {
		t.Errorf("expected trace ID, got %s", traceID)
	}
	if spanID != "1234567890abcdef" {
		t.Errorf("expected span ID, got %s", spanID)
	}
}

func TestTracePropagation_Empty(t *testing.T) {
	tp := TracePropagation{}
	if !tp.IsEmpty() {
		t.Error("empty propagation should return true for IsEmpty")
	}
}

func TestSpan_AddEvent(t *testing.T) {
	exporter := NewInMemorySpanExporter()
	tracer := NewTracer(TracerConfig{Exporter: exporter})
	defer tracer.Shutdown()

	span, _ := tracer.Start("op")
	span.AddEvent("test-event", map[string]interface{}{"count": 42})
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatal("expected 1 span")
	}
	if len(spans[0].Events) != 1 {
		t.Fatal("expected 1 event")
	}
	if spans[0].Events[0].Name != "test-event" {
		t.Errorf("expected 'test-event', got %s", spans[0].Events[0].Name)
	}
}

func TestSpan_DoubleEnd(t *testing.T) {
	exporter := NewInMemorySpanExporter()
	tracer := NewTracer(TracerConfig{Exporter: exporter})
	defer tracer.Shutdown()

	span, _ := tracer.Start("op")
	span.End()
	span.End() // second end should be no-op

	if len(exporter.GetSpans()) != 1 {
		t.Fatal("double End() should only export once")
	}
}

func TestInMemoryExporter_Reset(t *testing.T) {
	e := NewInMemorySpanExporter()
	e.ExportSpan(ExportSpanData{OperationName: "a"})
	e.ExportSpan(ExportSpanData{OperationName: "b"})

	if len(e.GetSpans()) != 2 {
		t.Fatal("expected 2 spans")
	}

	e.Reset()
	if len(e.GetSpans()) != 0 {
		t.Fatal("expected 0 spans after reset")
	}
}

func TestLogSpanExporter(t *testing.T) {
	var logged bool
	e := &LogSpanExporter{
		LogFn: func(format string, args ...interface{}) {
			logged = true
		},
	}
	e.ExportSpan(ExportSpanData{
		OperationName: "test",
		StartTime:     time.Now(),
		EndTime:       time.Now(),
	})
	if !logged {
		t.Error("expected log function to be called")
	}
}
