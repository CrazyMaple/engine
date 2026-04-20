package remote

import (
	"strings"
	"testing"

	"engine/telemetry"
)

func TestFormatTraceParent_FullTP(t *testing.T) {
	root := telemetry.NewRoot()
	tp := root.TraceParent()
	got := formatTraceParent(tp)
	if got != tp {
		t.Errorf("full traceparent should pass through: got %q want %q", got, tp)
	}
}

func TestFormatTraceParent_BareTraceID(t *testing.T) {
	traceID := strings.Repeat("a", 32)
	got := formatTraceParent(traceID)
	if got == "" {
		t.Fatalf("expected non-empty traceparent")
	}
	parsed := telemetry.ParseTraceParent(got)
	if parsed.TraceID != traceID {
		t.Errorf("TraceID not preserved: got %q want %q", parsed.TraceID, traceID)
	}
	if parsed.SpanID == "" || len(parsed.SpanID) != 16 {
		t.Errorf("expected new 16-char child SpanID, got %q", parsed.SpanID)
	}
}

func TestFormatTraceParent_Empty(t *testing.T) {
	if got := formatTraceParent(""); got != "" {
		t.Errorf("empty trace id should yield empty traceparent, got %q", got)
	}
}

func TestFormatTraceParent_ShortForm(t *testing.T) {
	// "tid/sid" 形式由 ParseShort 兼容处理
	traceID := strings.Repeat("c", 32)
	spanID := strings.Repeat("d", 16)
	got := formatTraceParent(traceID + "/" + spanID)
	parsed := telemetry.ParseTraceParent(got)
	if parsed.TraceID != traceID || parsed.SpanID != spanID {
		t.Errorf("short form not recovered: %+v", parsed)
	}
}

func TestRemoteMessage_TraceParentField(t *testing.T) {
	// 确保字段存在且可序列化（JSON tag = "traceparent"）
	msg := &RemoteMessage{TraceParent: "00-abc-def-01"}
	if msg.TraceParent != "00-abc-def-01" {
		t.Errorf("TraceParent field missing or wrong")
	}
}
