//go:build ignore

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"engine/actor"
	"gamelib/middleware"
	"gamelib/persistence"
)

// --- 持久化存储示例 ---
// 演示如何使用 PersistenceMiddleware 实现 Actor 状态自动保存和恢复

// GameState 游戏玩家状态
type GameState struct {
	Name      string `json:"name"`
	Level     int    `json:"level"`
	Exp       int    `json:"exp"`
	Gold      int    `json:"gold"`
	LoginDays int    `json:"login_days"`
}

// GamePlayerActor 带持久化的玩家 Actor
type GamePlayerActor struct {
	id    string
	state GameState
}

// 消息定义
type GainExp struct{ Amount int }
type GainGold struct{ Amount int }
type DailyLogin struct{}
type ShowState struct{}

// PersistenceID 返回持久化标识（persistence.Persistent 接口）
func (p *GamePlayerActor) PersistenceID() string {
	return p.id
}

// GetState 返回需要持久化的状态（persistence.Persistent 接口）
func (p *GamePlayerActor) GetState() interface{} {
	return &p.state
}

// SetState 从持久化存储恢复状态（persistence.Persistent 接口）
func (p *GamePlayerActor) SetState(state interface{}) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &p.state)
}

func (p *GamePlayerActor) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		if p.state.Name == "" {
			// 新玩家初始化
			p.state = GameState{
				Name:  p.id,
				Level: 1,
				Gold:  100,
			}
			fmt.Printf("[%s] 新玩家创建: %+v\n", p.id, p.state)
		} else {
			fmt.Printf("[%s] 状态已恢复: %+v\n", p.id, p.state)
		}
	case *GainExp:
		p.state.Exp += msg.Amount
		// 每100经验升1级
		for p.state.Exp >= 100 {
			p.state.Exp -= 100
			p.state.Level++
			fmt.Printf("[%s] 升级! 当前等级: %d\n", p.id, p.state.Level)
		}
		fmt.Printf("[%s] 获得 %d 经验, 当前: %d/%d\n",
			p.id, msg.Amount, p.state.Exp, 100)
	case *GainGold:
		p.state.Gold += msg.Amount
		fmt.Printf("[%s] 获得 %d 金币, 当前: %d\n",
			p.id, msg.Amount, p.state.Gold)
	case *DailyLogin:
		p.state.LoginDays++
		bonus := p.state.LoginDays * 10
		p.state.Gold += bonus
		fmt.Printf("[%s] 每日登录第 %d 天, 奖励 %d 金币\n",
			p.id, p.state.LoginDays, bonus)
	case *ShowState:
		fmt.Printf("[%s] 当前状态: 等级=%d 经验=%d 金币=%d 登录天数=%d\n",
			p.id, p.state.Level, p.state.Exp, p.state.Gold, p.state.LoginDays)
	}
}

func main() {
	fmt.Println("=== 持久化存储示例 ===")

	system := actor.NewActorSystem()

	// 使用内存存储（生产环境可替换为 MongoStorage）
	storage := persistence.NewMemoryStorage()

	// 创建持久化中间件
	persistMiddleware := middleware.NewPersistenceMiddleware(storage)

	// 创建带持久化的玩家 Actor
	fmt.Println("\n--- 第一次创建玩家 ---")
	props := actor.PropsFromProducer(func() actor.Actor {
		return &GamePlayerActor{id: "player-001"}
	}).WithReceiverMiddleware(persistMiddleware)

	pid := system.Root.SpawnNamed(props, "player-001")
	time.Sleep(50 * time.Millisecond)

	// 发送一些消息修改状态
	fmt.Println("\n--- 游戏操作 ---")
	system.Root.Send(pid, &DailyLogin{})
	system.Root.Send(pid, &GainExp{Amount: 150})
	system.Root.Send(pid, &GainGold{Amount: 500})
	system.Root.Send(pid, &ShowState{})
	time.Sleep(100 * time.Millisecond)

	// 停止 Actor（触发状态保存）
	fmt.Println("\n--- 停止 Actor（保存状态）---")
	system.Root.Stop(pid)
	time.Sleep(100 * time.Millisecond)

	// 验证状态已保存
	var saved GameState
	err := storage.Load(context.Background(), "player-001", &saved)
	if err != nil {
		fmt.Printf("加载失败: %v\n", err)
	} else {
		fmt.Printf("存储中的状态: %+v\n", saved)
	}

	// 重新创建同一玩家（自动恢复状态）
	fmt.Println("\n--- 重新创建玩家（恢复状态）---")
	pid2 := system.Root.SpawnNamed(props, "player-001-restored")
	time.Sleep(50 * time.Millisecond)

	// 继续游戏
	system.Root.Send(pid2, &DailyLogin{})
	system.Root.Send(pid2, &ShowState{})
	time.Sleep(100 * time.Millisecond)

	fmt.Println("\n=== 示例结束 ===")
}
