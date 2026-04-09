package persistence

import (
	"context"
	"testing"
)

// counterEvent 测试用事件
type counterEvent struct {
	Delta int `json:"delta"`
}

// counterState 测试用状态
type counterState struct {
	Value int `json:"value"`
}

// counterActor 实现 EventSourced 的测试 Actor
type counterActor struct {
	id    string
	state counterState
}

func (c *counterActor) PersistenceID() string {
	return c.id
}

func (c *counterActor) ApplyEvent(event interface{}) {
	// 从 JSON 还原时是 map[string]interface{}
	switch e := event.(type) {
	case *counterEvent:
		c.state.Value += e.Delta
	case map[string]interface{}:
		if delta, ok := e["delta"].(float64); ok {
			c.state.Value += int(delta)
		}
	}
}

func (c *counterActor) SnapshotState() interface{} {
	return c.state
}

func (c *counterActor) RestoreSnapshot(snapshot interface{}) error {
	if m, ok := snapshot.(map[string]interface{}); ok {
		if v, ok := m["value"].(float64); ok {
			c.state.Value = int(v)
		}
		return nil
	}
	if s, ok := snapshot.(counterState); ok {
		c.state = s
		return nil
	}
	return nil
}

func TestMemoryJournal_AppendAndLoad(t *testing.T) {
	mj := NewMemoryJournal()
	ctx := context.Background()

	seq1, _ := mj.AppendEvent(ctx, &PersistedEvent{
		PersistenceID: "p1",
		Payload:       &counterEvent{Delta: 5},
	})
	seq2, _ := mj.AppendEvent(ctx, &PersistedEvent{
		PersistenceID: "p1",
		Payload:       &counterEvent{Delta: 10},
	})

	if seq1 >= seq2 {
		t.Errorf("sequences not monotonic: %d, %d", seq1, seq2)
	}

	events, _ := mj.LoadEvents(ctx, "p1", 0)
	if len(events) != 2 {
		t.Errorf("loaded %d events", len(events))
	}

	// Filter by fromSeq
	events, _ = mj.LoadEvents(ctx, "p1", seq2)
	if len(events) != 1 {
		t.Errorf("from seq %d: loaded %d events", seq2, len(events))
	}
}

func TestMemorySnapshotStore_SaveLoad(t *testing.T) {
	store := NewMemorySnapshotStore()
	ctx := context.Background()

	err := store.SaveSnapshot(ctx, &Snapshot{
		PersistenceID:  "p1",
		SequenceNumber: 10,
		Data:           counterState{Value: 42},
	})
	if err != nil {
		t.Fatal(err)
	}

	snap, err := store.LoadSnapshot(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if snap == nil {
		t.Fatal("snapshot not found")
	}
	if snap.SequenceNumber != 10 {
		t.Errorf("seq = %d", snap.SequenceNumber)
	}
}

func TestEventSourcedContext_PersistAndRecover(t *testing.T) {
	mj := NewMemoryJournal()
	mss := NewMemorySnapshotStore()

	// 第一轮：创建 actor 并持久化一些事件
	c1 := &counterActor{id: "counter-1"}
	esc1 := NewEventSourcedContext(c1, mj, mss, 1000)

	_ = esc1.Persist(&counterEvent{Delta: 10})
	_ = esc1.Persist(&counterEvent{Delta: 20})
	_ = esc1.Persist(&counterEvent{Delta: 5})

	if c1.state.Value != 35 {
		t.Errorf("state = %d, want 35", c1.state.Value)
	}

	// 第二轮：新 actor 通过 Recover 恢复状态
	c2 := &counterActor{id: "counter-1"}
	esc2 := NewEventSourcedContext(c2, mj, mss, 1000)
	if err := esc2.Recover(); err != nil {
		t.Fatal(err)
	}

	if c2.state.Value != 35 {
		t.Errorf("recovered state = %d, want 35", c2.state.Value)
	}
}

func TestEventSourcedContext_AutoSnapshot(t *testing.T) {
	mj := NewMemoryJournal()
	mss := NewMemorySnapshotStore()

	c := &counterActor{id: "counter-2"}
	esc := NewEventSourcedContext(c, mj, mss, 3) // 每 3 条事件快照一次

	_ = esc.Persist(&counterEvent{Delta: 1})
	_ = esc.Persist(&counterEvent{Delta: 2})
	// 第 3 次应触发快照
	_ = esc.Persist(&counterEvent{Delta: 3})

	snap, _ := mss.LoadSnapshot(context.Background(), "counter-2")
	if snap == nil {
		t.Fatal("snapshot not saved automatically")
	}
	if snap.SequenceNumber != 3 {
		t.Errorf("snapshot seq = %d, want 3", snap.SequenceNumber)
	}
}

func TestEventSourcedContext_RecoverWithSnapshot(t *testing.T) {
	mj := NewMemoryJournal()
	mss := NewMemorySnapshotStore()

	// 产生一些事件并手动触发快照
	c := &counterActor{id: "c3"}
	esc := NewEventSourcedContext(c, mj, mss, 1000)
	_ = esc.Persist(&counterEvent{Delta: 50})
	_ = esc.Persist(&counterEvent{Delta: 30})
	_ = esc.SaveSnapshot()

	// 快照后再产生新事件
	_ = esc.Persist(&counterEvent{Delta: 20})

	// 恢复：应先加载快照（value=80）再重放新事件（+20=100）
	c2 := &counterActor{id: "c3"}
	esc2 := NewEventSourcedContext(c2, mj, mss, 1000)
	if err := esc2.Recover(); err != nil {
		t.Fatal(err)
	}

	if c2.state.Value != 100 {
		t.Errorf("recovered with snapshot: state = %d, want 100", c2.state.Value)
	}
}
