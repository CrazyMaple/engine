package main

import (
	"math/rand"
	"time"

	"engine/actor"
	"engine/log"
)

// PlayerActor 玩家 Actor
// 模拟客户端行为：收到 TurnNotify 后自动猜数
type PlayerActor struct {
	name       string
	system     *actor.ActorSystem
	roomPID    *actor.PID
	lastGuess  int
	guessLow   int // 二分搜索下界
	guessHigh  int // 二分搜索上界
	totalGuess int
}

// NewPlayerActor 创建玩家 Actor
func NewPlayerActor(name string, system *actor.ActorSystem) *PlayerActor {
	return &PlayerActor{
		name:      name,
		system:    system,
		guessLow:  1,
		guessHigh: 100,
	}
}

// Receive 处理消息
func (p *PlayerActor) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		log.Debug("[player:%s] started", p.name)

	case *MatchFoundNotify:
		p.roomPID = msg.RoomPID
		log.Info("[player:%s] matched! opponent: %s", p.name, msg.OpponentID)

	case *GameStartNotify:
		p.roomPID = msg.RoomPID
		p.guessLow = 1
		p.guessHigh = 100
		p.totalGuess = 0
		log.Info("[player:%s] game started, players: %v, my turn: %v",
			p.name, msg.Players, msg.YourTurn)

		if msg.YourTurn {
			p.makeGuess(ctx)
		}

	case *TurnNotify:
		log.Debug("[player:%s] my turn (round %d)", p.name, msg.Round)
		p.makeGuess(ctx)

	case *GuessResultNotify:
		if msg.PlayerID == p.name {
			// 我的猜测结果
			switch msg.Result {
			case GuessTooLow:
				if msg.Number >= p.guessLow {
					p.guessLow = msg.Number + 1
				}
				log.Info("[player:%s] guessed %d → Too Low (range: %d-%d)",
					p.name, msg.Number, p.guessLow, p.guessHigh)
			case GuessTooHigh:
				if msg.Number <= p.guessHigh {
					p.guessHigh = msg.Number - 1
				}
				log.Info("[player:%s] guessed %d → Too High (range: %d-%d)",
					p.name, msg.Number, p.guessLow, p.guessHigh)
			case GuessCorrect:
				log.Info("[player:%s] guessed %d → CORRECT!", p.name, msg.Number)
			}
		} else {
			// 对手的猜测结果（也可以用来缩小范围）
			log.Debug("[player:%s] opponent %s guessed %d → %s",
				p.name, msg.PlayerID, msg.Number, msg.Result)
		}

	case *GameOverNotify:
		if msg.WinnerID == p.name {
			log.Info("[player:%s] I WON! Target: %d, Score: %d",
				p.name, msg.TargetNum, msg.Score)
		} else {
			log.Info("[player:%s] I lost. Winner: %s, Target: %d",
				p.name, msg.WinnerID, msg.TargetNum)
		}
		// 重置状态
		p.roomPID = nil
		p.guessLow = 1
		p.guessHigh = 100
		p.totalGuess = 0

	case *ScoreUpdateNotify:
		log.Info("[player:%s] rank update: score=%.0f, rank=%d",
			p.name, msg.Score, msg.Rank)

	case *TimeoutNotify:
		log.Info("[player:%s] round %d timed out for %s",
			p.name, msg.Round, msg.PlayerID)

	case *actor.Stopping:
		log.Debug("[player:%s] stopping", p.name)
	}
}

// makeGuess 使用二分策略猜数
func (p *PlayerActor) makeGuess(ctx actor.Context) {
	if p.roomPID == nil {
		return
	}

	// 二分猜测 + 少量随机扰动使游戏更有趣
	guess := (p.guessLow + p.guessHigh) / 2
	// 小概率偏移（模拟真实玩家行为）
	if rand.Intn(4) == 0 && p.guessHigh-p.guessLow > 5 {
		offset := rand.Intn(3) - 1
		guess += offset
	}
	if guess < p.guessLow {
		guess = p.guessLow
	}
	if guess > p.guessHigh {
		guess = p.guessHigh
	}

	p.lastGuess = guess
	p.totalGuess++

	// 模拟短暂思考时间
	time.AfterFunc(100*time.Millisecond, func() {
		ctx.Send(p.roomPID, &GuessRequest{
			PlayerPID: ctx.Self(),
			Number:    guess,
		})
	})
}
