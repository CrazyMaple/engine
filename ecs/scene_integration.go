package ecs

import (
	"engine/actor"
	"engine/log"
	"time"
)

// SceneWorld 场景 ECS 世界，融合 Actor 模型与 ECS 帧循环
// Scene Actor 拥有 SceneWorld，通过 Timer 驱动 Tick
type SceneWorld struct {
	World   *World
	Systems *SystemGroup
	Ticker  *Ticker
}

// NewSceneWorld 创建场景 ECS 世界
func NewSceneWorld(fps int) *SceneWorld {
	world := NewWorld()
	systems := NewSystemGroup()
	ticker := NewTicker(world, systems, fps)
	return &SceneWorld{
		World:   world,
		Systems: systems,
		Ticker:  ticker,
	}
}

// ECSSceneActor 示例：融合 ECS 帧循环的 Scene Actor
// 在 Started 时启动帧循环，每帧驱动 ECS 系统更新
type ECSSceneActor struct {
	sceneWorld *SceneWorld
	fps        int
	timerPID   *actor.PID
}

// NewECSSceneActor 创建 ECS 场景 Actor
func NewECSSceneActor(fps int) actor.Producer {
	return func() actor.Actor {
		return &ECSSceneActor{fps: fps}
	}
}

func (a *ECSSceneActor) Receive(ctx actor.Context) {
	switch ctx.Message().(type) {
	case *actor.Started:
		a.sceneWorld = NewSceneWorld(a.fps)
		// 使用 Actor 的 ReceiveTimeout 作为帧驱动
		ctx.SetReceiveTimeout(a.sceneWorld.Ticker.Interval())
		log.Info("[ecs-scene] started, fps=%d, interval=%v", a.fps, a.sceneWorld.Ticker.Interval())

	case *actor.ReceiveTimeout:
		// 每帧驱动
		a.sceneWorld.Ticker.Tick()
		ctx.SetReceiveTimeout(a.sceneWorld.Ticker.Interval())

	case *TickMsg:
		// 也支持外部手动 Tick
		a.sceneWorld.Ticker.TickOnce(a.sceneWorld.Ticker.Interval())

	case *AddSystemMsg:
		msg := ctx.Message().(*AddSystemMsg)
		a.sceneWorld.Systems.Add(msg.System)

	case *AddEntityMsg:
		msg := ctx.Message().(*AddEntityMsg)
		a.sceneWorld.World.Add(msg.Entity)

	case *RemoveEntityMsg:
		msg := ctx.Message().(*RemoveEntityMsg)
		a.sceneWorld.World.Remove(msg.EntityID)

	case *QueryMsg:
		msg := ctx.Message().(*QueryMsg)
		entities := a.sceneWorld.World.QueryMulti(msg.ComponentTypes...)
		ctx.Respond(&QueryResult{Entities: entities})

	case *actor.Stopping:
		ctx.CancelReceiveTimeout()
		log.Info("[ecs-scene] stopping, entities=%d, frames=%d",
			a.sceneWorld.World.Count(), a.sceneWorld.Ticker.FrameCount())
	}
}

// SceneWorld 获取 ECS 世界（允许在测试中直接操作）
func (a *ECSSceneActor) GetSceneWorld() *SceneWorld {
	return a.sceneWorld
}

// --- ECS Scene Actor 消息 ---

// AddSystemMsg 添加系统
type AddSystemMsg struct {
	System System
}

// AddEntityMsg 添加实体
type AddEntityMsg struct {
	Entity *Entity
}

// RemoveEntityMsg 移除实体
type RemoveEntityMsg struct {
	EntityID string
}

// QueryMsg 查询实体
type QueryMsg struct {
	ComponentTypes []string
}

// QueryResult 查询结果
type QueryResult struct {
	Entities []*Entity
}

// FrameStatsMsg 查询帧统计
type FrameStatsMsg struct{}

// FrameStats 帧统计
type FrameStats struct {
	FrameCount uint64
	Interval   time.Duration
	SystemNum  int
	EntityNum  int
}
