package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gamelib/middleware"
)

func newTraceHandlers(exp *middleware.InMemorySpanExporter) *handlers {
	return &handlers{config: Config{SpanExporter: exp}}
}

func addSpan(exp *middleware.InMemorySpanExporter, traceID, spanID, parent, op, node string, start time.Time, dur time.Duration) {
	exp.ExportSpan(middleware.ExportSpanData{
		TraceID:       traceID,
		SpanID:        spanID,
		ParentSpanID:  parent,
		OperationName: op,
		StartTime:     start,
		EndTime:       start.Add(dur),
		Attributes:    map[string]interface{}{"node": node},
	})
}

func TestHandleTraceChain_MissingConfig(t *testing.T) {
	h := &handlers{config: Config{}}
	req := httptest.NewRequest(http.MethodGet, "/api/trace/chain?trace_id=abc", nil)
	rec := httptest.NewRecorder()
	h.handleTraceChain(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", rec.Code)
	}
}

func TestHandleTraceChain_MissingTraceID(t *testing.T) {
	exp := middleware.NewInMemorySpanExporter()
	h := newTraceHandlers(exp)
	req := httptest.NewRequest(http.MethodGet, "/api/trace/chain", nil)
	rec := httptest.NewRecorder()
	h.handleTraceChain(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rec.Code)
	}
}

func TestHandleTraceChain_Basic(t *testing.T) {
	exp := middleware.NewInMemorySpanExporter()
	h := newTraceHandlers(exp)

	now := time.Now()
	tid := strings.Repeat("a", 32)
	addSpan(exp, tid, "span-1", "", "root", "node-1", now, 10*time.Millisecond)
	addSpan(exp, tid, "span-2", "span-1", "child", "node-2", now.Add(2*time.Millisecond), 5*time.Millisecond)
	addSpan(exp, "other-trace", "span-3", "", "other", "node-3", now, time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/api/trace/chain?trace_id="+tid, nil)
	rec := httptest.NewRecorder()
	h.handleTraceChain(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp traceChainResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if resp.TraceID != tid {
		t.Errorf("trace_id mismatch: %s", resp.TraceID)
	}
	if resp.SpanCount != 2 {
		t.Errorf("span_count want 2 got %d", resp.SpanCount)
	}
	if len(resp.Nodes) != 2 {
		t.Errorf("want 2 nodes got %v", resp.Nodes)
	}
	if resp.Spans[0].OperationName != "root" {
		t.Errorf("spans not sorted: %+v", resp.Spans)
	}
	if resp.TotalDurMs <= 0 {
		t.Errorf("duration zero: %v", resp.TotalDurMs)
	}
}

func TestHandleTraceActive(t *testing.T) {
	exp := middleware.NewInMemorySpanExporter()
	h := newTraceHandlers(exp)

	now := time.Now()
	for i := 0; i < 3; i++ {
		tid := strings.Repeat(string(rune('a'+i)), 32)
		addSpan(exp, tid, "s1", "", "op", "node-1", now.Add(time.Duration(i)*time.Second), 5*time.Millisecond)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/trace/active?limit=2", nil)
	rec := httptest.NewRecorder()
	h.handleTraceActive(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var resp struct {
		Count  int                      `json:"count"`
		Traces []map[string]interface{} `json:"traces"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if resp.Count != 2 {
		t.Errorf("limit not applied: count=%d", resp.Count)
	}
	if len(resp.Traces) > 0 {
		first := resp.Traces[0]
		if _, ok := first["trace_id"]; !ok {
			t.Errorf("missing trace_id: %+v", first)
		}
	}
}

func TestParseIntDefault(t *testing.T) {
	if parseIntDefault("", 10) != 10 {
		t.Errorf("empty default")
	}
	if parseIntDefault("abc", 7) != 7 {
		t.Errorf("non-numeric default")
	}
	if parseIntDefault("42", 0) != 42 {
		t.Errorf("parse failed")
	}
}
