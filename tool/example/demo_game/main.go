// Package main 实现一个完整的多人在线猜数字对战游戏 Demo
//
// 展示引擎核心能力：
//   - Actor 模型：PlayerActor、GameRoomActor、MatchmakerActor、LeaderboardActor
//   - 房间系统：房间创建、加入、匹配、状态机流转
//   - 消息传递：Actor 间通信（Send/Request/Respond）
//   - 排行榜：得分排名、TopN 查询
//   - 持久化：排行榜快照
//   - 定时器：游戏回合超时控制
//
// 游戏规则：
//   1. 服务器生成 1-100 的随机数
//   2. 2 名玩家匹配后进入房间
//   3. 每轮一名玩家猜数，服务器回复"大了/小了/正确"
//   4. 先猜对者获胜，得分 = 100 - 猜测次数*10
//   5. 每轮限时 30 秒，超时自动跳到下一玩家
//
// 运行方式：
//   go run example/demo_game/main.go
//
// 项目结构：
//   demo_game/
//   ├── main.go           # 入口、初始化
//   ├── messages.go        # 消息定义
//   ├── player_actor.go    # 玩家 Actor
//   ├── game_room.go       # 游戏房间 Actor
//   └── matchmaker.go      # 匹配 Actor
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"engine/actor"
	"gamelib/leaderboard"
	"engine/log"
)

func main() {
	system := actor.NewActorSystem()

	// 1. 创建排行榜 Actor
	lbProps := leaderboard.NewProps(leaderboard.LeaderboardConfig{
		Boards: []leaderboard.BoardConfig{
			{Name: "global", MaxSize: 100},
		},
	})
	leaderboardPID := system.Root.SpawnNamed(lbProps, "leaderboard")
	log.Info("[demo] Leaderboard Actor started: %s", leaderboardPID.String())

	// 2. 创建匹配 Actor
	matchProps := actor.PropsFromProducer(func() actor.Actor {
		return NewMatchmakerActor(system, leaderboardPID)
	})
	matchmakerPID := system.Root.SpawnNamed(matchProps, "matchmaker")
	log.Info("[demo] Matchmaker Actor started: %s", matchmakerPID.String())

	// 3. 模拟玩家加入和游戏流程
	simulateGame(system, matchmakerPID, leaderboardPID)

	// 4. 等待退出信号
	log.Info("[demo] Game server running. Press Ctrl+C to exit.")
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Info("[demo] Shutting down...")
}

// simulateGame 模拟多轮游戏（无需真实网络连接）
func simulateGame(system *actor.ActorSystem, matchmakerPID, leaderboardPID *actor.PID) {
	// 创建 4 名模拟玩家
	players := make([]*actor.PID, 4)
	for i := 0; i < 4; i++ {
		name := fmt.Sprintf("player-%d", i+1)
		props := actor.PropsFromProducer(func() actor.Actor {
			return NewPlayerActor(name, system)
		})
		players[i] = system.Root.SpawnNamed(props, name)
		log.Info("[demo] Player %s joined", name)
	}

	// 让玩家加入匹配队列
	for _, pid := range players {
		system.Root.Send(matchmakerPID, &JoinMatchRequest{
			PlayerPID: pid,
			PlayerID:  pid.Id,
		})
	}

	// 此时 matchmaker 会自动匹配 2 对玩家并创建 2 个房间
	log.Info("[demo] All players queued for matching")
}
