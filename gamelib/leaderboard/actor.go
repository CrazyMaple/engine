package leaderboard

import (
	"engine/actor"
)

// Actor Actor 适配层，将 LeaderboardActor 接入 Actor 系统
// 实现 actor.Actor 接口，所有写操作通过消息串行化，保证并发安全
type Actor struct {
	core *LeaderboardActor
}

// NewActor 创建排行榜 Actor 适配器
func NewActor(cfg LeaderboardConfig) *Actor {
	return &Actor{
		core: NewLeaderboardActor(cfg),
	}
}

// NewProps 创建排行榜 Actor 的 Props
func NewProps(cfg LeaderboardConfig) *actor.Props {
	return actor.PropsFromProducer(func() actor.Actor {
		return NewActor(cfg)
	})
}

// Receive 实现 actor.Actor 接口
func (a *Actor) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started, *actor.Stopping, *actor.Stopped,
		*actor.Restarting:
		// 生命周期消息，无需处理
		return

	case *UpdateScoreRequest,
		*GetRankRequest,
		*GetTopNRequest,
		*GetAroundMeRequest,
		*ResetBoardRequest:
		resp := a.core.ProcessMessage(msg)
		ctx.Respond(resp)
	}
}

// Core 返回内部核心实例（供测试或高级场景直接调用）
func (a *Actor) Core() *LeaderboardActor {
	return a.core
}
