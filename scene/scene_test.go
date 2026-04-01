package scene

import (
	"testing"
	"time"

	"engine/actor"
)

// 收集消息的测试 Actor
type collectActor struct {
	msgs chan interface{}
}

func newCollectActor() *collectActor {
	return &collectActor{msgs: make(chan interface{}, 100)}
}

func (a *collectActor) Receive(ctx actor.Context) {
	switch ctx.Message().(type) {
	case *actor.Started, *actor.Stopping, *actor.Stopped:
	default:
		a.msgs <- ctx.Message()
	}
}

func TestSceneActorEnterLeave(t *testing.T) {
	system := actor.DefaultSystem()

	scenePID := system.Root.Spawn(actor.PropsFromProducer(NewSceneActor(SceneConfig{
		SceneID:    "test-scene",
		GridConfig: GridConfig{Width: 1000, Height: 1000, CellSize: 100},
	})))
	defer system.Root.Stop(scenePID)

	// 创建两个玩家 Actor
	p1 := newCollectActor()
	p1PID := system.Root.Spawn(actor.PropsFromProducer(func() actor.Actor { return p1 }))
	defer system.Root.Stop(p1PID)

	p2 := newCollectActor()
	p2PID := system.Root.Spawn(actor.PropsFromProducer(func() actor.Actor { return p2 }))
	defer system.Root.Stop(p2PID)

	time.Sleep(50 * time.Millisecond)

	// p1 进入场景
	system.Root.Send(scenePID, &EnterScene{EntityID: "p1", PID: p1PID, X: 50, Y: 50})
	time.Sleep(50 * time.Millisecond)

	// p2 进入场景（同一格子）
	system.Root.Send(scenePID, &EnterScene{EntityID: "p2", PID: p2PID, X: 60, Y: 60})
	time.Sleep(50 * time.Millisecond)

	// p1 应收到 p2 进入的通知
	select {
	case msg := <-p1.msgs:
		entered, ok := msg.(*EntityEntered)
		if !ok {
			t.Fatalf("expected EntityEntered, got %T", msg)
		}
		if entered.EntityID != "p2" {
			t.Errorf("expected p2 entered, got %s", entered.EntityID)
		}
	case <-time.After(time.Second):
		t.Fatal("p1 did not receive enter notification")
	}

	// p2 离开
	system.Root.Send(scenePID, &LeaveScene{EntityID: "p2"})
	time.Sleep(50 * time.Millisecond)

	// p1 应收到 p2 离开的通知
	select {
	case msg := <-p1.msgs:
		left, ok := msg.(*EntityLeft)
		if !ok {
			t.Fatalf("expected EntityLeft, got %T", msg)
		}
		if left.EntityID != "p2" {
			t.Errorf("expected p2 left, got %s", left.EntityID)
		}
	case <-time.After(time.Second):
		t.Fatal("p1 did not receive leave notification")
	}
}

func TestSceneActorMove(t *testing.T) {
	system := actor.DefaultSystem()

	scenePID := system.Root.Spawn(actor.PropsFromProducer(NewSceneActor(SceneConfig{
		SceneID:    "move-scene",
		GridConfig: GridConfig{Width: 1000, Height: 1000, CellSize: 100},
	})))
	defer system.Root.Stop(scenePID)

	p1 := newCollectActor()
	p1PID := system.Root.Spawn(actor.PropsFromProducer(func() actor.Actor { return p1 }))
	defer system.Root.Stop(p1PID)

	time.Sleep(50 * time.Millisecond)

	// p1 进入
	system.Root.Send(scenePID, &EnterScene{EntityID: "p1", PID: p1PID, X: 50, Y: 50})
	time.Sleep(50 * time.Millisecond)

	// p1 移动到新格子
	system.Root.Send(scenePID, &MoveInScene{EntityID: "p1", X: 250, Y: 250})
	time.Sleep(50 * time.Millisecond)

	// 没有其他实体，不应有 AOI 通知
	select {
	case msg := <-p1.msgs:
		t.Fatalf("unexpected message: %T %v", msg, msg)
	default:
		// OK
	}
}

func TestSceneActorBroadcast(t *testing.T) {
	system := actor.DefaultSystem()

	scenePID := system.Root.Spawn(actor.PropsFromProducer(NewSceneActor(SceneConfig{
		SceneID:    "broadcast-scene",
		GridConfig: GridConfig{Width: 1000, Height: 1000, CellSize: 100},
	})))
	defer system.Root.Stop(scenePID)

	p1 := newCollectActor()
	p1PID := system.Root.Spawn(actor.PropsFromProducer(func() actor.Actor { return p1 }))
	defer system.Root.Stop(p1PID)

	p2 := newCollectActor()
	p2PID := system.Root.Spawn(actor.PropsFromProducer(func() actor.Actor { return p2 }))
	defer system.Root.Stop(p2PID)

	time.Sleep(50 * time.Millisecond)

	system.Root.Send(scenePID, &EnterScene{EntityID: "p1", PID: p1PID, X: 50, Y: 50})
	system.Root.Send(scenePID, &EnterScene{EntityID: "p2", PID: p2PID, X: 60, Y: 60})
	time.Sleep(100 * time.Millisecond)

	// 清空已有通知
	drainChannel(p1.msgs)
	drainChannel(p2.msgs)

	// 全场景广播
	system.Root.Send(scenePID, &BroadcastToScene{Message: "hello world"})
	time.Sleep(100 * time.Millisecond)

	// 两个玩家都应收到
	assertReceivedString(t, p1.msgs, "hello world", "p1")
	assertReceivedString(t, p2.msgs, "hello world", "p2")
}

func TestSceneManager(t *testing.T) {
	system := actor.DefaultSystem()
	mgr := NewSceneManager(system)

	mgr.CreateScene(SceneConfig{
		SceneID:    "town",
		GridConfig: GridConfig{Width: 500, Height: 500, CellSize: 50},
	})

	if mgr.SceneCount() != 1 {
		t.Errorf("expected 1 scene, got %d", mgr.SceneCount())
	}

	_, ok := mgr.GetScene("town")
	if !ok {
		t.Error("should find town scene")
	}

	mgr.RemoveScene("town")
	time.Sleep(50 * time.Millisecond)

	if mgr.SceneCount() != 0 {
		t.Errorf("expected 0 scenes, got %d", mgr.SceneCount())
	}
}

func drainChannel(ch chan interface{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func assertReceivedString(t *testing.T, ch chan interface{}, expected string, who string) {
	t.Helper()
	select {
	case msg := <-ch:
		if s, ok := msg.(string); ok {
			if s != expected {
				t.Errorf("%s: expected %q, got %q", who, expected, s)
			}
		} else {
			t.Errorf("%s: expected string, got %T", who, msg)
		}
	case <-time.After(time.Second):
		t.Errorf("%s: did not receive message", who)
	}
}
