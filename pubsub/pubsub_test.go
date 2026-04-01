package pubsub

import (
	"sync"
	"testing"
	"time"

	"engine/actor"
	"engine/cluster"
	"engine/grain"
)

// subscriberActor 测试用订阅者
type subscriberActor struct {
	received []interface{}
	mu       sync.Mutex
	wg       *sync.WaitGroup
}

func (a *subscriberActor) Receive(ctx actor.Context) {
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

func (a *subscriberActor) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.received)
}

func TestPubSubLocalSubscribePublish(t *testing.T) {
	system := actor.DefaultSystem()

	// 配置集群（单节点模式）
	config := cluster.DefaultClusterConfig("test-pubsub", "127.0.0.1:9001").
		WithKinds(TopicKindName)

	c := cluster.NewCluster(system, nil, config)

	// 创建 Kind 注册表和 IdentityLookup
	kinds := grain.NewKindRegistry()
	lookup := grain.NewDistHashIdentityLookup()

	// 创建 PubSub
	grainClient := grain.NewGrainClient(c, lookup)
	ps := NewPubSub(c, grainClient)

	// 启动 PubSub（注册 Topic Kind）
	ps.Start(kinds)

	// 初始化 IdentityLookup（需要在 Kind 注册后）
	lookup.Setup(c, kinds)

	// 启动集群
	c.Start()

	// 等待集群初始化
	time.Sleep(50 * time.Millisecond)

	// 创建订阅者
	var wg sync.WaitGroup

	sub1 := &subscriberActor{wg: &wg}
	sub1Props := actor.PropsFromProducer(func() actor.Actor { return sub1 })
	sub1PID := system.Root.Spawn(sub1Props)

	sub2 := &subscriberActor{wg: &wg}
	sub2Props := actor.PropsFromProducer(func() actor.Actor { return sub2 })
	sub2PID := system.Root.Spawn(sub2Props)

	// 订阅
	err := ps.Subscribe("world_chat", sub1PID)
	if err != nil {
		t.Fatalf("Subscribe sub1 failed: %v", err)
	}

	err = ps.Subscribe("world_chat", sub2PID)
	if err != nil {
		t.Fatalf("Subscribe sub2 failed: %v", err)
	}

	// 发布消息
	wg.Add(2)
	err = ps.Publish("world_chat", "Hello World!")
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	wg.Wait()
	time.Sleep(20 * time.Millisecond)

	if sub1.count() != 1 {
		t.Errorf("sub1 should receive 1 message, got %d", sub1.count())
	}
	if sub2.count() != 1 {
		t.Errorf("sub2 should receive 1 message, got %d", sub2.count())
	}

	// 取消订阅 sub2
	err = ps.Unsubscribe("world_chat", sub2PID)
	if err != nil {
		t.Fatalf("Unsubscribe failed: %v", err)
	}

	// 再次发布
	wg.Add(1)
	err = ps.Publish("world_chat", "Second message")
	if err != nil {
		t.Fatalf("Second publish failed: %v", err)
	}

	wg.Wait()
	time.Sleep(20 * time.Millisecond)

	if sub1.count() != 2 {
		t.Errorf("sub1 should receive 2 messages, got %d", sub1.count())
	}
	if sub2.count() != 1 {
		t.Errorf("sub2 should still have 1 message, got %d", sub2.count())
	}

	// 清理
	ps.Stop()
	c.Stop()
}
