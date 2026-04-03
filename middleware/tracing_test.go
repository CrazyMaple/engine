package middleware

import (
	"testing"
	"time"
)

func TestTraceContext_NewAndChildSpan(t *testing.T) {
	tc := NewTraceContext()
	if tc.TraceID == "" {
		t.Error("TraceID should not be empty")
	}
	if tc.SpanID == "" {
		t.Error("SpanID should not be empty")
	}
	if tc.ParentID != "" {
		t.Error("root span should have empty ParentID")
	}

	child := tc.NewChildSpan()
	if child.TraceID != tc.TraceID {
		t.Error("child should inherit TraceID")
	}
	if child.ParentID != tc.SpanID {
		t.Error("child ParentID should be parent SpanID")
	}
	if child.SpanID == tc.SpanID {
		t.Error("child should have its own SpanID")
	}
}

func TestTracedMessageWrap(t *testing.T) {
	tc := NewTraceContext()
	msg := WithTrace("hello", tc)

	if msg.Inner != "hello" {
		t.Error("Inner should be original message")
	}

	trace := msg.GetTrace()
	if trace != tc {
		t.Error("GetTrace should return the same TraceContext")
	}
}

func TestTraceStore_RecordAndQuery(t *testing.T) {
	store := NewTraceStore(100)

	store.Record(TraceRecord{
		TraceID:   "trace-1",
		ActorPID:  "actor-a",
		MsgType:   "string",
		Timestamp: time.Now(),
	})
	store.Record(TraceRecord{
		TraceID:   "trace-1",
		ActorPID:  "actor-b",
		MsgType:   "int",
		Timestamp: time.Now(),
	})
	store.Record(TraceRecord{
		TraceID:   "trace-2",
		ActorPID:  "actor-c",
		MsgType:   "bool",
		Timestamp: time.Now(),
	})

	// 按 TraceID 查询
	results := store.QueryByTraceID("trace-1")
	if len(results) != 2 {
		t.Fatalf("expected 2 records for trace-1, got %d", len(results))
	}

	results = store.QueryByTraceID("trace-2")
	if len(results) != 1 {
		t.Fatalf("expected 1 record for trace-2, got %d", len(results))
	}

	results = store.QueryByTraceID("non-existent")
	if len(results) != 0 {
		t.Fatalf("expected 0 records, got %d", len(results))
	}
}

func TestTraceStore_Recent(t *testing.T) {
	store := NewTraceStore(100)

	for i := 0; i < 10; i++ {
		store.Record(TraceRecord{
			TraceID:  "trace-" + string(rune('0'+i)),
			ActorPID: "actor",
			MsgType:  "msg",
		})
	}

	recent := store.Recent(3)
	if len(recent) != 3 {
		t.Fatalf("expected 3 records, got %d", len(recent))
	}

	// 应返回最新的 3 条
	if recent[0].TraceID != "trace-7" {
		t.Errorf("expected trace-7, got %s", recent[0].TraceID)
	}
}

func TestTraceStore_Eviction(t *testing.T) {
	store := NewTraceStore(10)

	for i := 0; i < 15; i++ {
		store.Record(TraceRecord{
			TraceID:  "trace",
			ActorPID: "actor",
		})
	}

	// 应该触发了淘汰
	store.mu.RLock()
	count := len(store.records)
	store.mu.RUnlock()

	if count > 10 {
		t.Errorf("expected max 10 records after eviction, got %d", count)
	}
}

func TestTraceStore_RecentEmpty(t *testing.T) {
	store := NewTraceStore(10)

	if records := store.Recent(5); records != nil {
		t.Errorf("expected nil for empty store, got %v", records)
	}
	if records := store.Recent(0); records != nil {
		t.Errorf("expected nil for n=0, got %v", records)
	}
}

func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()

	if len(id1) != 16 { // 8 bytes = 16 hex chars
		t.Errorf("expected 16 char hex, got %d chars", len(id1))
	}
	if id1 == id2 {
		t.Error("two generated IDs should differ")
	}
}
