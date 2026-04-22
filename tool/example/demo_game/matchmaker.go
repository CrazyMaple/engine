package main

import (
	"fmt"
	"sync/atomic"

	"engine/actor"
	"engine/log"
)

// MatchmakerActor 匹配 Actor
// 维护匹配队列，每凑齐 2 名玩家创建一个游戏房间
type MatchmakerActor struct {
	system         *actor.ActorSystem
	leaderboardPID *actor.PID

	queue      []*waitingPlayer // 匹配队列
	roomCount  int64            // 房间计数器
}

// waitingPlayer 等待匹配的玩家
type waitingPlayer struct {
	PID      *actor.PID
	PlayerID string
}

// NewMatchmakerActor 创建匹配 Actor
func NewMatchmakerActor(system *actor.ActorSystem, leaderboardPID *actor.PID) *MatchmakerActor {
	return &MatchmakerActor{
		system:         system,
		leaderboardPID: leaderboardPID,
		queue:          make([]*waitingPlayer, 0, 16),
	}
}

// Receive 处理消息
func (m *MatchmakerActor) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		log.Info("[matchmaker] started")

	case *JoinMatchRequest:
		m.handleJoinMatch(ctx, msg)

	case *actor.Stopping:
		log.Info("[matchmaker] stopping, queue size: %d", len(m.queue))
	}
}

// handleJoinMatch 处理加入匹配请求
func (m *MatchmakerActor) handleJoinMatch(ctx actor.Context, msg *JoinMatchRequest) {
	// 检查是否已在队列中
	for _, wp := range m.queue {
		if wp.PlayerID == msg.PlayerID {
			log.Debug("[matchmaker] player %s already in queue", msg.PlayerID)
			return
		}
	}

	m.queue = append(m.queue, &waitingPlayer{
		PID:      msg.PlayerPID,
		PlayerID: msg.PlayerID,
	})

	log.Info("[matchmaker] player %s joined queue (size: %d)", msg.PlayerID, len(m.queue))

	// 尝试匹配
	m.tryMatch(ctx)
}

// tryMatch 尝试匹配
func (m *MatchmakerActor) tryMatch(ctx actor.Context) {
	for len(m.queue) >= 2 {
		p1 := m.queue[0]
		p2 := m.queue[1]
		m.queue = m.queue[2:]

		m.createRoom(ctx, p1, p2)
	}
}

// createRoom 创建游戏房间
func (m *MatchmakerActor) createRoom(ctx actor.Context, p1, p2 *waitingPlayer) {
	roomNum := atomic.AddInt64(&m.roomCount, 1)
	roomID := fmt.Sprintf("room-%d", roomNum)

	// 创建房间 Actor
	roomProps := actor.PropsFromProducer(func() actor.Actor {
		return NewGameRoomActor(roomID, m.system, m.leaderboardPID)
	})
	roomPID := ctx.Spawn(roomProps)

	log.Info("[matchmaker] created %s for %s vs %s", roomID, p1.PlayerID, p2.PlayerID)

	// 通知玩家匹配成功
	ctx.Send(p1.PID, &MatchFoundNotify{
		RoomPID:    roomPID,
		OpponentID: p2.PlayerID,
	})
	ctx.Send(p2.PID, &MatchFoundNotify{
		RoomPID:    roomPID,
		OpponentID: p1.PlayerID,
	})

	// 通知房间玩家就绪
	ctx.Send(roomPID, &PlayerReady{PlayerPID: p1.PID, PlayerID: p1.PlayerID})
	ctx.Send(roomPID, &PlayerReady{PlayerPID: p2.PID, PlayerID: p2.PlayerID})
}
