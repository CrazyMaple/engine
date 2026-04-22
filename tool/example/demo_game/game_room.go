package main

import (
	"fmt"
	"math/rand"
	"time"

	"engine/actor"
	"gamelib/leaderboard"
	"engine/log"
)

// GameState 游戏房间状态
type GameState int

const (
	StateWaiting  GameState = iota // 等待玩家
	StatePlaying                    // 游戏中
	StateFinished                   // 已结束
)

// PlayerSlot 玩家槽位
type PlayerSlot struct {
	PID      *actor.PID
	PlayerID string
	Guesses  int
}

// GameRoomActor 猜数字游戏房间 Actor
// 管理一局猜数字游戏的完整生命周期
type GameRoomActor struct {
	roomID         string
	system         *actor.ActorSystem
	leaderboardPID *actor.PID

	// 游戏状态
	state       GameState
	targetNum   int           // 目标数字 1-100
	players     []*PlayerSlot // 固定 2 人
	currentTurn int           // 当前轮到的玩家索引
	round       int           // 当前回合数
	maxRounds   int           // 最大回合数

	// 超时控制
	turnTimer *time.Timer
	turnTimeout time.Duration
}

// NewGameRoomActor 创建游戏房间 Actor
func NewGameRoomActor(roomID string, system *actor.ActorSystem, leaderboardPID *actor.PID) *GameRoomActor {
	return &GameRoomActor{
		roomID:         roomID,
		system:         system,
		leaderboardPID: leaderboardPID,
		state:          StateWaiting,
		targetNum:      rand.Intn(100) + 1, // 1-100
		players:        make([]*PlayerSlot, 0, 2),
		maxRounds:      20,
		turnTimeout:    30 * time.Second,
	}
}

// Receive 处理消息
func (g *GameRoomActor) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		log.Info("[room:%s] created, target number: %d", g.roomID, g.targetNum)

	case *PlayerReady:
		g.handlePlayerReady(ctx, msg)

	case *GuessRequest:
		g.handleGuess(ctx, msg)

	case *RoundTimeout:
		g.handleTimeout(ctx, msg)

	case *LeaveRoomRequest:
		g.handleLeave(ctx, msg)

	case *actor.Stopping:
		g.cancelTimer()
		log.Info("[room:%s] stopping", g.roomID)
	}
}

// handlePlayerReady 玩家加入房间
func (g *GameRoomActor) handlePlayerReady(ctx actor.Context, msg *PlayerReady) {
	if g.state != StateWaiting {
		return
	}
	if len(g.players) >= 2 {
		return
	}

	g.players = append(g.players, &PlayerSlot{
		PID:      msg.PlayerPID,
		PlayerID: msg.PlayerID,
	})

	log.Info("[room:%s] player %s joined (%d/2)", g.roomID, msg.PlayerID, len(g.players))

	// 满 2 人开始游戏
	if len(g.players) == 2 {
		g.startGame(ctx)
	}
}

// startGame 开始游戏
func (g *GameRoomActor) startGame(ctx actor.Context) {
	g.state = StatePlaying
	g.round = 1
	// 随机决定先手
	g.currentTurn = rand.Intn(2)

	playerNames := make([]string, len(g.players))
	for i, p := range g.players {
		playerNames[i] = p.PlayerID
	}

	// 通知所有玩家游戏开始
	for i, p := range g.players {
		ctx.Send(p.PID, &GameStartNotify{
			RoomPID:    ctx.Self(),
			YourTurn:   i == g.currentTurn,
			Players:    playerNames,
			TargetHint: "Guess a number between 1 and 100",
		})
	}

	log.Info("[room:%s] game started! First turn: %s, target: %d",
		g.roomID, g.players[g.currentTurn].PlayerID, g.targetNum)

	// 启动回合超时
	g.startTurnTimer(ctx)
}

// handleGuess 处理猜数请求
func (g *GameRoomActor) handleGuess(ctx actor.Context, msg *GuessRequest) {
	if g.state != StatePlaying {
		return
	}

	// 验证是当前回合的玩家
	current := g.players[g.currentTurn]
	if msg.PlayerPID.Id != current.PID.Id {
		return // 不是你的回合
	}

	current.Guesses++
	g.cancelTimer()

	// 判定结果
	var result GuessResult
	switch {
	case msg.Number < g.targetNum:
		result = GuessTooLow
	case msg.Number > g.targetNum:
		result = GuessTooHigh
	default:
		result = GuessCorrect
	}

	// 广播猜测结果给所有玩家
	resultNotify := &GuessResultNotify{
		PlayerID: current.PlayerID,
		Number:   msg.Number,
		Result:   result,
	}
	for _, p := range g.players {
		ctx.Send(p.PID, resultNotify)
	}

	if result == GuessCorrect {
		g.endGame(ctx, current)
		return
	}

	// 检查是否达到最大回合数
	g.round++
	if g.round > g.maxRounds {
		// 平局：谁猜的次数少谁赢，相同则先手赢
		winner := g.players[0]
		if g.players[1].Guesses < g.players[0].Guesses {
			winner = g.players[1]
		}
		g.endGame(ctx, winner)
		return
	}

	// 切换到下一个玩家
	g.currentTurn = (g.currentTurn + 1) % 2
	next := g.players[g.currentTurn]
	ctx.Send(next.PID, &TurnNotify{Round: g.round})

	g.startTurnTimer(ctx)
}

