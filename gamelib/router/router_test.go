package router

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"engine/actor"
)

// testActor 测试用 Actor
type testActor struct {
	received []interface{}
	mu       sync.Mutex
	wg       *sync.WaitGroup
}

func (a *testActor) Receive(ctx actor.Context) {
	switch ctx.Message().(type) {
	case *actor.Started:
		return
	default:
		a.mu.Lock()
		a.received = append(a.received, ctx.Message())
		a.mu.Unlock()
		if a.wg != nil {
			a.wg.Done()
		}
	}
}

func (a *testActor) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.received)
}

// hashMsg 实现 Hasher 接口的测试消息
type hashMsg struct {
	Key   string
	Value string
}

func (m *hashMsg) Hash() string {
	return m.Key
}

func spawnTestActor(system *actor.ActorSystem, wg *sync.WaitGroup) (*actor.PID, *testActor) {
	a := &testActor{wg: wg}
	props := actor.PropsFromProducer(func() actor.Actor { return a })
	pid := system.Root.Spawn(props)
	return pid, a
}

func TestBroadcastRouter(t *testing.T) {
	system := actor.NewActorSystem()

	var wg sync.WaitGroup
	wg.Add(3) // 3个Actor各收1条消息

	pid1, a1 := spawnTestActor(system, &wg)
	pid2, a2 := spawnTestActor(system, &wg)
	pid3, a3 := spawnTestActor(system, &wg)

	routerPID := NewBroadcastGroup(system, pid1, pid2, pid3)
	system.Root.Send(routerPID, "hello")

	wg.Wait()
	time.Sleep(10 * time.Millisecond)

	if a1.count() != 1 || a2.count() != 1 || a3.count() != 1 {
		t.Errorf("Broadcast: expected each actor to receive 1 message, got %d, %d, %d",
			a1.count(), a2.count(), a3.count())
	}
}

func TestRoundRobinRouter(t *testing.T) {
	system := actor.NewActorSystem()

	var wg sync.WaitGroup
	wg.Add(6) // 6条消息

	pid1, a1 := spawnTestActor(system, &wg)
	pid2, a2 := spawnTestActor(system, &wg)
	pid3, a3 := spawnTestActor(system, &wg)

	routerPID := NewRoundRobinGroup(system, pid1, pid2, pid3)

	for i := 0; i < 6; i++ {
		system.Root.Send(routerPID, i)
	}

	wg.Wait()
	time.Sleep(10 * time.Millisecond)

	// 轮询分配，每个 Actor 应收到 2 条
	if a1.count() != 2 || a2.count() != 2 || a3.count() != 2 {
		t.Errorf("RoundRobin: expected 2 each, got %d, %d, %d",
			a1.count(), a2.count(), a3.count())
	}
}

func TestConsistentHashRouter(t *testing.T) {
	system := actor.NewActorSystem()

	var totalMsgs int32
	actors := make([]*testActor, 3)
	pids := make([]*actor.PID, 3)

	for i := 0; i < 3; i++ {
		a := &testActor{}
		actors[i] = a
		props := actor.PropsFromProducer(func() actor.Actor { return a })
		pids[i] = system.Root.Spawn(props)
	}

	routerPID := NewConsistentHashGroup(system, pids...)

	// 发送带相同 Key 的消息，应路由到同一个 Actor
	for i := 0; i < 10; i++ {
		system.Root.Send(routerPID, &hashMsg{Key: "player-123", Value: "msg"})
	}

	time.Sleep(50 * time.Millisecond)

	for _, a := range actors {
		atomic.AddInt32(&totalMsgs, int32(a.count()))
	}

	if totalMsgs != 10 {
		t.Errorf("ConsistentHash: expected 10 total messages, got %d", totalMsgs)
	}

	// 检查相同 key 的消息都路由到同一个 actor
	var targetCount int
	for _, a := range actors {
		c := a.count()
		if c > 0 {
			targetCount++
			if c != 10 {
				t.Errorf("ConsistentHash: same key should go to same actor, but got %d messages", c)
			}
		}
	}
	if targetCount != 1 {
		t.Errorf("ConsistentHash: expected messages to go to 1 actor, but went to %d", targetCount)
	}
}

func TestAddRemoveRoutee(t *testing.T) {
	system := actor.NewActorSystem()

	var wg sync.WaitGroup
	wg.Add(2)

	pid1, a1 := spawnTestActor(system, &wg)
	pid2, a2 := spawnTestActor(system, &wg)

	routerPID := NewBroadcastGroup(system, pid1)

	// 添加 routee
	system.Root.Send(routerPID, &AddRoutee{PID: pid2})
	time.Sleep(10 * time.Millisecond)

	system.Root.Send(routerPID, "hello")
	wg.Wait()
	time.Sleep(10 * time.Millisecond)

	if a1.count() != 1 || a2.count() != 1 {
		t.Errorf("AddRoutee: expected 1 each, got %d, %d", a1.count(), a2.count())
	}

	// 移除 routee
	system.Root.Send(routerPID, &RemoveRoutee{PID: pid2})
	time.Sleep(10 * time.Millisecond)

	wg.Add(1)
	system.Root.Send(routerPID, "world")
	wg.Wait()
	time.Sleep(10 * time.Millisecond)

	if a1.count() != 2 {
		t.Errorf("RemoveRoutee: expected pid1 to have 2 messages, got %d", a1.count())
	}
	if a2.count() != 1 {
		t.Errorf("RemoveRoutee: expected pid2 to still have 1 message, got %d", a2.count())
	}
}
