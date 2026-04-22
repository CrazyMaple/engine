package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"engine/log"
)

func TestHandleLogQuery(t *testing.T) {
	rb := log.NewRingBufferSink(8)
	now := time.Now()
	_ = rb.Write(log.LogEntry{Time: now, Level: log.LevelInfo, Msg: "a", TraceID: "t1"})
	_ = rb.Write(log.LogEntry{Time: now.Add(time.Second), Level: log.LevelError, Msg: "b", TraceID: "t2"})

	h := &handlers{config: Config{LogRingBuffer: rb}}

	req := httptest.NewRequest(http.MethodGet, "/api/log/query?trace_id=t2", nil)
	w := httptest.NewRecorder()
	h.handleLogQuery(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if int(resp["count"].(float64)) != 1 {
		t.Fatalf("want 1 entry, got %v", resp["count"])
	}

	// 按级别过滤
	req2 := httptest.NewRequest(http.MethodGet, "/api/log/query?level=error", nil)
	w2 := httptest.NewRecorder()
	h.handleLogQuery(w2, req2)
	var resp2 map[string]interface{}
	_ = json.Unmarshal(w2.Body.Bytes(), &resp2)
	if int(resp2["count"].(float64)) != 1 {
		t.Fatalf("level filter: want 1, got %v", resp2["count"])
	}
}

func TestHandleLogStats(t *testing.T) {
	rb := log.NewRingBufferSink(4)
	for i := 0; i < 3; i++ {
		_ = rb.Write(log.LogEntry{Msg: "m"})
	}
	h := &handlers{config: Config{LogRingBuffer: rb}}
	req := httptest.NewRequest(http.MethodGet, "/api/log/stats", nil)
	w := httptest.NewRecorder()
	h.handleLogStats(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if int(resp["buffered"].(float64)) != 3 {
		t.Fatalf("buffered mismatch: %v", resp)
	}
}

func TestHandleLog_NoConfig(t *testing.T) {
	h := &handlers{config: Config{}}
	req := httptest.NewRequest(http.MethodGet, "/api/log/query", nil)
	w := httptest.NewRecorder()
	h.handleLogQuery(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", w.Code)
	}
}

func TestParseTimestamp(t *testing.T) {
	if _, err := parseTimestamp("2026-04-17T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := parseTimestamp("1745000000"); err != nil {
		t.Fatal(err)
	}
	if _, err := parseTimestamp("notatime"); err == nil {
		t.Fatal("expected parse failure")
	}
}
