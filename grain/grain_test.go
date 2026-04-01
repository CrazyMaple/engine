package grain

import (
	"sync"
	"testing"
	"time"

	"engine/actor"
)

// playerActor 测试用玩家 Grain
type playerActor struct {
	identity *GrainIdentity
	mu       sync.Mutex
	messages []interface{}
}

func (a *playerActor) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		// 初始化
	case *GrainInit:
		a.identity = msg.Identity
	case *actor.ReceiveTimeout:
		// 超时，停止自身
		ctx.StopActor(ctx.Self())
	case string:
		a.mu.Lock()
		a.messages = append(a.messages, msg)
		a.mu.Unlock()
		ctx.Respond("ok")
	default:
		_ = msg
	}
}

func TestKindRegistry(t *testing.T) {
	kr := NewKindRegistry()

	kind := NewKind("Player", func() actor.Actor { return &playerActor{} })
	kr.Register(kind)

	got, ok := kr.Get("Player")
	if !ok {
		t.Fatal("Kind not found")
	}
	if got.Name != "Player" {
		t.Errorf("Expected Player, got %s", got.Name)
	}

	_, ok = kr.Get("NonExistent")
	if ok {
		t.Error("Should not find NonExistent kind")
	}

	names := kr.GetNames()
	if len(names) != 1 || names[0] != "Player" {
		t.Errorf("Expected [Player], got %v", names)
	}
}

func TestKindTTL(t *testing.T) {
	kind := NewKind("Player", func() actor.Actor { return &playerActor{} })
	if kind.TTL != DefaultTTL {
		t.Errorf("Expected default TTL, got %v", kind.TTL)
	}

	kind.WithTTL(5 * time.Minute)
	if kind.TTL != 5*time.Minute {
		t.Errorf("Expected 5m TTL, got %v", kind.TTL)
	}
}

func TestGrainIdentity(t *testing.T) {
	gi := &GrainIdentity{Kind: "Player", Identity: "12345"}
	if gi.String() != "Player/12345" {
		t.Errorf("Expected Player/12345, got %s", gi.String())
	}
}

func TestPlacementActorLocalActivation(t *testing.T) {
	system := actor.DefaultSystem()

	kr := NewKindRegistry()
	kr.Register(NewKind("Player", func() actor.Actor {
		return &playerActor{}
	}).WithTTL(1 * time.Second))

	// 创建 PlacementActor
	props := actor.PropsFromProducer(func() actor.Actor {
		return newPlacementActor(kr)
	})
	placementPID := system.Root.SpawnNamed(props, "grain/placement")
	time.Sleep(10 * time.Millisecond)

	// 激活 Grain
	future := system.Root.RequestFuture(placementPID, &ActivateRequest{
		Identity: &GrainIdentity{Kind: "Player", Identity: "p1"},
	}, 5*time.Second)

	result, err := future.Wait()
	if err != nil {
		t.Fatalf("Activation failed: %v", err)
	}

	resp := result.(*ActivateResponse)
	if resp.Error != "" {
		t.Fatalf("Activation error: %s", resp.Error)
	}
	if resp.PID == nil {
		t.Fatal("PID should not be nil")
	}

	// 再次激活同一个应该返回相同 PID
	future2 := system.Root.RequestFuture(placementPID, &ActivateRequest{
		Identity: &GrainIdentity{Kind: "Player", Identity: "p1"},
	}, 5*time.Second)

	result2, err := future2.Wait()
	if err != nil {
		t.Fatalf("Second activation failed: %v", err)
	}

	resp2 := result2.(*ActivateResponse)
	if !resp.PID.Equal(resp2.PID) {
		t.Error("Same identity should return same PID")
	}

	// 激活未知 Kind
	future3 := system.Root.RequestFuture(placementPID, &ActivateRequest{
		Identity: &GrainIdentity{Kind: "Unknown", Identity: "u1"},
	}, 5*time.Second)

	result3, err := future3.Wait()
	if err != nil {
		t.Fatalf("Unknown kind request failed: %v", err)
	}

	resp3 := result3.(*ActivateResponse)
	if resp3.Error == "" {
		t.Error("Unknown kind should return error")
	}
}
