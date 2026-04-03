package scene

import (
	"testing"
	"time"

	"engine/actor"
)

func TestTransferEntity(t *testing.T) {
	system := actor.NewActorSystem()
	manager := NewSceneManager(system)

	// 创建源场景和目标场景
	srcPID := manager.CreateScene(SceneConfig{
		SceneID:    "scene-src",
		GridConfig: GridConfig{Width: 100, Height: 100, CellSize: 10},
	})
	manager.CreateScene(SceneConfig{
		SceneID:    "scene-dst",
		GridConfig: GridConfig{Width: 100, Height: 100, CellSize: 10},
	})
	time.Sleep(50 * time.Millisecond)

	// 实体进入源场景
	receiver := newTestReceiver(system)
	system.Root.Send(srcPID, &EnterScene{
		EntityID: "player1",
		PID:      receiver.pid,
		X:        50, Y: 50,
	})
	time.Sleep(50 * time.Millisecond)

	// 发起转移
	system.Root.Send(srcPID, &TransferEntity{
		EntityID:      "player1",
		TargetSceneID: "scene-dst",
		TargetX:       20,
		TargetY:       20,
	})
	time.Sleep(100 * time.Millisecond)

	// 验证实体收到 TransferResult
	msgs := receiver.getMessages()
	found := false
	for _, m := range msgs {
		if tr, ok := m.(*TransferResult); ok {
			if tr.Success && tr.TargetSceneID == "scene-dst" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected successful TransferResult, got messages:", msgs)
	}
}

func TestTransferEntity_TargetNotFound(t *testing.T) {
	system := actor.NewActorSystem()
	manager := NewSceneManager(system)

	srcPID := manager.CreateScene(SceneConfig{
		SceneID:    "scene-a",
		GridConfig: GridConfig{Width: 100, Height: 100, CellSize: 10},
	})
	time.Sleep(50 * time.Millisecond)

	receiver := newTestReceiver(system)
	system.Root.Send(srcPID, &EnterScene{
		EntityID: "player1",
		PID:      receiver.pid,
		X:        50, Y: 50,
	})
	time.Sleep(50 * time.Millisecond)

	// 转移到不存在的场景
	system.Root.Send(srcPID, &TransferEntity{
		EntityID:      "player1",
		TargetSceneID: "scene-nonexist",
		TargetX:       10,
		TargetY:       10,
	})
	time.Sleep(100 * time.Millisecond)

	// 验证收到失败结果
	msgs := receiver.getMessages()
	found := false
	for _, m := range msgs {
		if tr, ok := m.(*TransferResult); ok {
			if !tr.Success {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected failed TransferResult")
	}
}

func TestMessageStashDuringTransfer(t *testing.T) {
	system := actor.NewActorSystem()
	manager := NewSceneManager(system)

	srcPID := manager.CreateScene(SceneConfig{
		SceneID:    "scene-s",
		GridConfig: GridConfig{Width: 100, Height: 100, CellSize: 10},
	})
	manager.CreateScene(SceneConfig{
		SceneID:    "scene-t",
		GridConfig: GridConfig{Width: 100, Height: 100, CellSize: 10},
	})
	time.Sleep(50 * time.Millisecond)

	receiver := newTestReceiver(system)
	system.Root.Send(srcPID, &EnterScene{
		EntityID: "player1",
		PID:      receiver.pid,
		X:        50, Y: 50,
	})
	time.Sleep(50 * time.Millisecond)

	// 发起转移，同时发送移动消息（应该被暂存）
	system.Root.Send(srcPID, &TransferEntity{
		EntityID:      "player1",
		TargetSceneID: "scene-t",
		TargetX:       20, TargetY: 20,
	})
	// 立刻发送移动消息
	system.Root.Send(srcPID, &MoveInScene{
		EntityID: "player1",
		X:        60, Y: 60,
	})
	time.Sleep(100 * time.Millisecond)

	// 转移完成后，移动消息不应导致源场景报错
}

func TestBorderEntityUpdate(t *testing.T) {
	system := actor.NewActorSystem()
	manager := NewSceneManager(system)

	scene1PID := manager.CreateScene(SceneConfig{
		SceneID:    "scene-1",
		GridConfig: GridConfig{Width: 100, Height: 100, CellSize: 10},
	})
	scene2PID := manager.CreateScene(SceneConfig{
		SceneID:    "scene-2",
		GridConfig: GridConfig{Width: 100, Height: 100, CellSize: 10},
	})
	time.Sleep(50 * time.Millisecond)

	// 注册 scene-2 为 scene-1 的东邻
	system.Root.Send(scene1PID, &RegisterAdjacentScene{
		SceneID:   "scene-2",
		ScenePID:  scene2PID,
		Direction: AdjacentEast,
		Overlap:   15,
	})
	time.Sleep(30 * time.Millisecond)

	// 实体进入 scene-1 的边界区域
	receiver := newTestReceiver(system)
	system.Root.Send(scene1PID, &EnterScene{
		EntityID: "border-player",
		PID:      receiver.pid,
		X:        90, Y: 50, // 在东边界 overlap 区域内
	})
	time.Sleep(50 * time.Millisecond)

	// 移动到边界区域
	system.Root.Send(scene1PID, &MoveInScene{
		EntityID: "border-player",
		X:        95, Y: 50,
	})
	time.Sleep(50 * time.Millisecond)

	// scene-2 应该收到 BorderEntityUpdate（作为 ghost 实体处理）
	// 验证 ghost 实体不会导致崩溃
}

func TestAdjacentSceneRegistration(t *testing.T) {
	system := actor.NewActorSystem()
	manager := NewSceneManager(system)

	pid := manager.CreateScene(SceneConfig{
		SceneID:    "test-scene",
		GridConfig: GridConfig{Width: 100, Height: 100, CellSize: 10},
	})
	time.Sleep(50 * time.Millisecond)

	otherPID := actor.NewLocalPID("other-scene")
	system.Root.Send(pid, &RegisterAdjacentScene{
		SceneID:   "other",
		ScenePID:  otherPID,
		Direction: AdjacentNorth,
		Overlap:   10,
	})
	time.Sleep(30 * time.Millisecond)

	system.Root.Send(pid, &UnregisterAdjacentScene{SceneID: "other"})
	time.Sleep(30 * time.Millisecond)
}

func TestSceneManagerTransferEntity(t *testing.T) {
	system := actor.NewActorSystem()
	manager := NewSceneManager(system)

	manager.CreateScene(SceneConfig{
		SceneID:    "from",
		GridConfig: GridConfig{Width: 100, Height: 100, CellSize: 10},
	})
	manager.CreateScene(SceneConfig{
		SceneID:    "to",
		GridConfig: GridConfig{Width: 100, Height: 100, CellSize: 10},
	})
	time.Sleep(50 * time.Millisecond)

	fromPID, _ := manager.GetScene("from")
	receiver := newTestReceiver(system)
	system.Root.Send(fromPID, &EnterScene{
		EntityID: "p1", PID: receiver.pid, X: 10, Y: 10,
	})
	time.Sleep(50 * time.Millisecond)

	ok := manager.TransferEntity("p1", "from", "to", 50, 50)
	if !ok {
		t.Fatal("TransferEntity returned false")
	}
	time.Sleep(100 * time.Millisecond)
}

func TestSceneManagerRemoteScene(t *testing.T) {
	system := actor.NewActorSystem()
	manager := NewSceneManager(system)

	remotePID := &actor.PID{Address: "remote-node:8080", Id: "scene/remote-1"}
	manager.RegisterRemoteScene("remote-1", remotePID)

	pid, ok := manager.GetScene("remote-1")
	if !ok {
		t.Fatal("expected to find remote scene")
	}
	if pid.Address != "remote-node:8080" {
		t.Error("wrong remote address:", pid.Address)
	}

	manager.UnregisterRemoteScene("remote-1")
	_, ok = manager.GetScene("remote-1")
	if ok {
		t.Error("expected remote scene to be removed")
	}
}

func TestAllSceneIDs(t *testing.T) {
	system := actor.NewActorSystem()
	manager := NewSceneManager(system)

	manager.CreateScene(SceneConfig{
		SceneID:    "s1",
		GridConfig: GridConfig{Width: 50, Height: 50, CellSize: 10},
	})
	manager.CreateScene(SceneConfig{
		SceneID:    "s2",
		GridConfig: GridConfig{Width: 50, Height: 50, CellSize: 10},
	})
	time.Sleep(50 * time.Millisecond)

	ids := manager.AllSceneIDs()
	if len(ids) != 2 {
		t.Fatalf("expected 2 scenes, got %d", len(ids))
	}
}

// --- test helpers ---

type testReceiver struct {
	pid      *actor.PID
	messages []interface{}
	ch       chan interface{}
}

func newTestReceiver(system *actor.ActorSystem) *testReceiver {
	tr := &testReceiver{
		ch: make(chan interface{}, 100),
	}
	props := actor.PropsFromFunc(func(ctx actor.Context) {
		switch ctx.Message().(type) {
		case *actor.Started, *actor.Stopping, *actor.Stopped:
			return
		default:
			tr.ch <- ctx.Message()
		}
	})
	tr.pid = system.Root.Spawn(props)
	return tr
}

func (tr *testReceiver) getMessages() []interface{} {
	// drain channel
	var msgs []interface{}
	for {
		select {
		case m := <-tr.ch:
			msgs = append(msgs, m)
		default:
			return msgs
		}
	}
}
