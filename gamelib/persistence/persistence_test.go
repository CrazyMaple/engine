package persistence

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"engine/actor"
)

func TestMemoryStorage(t *testing.T) {
	ms := NewMemoryStorage()
	ctx := context.Background()

	// Save
	state := map[string]int{"hp": 100, "level": 5}
	if err := ms.Save(ctx, "player1", state); err != nil {
		t.Fatal(err)
	}

	// Load
	var loaded map[string]int
	if err := ms.Load(ctx, "player1", &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded["hp"] != 100 || loaded["level"] != 5 {
		t.Errorf("unexpected state: %v", loaded)
	}

	// Delete
	if err := ms.Delete(ctx, "player1"); err != nil {
		t.Fatal(err)
	}
	if ms.Has("player1") {
		t.Error("should be deleted")
	}

	// Load not found
	if err := ms.Load(ctx, "nonexistent", &loaded); err == nil {
		t.Error("expected error for missing key")
	}
}

// testPlayerState 测试用玩家状态
type testPlayerState struct {
	HP    int    `json:"hp"`
	Level int    `json:"level"`
	Name  string `json:"name"`
}

type setHPMsg struct{ HP int }

// testPersistentActor 测试用可持久化 Actor
type testPersistentActor struct {
	id    string
	state testPlayerState
}

func (a *testPersistentActor) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
	case *actor.Stopping:
	case *setHPMsg:
		a.state.HP = msg.HP
	case *getStateMsg:
		ctx.Respond(&a.state)
	}
}

func (a *testPersistentActor) PersistenceID() string { return a.id }

func (a *testPersistentActor) GetState() interface{} { return a.state }

func (a *testPersistentActor) SetState(state interface{}) error {
	data, _ := json.Marshal(state)
	return json.Unmarshal(data, &a.state)
}

type getStateMsg struct{}

func TestPersistenceMiddleware(t *testing.T) {
	storage := NewMemoryStorage()

	// 预存数据
	storage.Save(context.Background(), "player-1", testPlayerState{
		HP: 80, Level: 10, Name: "Hero",
	})

	system := actor.DefaultSystem()

	myActor := &testPersistentActor{id: "player-1"}
	props := actor.PropsFromProducer(func() actor.Actor {
		return myActor
	}).WithReceiverMiddleware(NewPersistenceMiddleware(PersistenceConfig{
		Storage:      storage,
		SaveInterval: 100 * time.Millisecond,
		SaveOnStop:   true,
		StateFactory: func() interface{} { return &testPlayerState{} },
	}))

	pid := system.Root.Spawn(props)
	time.Sleep(50 * time.Millisecond)

	// 验证状态已从 storage 恢复
	future := system.Root.RequestFuture(pid, &getStateMsg{}, time.Second)
	result, err := future.Wait()
	if err != nil {
		t.Fatal(err)
	}
	state := result.(*testPlayerState)
	if state.HP != 80 || state.Level != 10 {
		t.Errorf("expected restored state HP=80 Level=10, got %+v", state)
	}

	// 发送消息修改状态并使 dirty=true
	system.Root.Send(pid, &setHPMsg{HP: 50})

	// 等待自动保存触发
	time.Sleep(300 * time.Millisecond)

	// 验证 storage 中的数据已更新
	var saved testPlayerState
	if err := storage.Load(context.Background(), "player-1", &saved); err != nil {
		t.Fatal(err)
	}
	if saved.HP != 50 {
		t.Errorf("expected saved HP=50, got %d", saved.HP)
	}

	// 停止 Actor
	system.Root.Stop(pid)
	time.Sleep(100 * time.Millisecond)
}
