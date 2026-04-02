//go:build ignore

package main

import (
	"fmt"
	"time"

	"engine/actor"
	"engine/cluster"
	"engine/grain"
	"engine/remote"
)

// --- Grain 虚拟 Actor 示例 ---
// 演示如何使用 Grain 实现按需激活的虚拟 Actor

// PlayerGrainState 玩家 Grain 状态
type PlayerGrainState struct {
	Name  string
	Level int
	Gold  int
}

// PlayerGrain 玩家虚拟 Actor
type PlayerGrain struct {
	identity *grain.GrainIdentity
	state    PlayerGrainState
}

// 消息定义
type AddGold struct{ Amount int }
type LevelUp struct{}
type GetInfo struct{}

func (p *PlayerGrain) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		// Grain 被激活时初始化
		fmt.Printf("[Grain] 玩家 %s 被激活\n", p.identity.Identity)
		p.state = PlayerGrainState{
			Name:  p.identity.Identity,
			Level: 1,
			Gold:  100,
		}
	case *actor.Stopping:
		fmt.Printf("[Grain] 玩家 %s 被去激活\n", p.identity.Identity)
	case *AddGold:
		p.state.Gold += msg.Amount
		fmt.Printf("[Grain] %s 获得 %d 金币，当前: %d\n",
			p.state.Name, msg.Amount, p.state.Gold)
	case *LevelUp:
		p.state.Level++
		fmt.Printf("[Grain] %s 升级到 %d 级\n", p.state.Name, p.state.Level)
	case *GetInfo:
		fmt.Printf("[Grain] %s 信息: 等级=%d, 金币=%d\n",
			p.state.Name, p.state.Level, p.state.Gold)
	}
}

func main() {
	fmt.Println("=== Grain 虚拟 Actor 示例 ===")

	system := actor.NewActorSystem()
	system.Address = "127.0.0.1:9001"

	r := remote.NewRemote(system, "127.0.0.1:9001")
	r.Start()

	// 注册 Grain Kind
	grainMgr := grain.NewGrainManager(system)
	grainMgr.RegisterKind("player", func(id *grain.GrainIdentity) actor.Actor {
		return &PlayerGrain{identity: id}
	}, grain.WithTTL(30*time.Second)) // 30 秒无消息自动去激活

	// 创建集群（单节点模式）
	config := cluster.DefaultClusterConfig("grain-demo", "127.0.0.1:9001").
		WithKinds([]string{"player"}...)
	c := cluster.NewCluster(system, r, config)
	if err := c.Start(); err != nil {
		panic(err)
	}

	// 通过 (Kind, Identity) 获取 Grain — 首次访问自动激活
	fmt.Println("\n--- 激活玩家 ---")
	player1 := grainMgr.Get("player", "player-001")
	player2 := grainMgr.Get("player", "player-002")

	// 发送消息给 Grain
	fmt.Println("\n--- 发送消息 ---")
	system.Root.Send(player1, &AddGold{Amount: 500})
	system.Root.Send(player1, &LevelUp{})
	system.Root.Send(player2, &AddGold{Amount: 200})
	time.Sleep(100 * time.Millisecond)

	// 查询信息
	fmt.Println("\n--- 查询信息 ---")
	system.Root.Send(player1, &GetInfo{})
	system.Root.Send(player2, &GetInfo{})
	time.Sleep(100 * time.Millisecond)

	// 再次获取同一 Grain — 复用已有实例
	fmt.Println("\n--- 复用已有 Grain ---")
	player1Again := grainMgr.Get("player", "player-001")
	system.Root.Send(player1Again, &GetInfo{})
	time.Sleep(100 * time.Millisecond)

	fmt.Printf("\n当前活跃 Grain 数量: %d\n", grainMgr.ActiveCount())

	// 清理
	c.Stop()
	r.Stop()
	fmt.Println("\n=== 示例结束 ===")
}