// handleTimeout 回合超时
func (g *GameRoomActor) handleTimeout(ctx actor.Context, msg *RoundTimeout) {
	if g.state != StatePlaying || msg.Round != g.round {
		return
	}

	current := g.players[g.currentTurn]
	log.Info("[room:%s] timeout for player %s at round %d",
		g.roomID, current.PlayerID, g.round)

	// 通知所有玩家超时
	for _, p := range g.players {
		ctx.Send(p.PID, &TimeoutNotify{
			PlayerID: current.PlayerID,
			Round:    g.round,
		})
	}

	// 切换到下一个玩家
	g.round++
	if g.round > g.maxRounds {
		// 超过最大回合数，猜的次数少的赢
		winner := g.players[0]
		if g.players[1].Guesses < g.players[0].Guesses {
			winner = g.players[1]
		}
		g.endGame(ctx, winner)
		return
	}

	g.currentTurn = (g.currentTurn + 1) % 2
	next := g.players[g.currentTurn]
	ctx.Send(next.PID, &TurnNotify{Round: g.round})
	g.startTurnTimer(ctx)
}

// handleLeave 玩家离开
func (g *GameRoomActor) handleLeave(ctx actor.Context, msg *LeaveRoomRequest) {
	if g.state == StateFinished {
		return
	}

	// 对手获胜
	for _, p := range g.players {
		if p.PID.Id != msg.PlayerPID.Id {
			g.endGame(ctx, p)
			return
		}
	}
}

// endGame 结束游戏
func (g *GameRoomActor) endGame(ctx actor.Context, winner *PlayerSlot) {
	g.state = StateFinished
	g.cancelTimer()

	// 计算分数：100 - 猜测次数*10，最低 10 分
	score := 100 - winner.Guesses*10
	if score < 10 {
		score = 10
	}

	log.Info("[room:%s] game over! Winner: %s, Target: %d, Score: %d",
		g.roomID, winner.PlayerID, g.targetNum, score)

	// 通知所有玩家游戏结束
	overNotify := &GameOverNotify{
		WinnerID:   winner.PlayerID,
		TargetNum:  g.targetNum,
		Score:      score,
		TotalGuess: winner.Guesses,
	}
	for _, p := range g.players {
		ctx.Send(p.PID, overNotify)
	}

	// 更新排行榜
	if g.leaderboardPID != nil {
		future := ctx.RequestFuture(g.leaderboardPID, &leaderboard.UpdateScoreRequest{
			Board:    "global",
			PlayerID: winner.PlayerID,
			Score:    float64(score),
			Extra:    winner.PlayerID,
		}, 5*time.Second)

		go func() {
			result := future.Result()
			if resp, ok := result.(*leaderboard.UpdateScoreResponse); ok {
				// 通知获胜者排名变化
				ctx.Send(winner.PID, &ScoreUpdateNotify{
					PlayerID: winner.PlayerID,
					Score:    float64(score),
					Rank:     resp.Rank,
				})
			}
		}()
	}
}

// startTurnTimer 启动回合超时定时器
func (g *GameRoomActor) startTurnTimer(ctx actor.Context) {
	round := g.round
	g.turnTimer = time.AfterFunc(g.turnTimeout, func() {
		ctx.Send(ctx.Self(), &RoundTimeout{Round: round})
	})
}

// cancelTimer 取消超时定时器
func (g *GameRoomActor) cancelTimer() {
	if g.turnTimer != nil {
		g.turnTimer.Stop()
		g.turnTimer = nil
	}
}

// --- 辅助类型 ---

// RoomStats 房间统计（供 Dashboard 查询）
type RoomStats struct {
	RoomID    string   `json:"room_id"`
	State     string   `json:"state"`
	Players   []string `json:"players"`
	Round     int      `json:"round"`
	TargetNum int      `json:"target_num,omitempty"` // 仅结束后暴露
}

// Stats 返回房间统计信息
func (g *GameRoomActor) Stats() *RoomStats {
	stats := &RoomStats{
		RoomID: g.roomID,
		Round:  g.round,
	}
	switch g.state {
	case StateWaiting:
		stats.State = "waiting"
	case StatePlaying:
		stats.State = "playing"
	case StateFinished:
		stats.State = "finished"
		stats.TargetNum = g.targetNum
	}
	for _, p := range g.players {
		stats.Players = append(stats.Players, fmt.Sprintf("%s(%d guesses)", p.PlayerID, p.Guesses))
	}
	return stats
}
