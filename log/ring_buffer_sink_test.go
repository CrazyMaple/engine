package log

import (
	"testing"
	"time"
)

func TestRingBuffer_OverflowKeepsLatest(t *testing.T) {
	rb := NewRingBufferSink(3)
	for i := 0; i < 5; i++ {
		_ = rb.Write(LogEntry{Time: time.Now(), Level: LevelInfo, Msg: "m", Fields: map[string]interface{}{"i": i}})
	}
	if rb.Len() != 3 {
		t.Fatalf("expected 3, got %d", rb.Len())
	}
	if rb.TotalReceived() != 5 {
		t.Fatalf("expected total 5, got %d", rb.TotalReceived())
	}
	snap := rb.Snapshot()
	first := snap[0].Fields["i"].(int)
	last := snap[len(snap)-1].Fields["i"].(int)
	if first != 2 || last != 4 {
		t.Fatalf("unexpected order: first=%d last=%d", first, last)
	}
}

func TestRingBuffer_QueryFilters(t *testing.T) {
	rb := NewRingBufferSink(16)
	now := time.Now()
	rb.Write(LogEntry{Time: now, Level: LevelInfo, Msg: "login", TraceID: "t1", Actor: "/user/auth"})
	rb.Write(LogEntry{Time: now.Add(time.Second), Level: LevelError, Msg: "login fail", TraceID: "t2", Actor: "/user/auth"})
	rb.Write(LogEntry{Time: now.Add(2 * time.Second), Level: LevelWarn, Msg: "deg", TraceID: "t1", Actor: "/system"})

	// 按 TraceID 过滤
	got := rb.Query(QueryFilter{TraceID: "t1"})
	if len(got) != 2 {
		t.Fatalf("trace filter: want 2, got %d", len(got))
	}

	// 按 Actor 子串
	got = rb.Query(QueryFilter{Actor: "auth"})
	if len(got) != 2 {
		t.Fatalf("actor filter: want 2, got %d", len(got))
	}

	// 最低级别 Error
	got = rb.Query(QueryFilter{MinLevel: LevelError})
	if len(got) != 1 || got[0].Msg != "login fail" {
		t.Fatalf("level filter mismatch: %+v", got)
	}

	// 时间窗口
	got = rb.Query(QueryFilter{Since: now.Add(500 * time.Millisecond), Until: now.Add(1500 * time.Millisecond)})
	if len(got) != 1 || got[0].TraceID != "t2" {
		t.Fatalf("time window mismatch: %+v", got)
	}

	// limit
	got = rb.Query(QueryFilter{Limit: 1})
	if len(got) != 1 {
		t.Fatalf("limit failed: %d", len(got))
	}

	// 子串匹配 msg
	got = rb.Query(QueryFilter{MsgSubstr: "fail"})
	if len(got) != 1 {
		t.Fatalf("msg substr: %+v", got)
	}
}
