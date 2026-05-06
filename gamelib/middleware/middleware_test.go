package middleware

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"engine/actor"
)

// testActor 简单测试 Actor
type testActor struct {
	received []interface{}
}

func (a *testActor) Receive(ctx actor.Context) {
	a.received = append(a.received, ctx.Message())
}

func TestChain(t *testing.T) {
	var mu sync.Mutex
	order := make([]int, 0)

	mw1 := func(next actor.Actor) actor.Actor {
		return actor.ActorFunc(func(ctx actor.Context) {
			mu.Lock()
			order = append(order, 1)
			mu.Unlock()
			next.Receive(ctx)
		})
	}

	mw2 := func(next actor.Actor) actor.Actor {
		return actor.ActorFunc(func(ctx actor.Context) {
			mu.Lock()
			order = append(order, 2)
			mu.Unlock()
			next.Receive(ctx)
		})
	}

	chained := Chain(mw1, mw2)
	inner := &testActor{}
	wrapped := chained(inner)

	system := actor.DefaultSystem()
	props := actor.PropsFromProducer(func() actor.Actor { return wrapped })
	pid := system.Root.Spawn(props)
	defer system.Root.Stop(pid)

	system.Root.Send(pid, "hello")
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	snap := append([]int(nil), order...)
	mu.Unlock()

	// mw1 应该先执行（最外层），然后 mw2
	if len(snap) < 2 || snap[0] != 1 || snap[1] != 2 {
		t.Errorf("expected chain order [1,2], got %v", snap)
	}
}

func TestMetrics(t *testing.T) {
	m := NewMetrics()

	system := actor.DefaultSystem()
	props := actor.PropsFromProducer(func() actor.Actor {
		return &testActor{}
	}).WithReceiverMiddleware(NewMetricsMiddleware(m))

	pid := system.Root.Spawn(props)
	defer system.Root.Stop(pid)

	system.Root.Send(pid, "msg1")
	system.Root.Send(pid, "msg2")
	system.Root.Send(pid, 42)
	time.Sleep(100 * time.Millisecond)

	snap := m.Snapshot()
	if snap.MsgCount["string"] != 2 {
		t.Errorf("expected 2 string messages, got %d", snap.MsgCount["string"])
	}
	if snap.MsgCount["int"] != 1 {
		t.Errorf("expected 1 int message, got %d", snap.MsgCount["int"])
	}

	// 测试 Prometheus 输出
	var buf bytes.Buffer
	m.WritePrometheus(&buf)
	output := buf.String()
	if !strings.Contains(output, "actor_message_count") {
		t.Error("prometheus output missing actor_message_count")
	}
}

func TestMetricsReset(t *testing.T) {
	m := NewMetrics()
	m.record("test", time.Millisecond)
	snap := m.Snapshot()
	if snap.MsgCount["test"] != 1 {
		t.Errorf("expected 1, got %d", snap.MsgCount["test"])
	}

	m.Reset()
	snap = m.Snapshot()
	if len(snap.MsgCount) != 0 {
		t.Errorf("expected empty after reset, got %v", snap.MsgCount)
	}
}

func TestTracedMessage(t *testing.T) {
	tc := NewTraceContext()
	msg := WithTrace("hello", tc)

	if msg.GetTrace().TraceID != tc.TraceID {
		t.Error("trace ID mismatch")
	}
	if msg.Inner != "hello" {
		t.Error("inner message mismatch")
	}

	child := tc.NewChildSpan()
	if child.TraceID != tc.TraceID {
		t.Error("child should inherit trace ID")
	}
	if child.ParentID != tc.SpanID {
		t.Error("child parent should be parent span ID")
	}
}
