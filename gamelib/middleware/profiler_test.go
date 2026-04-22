package middleware

import (
	"testing"
	"time"
)

func TestProfileStore_SaveAndGet(t *testing.T) {
	store := NewProfileStore(3)

	p1 := &ProfileResult{ID: "cpu-1", Type: ProfileCPU, TypeName: "cpu", Data: []byte("data1")}
	p2 := &ProfileResult{ID: "heap-1", Type: ProfileHeap, TypeName: "heap", Data: []byte("data2")}
	store.Save(p1)
	store.Save(p2)

	if got := store.Get("cpu-1"); got == nil {
		t.Fatal("expected to find cpu-1")
	}
	if got := store.Get("heap-1"); got == nil {
		t.Fatal("expected to find heap-1")
	}
	if got := store.Get("nonexist"); got != nil {
		t.Fatal("expected nil for nonexistent profile")
	}
}

func TestProfileStore_Eviction(t *testing.T) {
	store := NewProfileStore(2)

	store.Save(&ProfileResult{ID: "p1"})
	store.Save(&ProfileResult{ID: "p2"})
	store.Save(&ProfileResult{ID: "p3"})

	if got := store.Get("p1"); got != nil {
		t.Fatal("p1 should have been evicted")
	}
	if got := store.Get("p2"); got == nil {
		t.Fatal("p2 should still exist")
	}
	if got := store.Get("p3"); got == nil {
		t.Fatal("p3 should exist")
	}
}

func TestProfileStore_List(t *testing.T) {
	store := NewProfileStore(10)
	store.Save(&ProfileResult{ID: "a", TypeName: "cpu", Size: 100, Trigger: "manual"})
	store.Save(&ProfileResult{ID: "b", TypeName: "heap", Size: 200, Trigger: "auto:gc"})

	list := store.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(list))
	}
	if list[0].ID != "a" || list[1].ID != "b" {
		t.Error("wrong order")
	}
}

func TestProfiler_CaptureHeapProfile(t *testing.T) {
	store := NewProfileStore(10)
	profiler := NewProfiler(store, AutoProfileConfig{})

	result := profiler.CaptureHeapProfile("trace-123")
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Type != ProfileHeap {
		t.Error("expected heap type")
	}
	if result.TraceID != "trace-123" {
		t.Error("expected trace ID")
	}
	if len(result.Data) == 0 {
		t.Error("expected non-empty data")
	}
	if result.Trigger != "manual" {
		t.Error("expected manual trigger")
	}
}

func TestProfiler_CaptureGoroutineProfile(t *testing.T) {
	store := NewProfileStore(10)
	profiler := NewProfiler(store, AutoProfileConfig{})

	result := profiler.CaptureGoroutineProfile("")
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Type != ProfileGoroutine {
		t.Error("expected goroutine type")
	}
	if len(result.Data) == 0 {
		t.Error("expected non-empty data")
	}
}

func TestActorProfiler_Record(t *testing.T) {
	ap := NewActorProfiler()
	ap.Enable()

	// 记录一些数据
	ap.Record("actor-1", 500*time.Microsecond) // <1ms 桶
	ap.Record("actor-1", 3*time.Millisecond)   // 1-5ms 桶
	ap.Record("actor-1", 7*time.Millisecond)   // 5-10ms 桶
	ap.Record("actor-2", 20*time.Millisecond)  // 10-50ms 桶

	stats := ap.Stats()
	if len(stats) != 2 {
		t.Fatalf("expected 2 actors, got %d", len(stats))
	}

	s1 := stats["actor-1"]
	if s1.MessageCount != 3 {
		t.Errorf("expected 3 messages, got %d", s1.MessageCount)
	}
	if s1.Buckets[0] != 1 { // <1ms
		t.Errorf("expected 1 in <1ms bucket, got %d", s1.Buckets[0])
	}
	if s1.Buckets[1] != 1 { // 1-5ms
		t.Errorf("expected 1 in 1-5ms bucket, got %d", s1.Buckets[1])
	}
	if s1.Buckets[2] != 1 { // 5-10ms
		t.Errorf("expected 1 in 5-10ms bucket, got %d", s1.Buckets[2])
	}
}

func TestActorProfiler_Disabled(t *testing.T) {
	ap := NewActorProfiler()
	// 默认关闭
	ap.Record("actor-1", time.Millisecond)

	stats := ap.Stats()
	if len(stats) != 0 {
		t.Error("should not record when disabled")
	}
}

func TestActorProfiler_Reset(t *testing.T) {
	ap := NewActorProfiler()
	ap.Enable()
	ap.Record("actor-1", time.Millisecond)
	ap.Reset()

	if len(ap.Stats()) != 0 {
		t.Error("stats should be empty after reset")
	}
}

func TestActorProfiler_StatsFor(t *testing.T) {
	ap := NewActorProfiler()
	ap.Enable()
	ap.Record("actor-x", 5*time.Millisecond)

	s := ap.StatsFor("actor-x")
	if s == nil {
		t.Fatal("expected stats for actor-x")
	}
	if s.MessageCount != 1 {
		t.Error("wrong count")
	}

	if ap.StatsFor("nonexist") != nil {
		t.Error("expected nil for nonexistent actor")
	}
}

func TestDurationToBucket(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want int
	}{
		{100 * time.Microsecond, 0},
		{1 * time.Millisecond, 1},
		{4 * time.Millisecond, 1},
		{5 * time.Millisecond, 2},
		{9 * time.Millisecond, 2},
		{10 * time.Millisecond, 3},
		{49 * time.Millisecond, 3},
		{50 * time.Millisecond, 4},
		{99 * time.Millisecond, 4},
		{100 * time.Millisecond, 5},
		{1 * time.Second, 5},
	}
	for _, tt := range tests {
		got := durationToBucket(tt.d)
		if got != tt.want {
			t.Errorf("durationToBucket(%v) = %d, want %d", tt.d, got, tt.want)
		}
	}
}

func TestProfileType_String(t *testing.T) {
	if ProfileCPU.String() != "cpu" {
		t.Error("wrong CPU string")
	}
	if ProfileHeap.String() != "heap" {
		t.Error("wrong heap string")
	}
	if ProfileGoroutine.String() != "goroutine" {
		t.Error("wrong goroutine string")
	}
	if ProfileBlock.String() != "block" {
		t.Error("wrong block string")
	}
}
