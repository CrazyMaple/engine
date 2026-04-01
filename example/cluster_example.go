//go:build ignore

package main

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"time"

	"engine/actor"
	"engine/cluster"
	"engine/grain"
	"engine/log"
	"engine/pubsub"
	"engine/remote"
	"engine/router"
)

// ===== 示例 1：Router 路由器 =====

func routerExample() {
	fmt.Println("===== Router Example =====")
	system := actor.DefaultSystem()

	var wg sync.WaitGroup
	wg.Add(3) // 广播3条

	// 创建3个 Worker Actor
	workers := make([]*actor.PID, 3)
	for i := 0; i < 3; i++ {
		idx := i
		props := actor.PropsFromFunc(func(ctx actor.Context) {
			switch msg := ctx.Message().(type) {
			case *actor.Started:
				return
			case string:
				fmt.Printf("  Worker %d received: %s\n", idx, msg)
				wg.Done()
			default:
				_ = msg
			}
		})
		workers[i] = system.Root.Spawn(props)
	}

	// 广播路由器
	broadcastPID := router.NewBroadcastGroup(system, workers...)
	system.Root.Send(broadcastPID, "broadcast message")
	wg.Wait()

	// 轮询路由器
	rrPID := router.NewRoundRobinGroup(system, workers...)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		system.Root.Send(rrPID, fmt.Sprintf("round-robin #%d", i))
	}
	wg.Wait()

	fmt.Println()
}

// ===== 示例 2：Cluster + Grain =====

// PlayerGrain 玩家虚拟 Actor
type PlayerGrain struct {
	identity *grain.GrainIdentity
	level    int
}

func (p *PlayerGrain) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		p.level = 1

	case *grain.GrainInit:
		p.identity = msg.Identity
		log.Info("Player %s activated, level: %d", p.identity.Identity, p.level)

	case string:
		fmt.Printf("  Player %s received: %s (level: %d)\n", p.identity.Identity, msg, p.level)
		ctx.Respond(fmt.Sprintf("Player %s says hello!", p.identity.Identity))

	case *actor.ReceiveTimeout:
		log.Info("Player %s idle timeout, deactivating", p.identity.Identity)
		ctx.StopActor(ctx.Self())
	}
}

func clusterGrainExample() {
	fmt.Println("===== Cluster + Grain Example =====")
	system := actor.DefaultSystem()

	// 配置集群
	config := cluster.DefaultClusterConfig("game-server", "127.0.0.1:7001").
		WithKinds("Player")

	// 创建远程通信层（单节点演示不启动）
	r := remote.NewRemote(system, "127.0.0.1:7001")

	// 创建集群
	c := cluster.NewCluster(system, r, config)

	// 注册 Grain Kind
	kinds := grain.NewKindRegistry()
	kinds.Register(grain.NewKind("Player", func() actor.Actor {
		return &PlayerGrain{}
	}).WithTTL(30 * time.Second))

	// 创建 Identity Lookup 和 GrainClient
	lookup := grain.NewDistHashIdentityLookup()
	grainClient := grain.NewGrainClient(c, lookup)

	// 初始化 PubSub
	ps := pubsub.NewPubSub(c, grainClient)
	ps.Start(kinds)

	// 设置 Lookup 并启动集群
	lookup.Setup(c, kinds)
	c.Start()
	time.Sleep(50 * time.Millisecond)

	// 通过 GrainClient 自动激活玩家
	resp, err := grainClient.Request("Player", "player-001", "你好!", 5*time.Second)
	if err != nil {
		log.Error("Request player-001 failed: %v", err)
	} else {
		fmt.Printf("  Response: %v\n", resp)
	}

	// 再次发送到同一个玩家（不会重新激活）
	resp, err = grainClient.Request("Player", "player-001", "第二条消息", 5*time.Second)
	if err != nil {
		log.Error("Second request failed: %v", err)
	} else {
		fmt.Printf("  Response: %v\n", resp)
	}

	// PubSub 示例
	fmt.Println("\n--- PubSub ---")

	// 创建聊天订阅者
	chatListener := actor.PropsFromFunc(func(ctx actor.Context) {
		switch msg := ctx.Message().(type) {
		case *actor.Started:
			return
		case string:
			fmt.Printf("  [Chat] received: %s\n", msg)
		default:
			_ = msg
		}
	})
	listenerPID := system.Root.Spawn(chatListener)

	ps.Subscribe("world_chat", listenerPID)
	time.Sleep(20 * time.Millisecond)

	ps.Publish("world_chat", "欢迎来到问鼎天下!")
	time.Sleep(50 * time.Millisecond)

	// 清理
	ps.Stop()
	c.Stop()
	fmt.Println()
}

// ===== 主函数 =====

func runClusterExample() {
	routerExample()
	clusterGrainExample()

	fmt.Println("Phase 3 demonstration complete.")
	fmt.Println("Press Ctrl+C to exit...")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
}
