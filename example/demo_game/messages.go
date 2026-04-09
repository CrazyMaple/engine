package main

import "engine/actor"

// --- 客户端 → 服务器 消息（C2S）---

// JoinMatchRequest 加入匹配请求
type JoinMatchRequest struct {
	PlayerPID *actor.PID
	PlayerID  string
}

// GuessRequest 猜数字请求
type GuessRequest struct {
	PlayerPID *actor.PID
	Number    int
}

// LeaveRoomRequest 离开房间请求
type LeaveRoomRequest struct {
	PlayerPID *actor.PID
}

// --- 服务器 → 客户端 消息（S2C）---

// MatchFoundNotify 匹配成功通知
type MatchFoundNotify struct {
	RoomPID    *actor.PID
	OpponentID string
}

// GameStartNotify 游戏开始通知
type GameStartNotify struct {
	RoomPID    *actor.PID
	YourTurn   bool
	Players    []string
	TargetHint string // 提示："猜一个 1-100 的数字"
}

// GuessResultNotify 猜测结果通知
type GuessResultNotify struct {
	PlayerID string
	Number   int
	Result   GuessResult
}

// GuessResult 猜测结果
type GuessResult int

const (
	GuessTooLow  GuessResult = iota // 太小
	GuessTooHigh                     // 太大
	GuessCorrect                     // 正确
)

func (r GuessResult) String() string {
	switch r {
	case GuessTooLow:
		return "Too Low"
	case GuessTooHigh:
		return "Too High"
	case GuessCorrect:
		return "Correct!"
	default:
		return "Unknown"
	}
}

// TurnNotify 轮到你猜了
type TurnNotify struct {
	Round int
}

// GameOverNotify 游戏结束通知
type GameOverNotify struct {
	WinnerID   string
	TargetNum  int
	Score      int
	TotalGuess int
}

// TimeoutNotify 超时通知
type TimeoutNotify struct {
	PlayerID string
	Round    int
}

// --- 排行榜消息 ---

// ScoreUpdateNotify 分数更新通知
type ScoreUpdateNotify struct {
	PlayerID string
	Score    float64
	Rank     int
}

// --- 内部消息 ---

// RoundTimeout 回合超时（内部定时器消息）
type RoundTimeout struct {
	Round int
}

// PlayerReady 玩家准备就绪（内部消息）
type PlayerReady struct {
	PlayerPID *actor.PID
	PlayerID  string
}
