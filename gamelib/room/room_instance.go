package room

import (
	"fmt"
	"sync"
	"time"
)

// RoomInstance 房间实例（Actor 内部状态）
type RoomInstance struct {
	ID        string
	Config    RoomConfig
	State     RoomState
	Players   map[string]*PlayerInfo
	CreatedAt time.Time
	StartedAt time.Time
	EndedAt   time.Time

	waitTimer *time.Timer
	gameTimer *time.Timer
	onTimeout func(roomID string, event string) // 超时回调

	mu sync.RWMutex
}

// NewRoomInstance 创建房间实例
func NewRoomInstance(id string, config RoomConfig) *RoomInstance {
	return &RoomInstance{
		ID:        id,
		Config:    config,
		State:     RoomStateWaiting,
		Players:   make(map[string]*PlayerInfo),
		CreatedAt: time.Now(),
	}
}

// SetTimeoutCallback 设置超时回调
func (r *RoomInstance) SetTimeoutCallback(fn func(roomID string, event string)) {
	r.onTimeout = fn
}

// Join 玩家加入房间
func (r *RoomInstance) Join(player PlayerInfo) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != RoomStateWaiting {
		return fmt.Errorf("room %s is not accepting players (state: %s)", r.ID, r.State)
	}
	if len(r.Players) >= r.Config.MaxPlayers {
		return fmt.Errorf("room %s is full (%d/%d)", r.ID, len(r.Players), r.Config.MaxPlayers)
	}
	if _, exists := r.Players[player.PlayerID]; exists {
		return fmt.Errorf("player %s already in room %s", player.PlayerID, r.ID)
	}

	p := player // copy
	r.Players[player.PlayerID] = &p

	// 启动等待超时计时器（第一个玩家加入时启动）
	if len(r.Players) == 1 && r.Config.WaitTimeout > 0 && r.waitTimer == nil {
		r.waitTimer = time.AfterFunc(r.Config.WaitTimeout, func() {
			if r.onTimeout != nil {
				r.onTimeout(r.ID, "wait_timeout")
			}
		})
	}

	return nil
}

// Leave 玩家离开房间
func (r *RoomInstance) Leave(playerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.Players[playerID]; !exists {
		return fmt.Errorf("player %s not in room %s", playerID, r.ID)
	}

	delete(r.Players, playerID)
	return nil
}

// Start 开始游戏
func (r *RoomInstance) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != RoomStateWaiting {
		return fmt.Errorf("room %s cannot start (state: %s)", r.ID, r.State)
	}
	if len(r.Players) < r.Config.MinPlayers {
		return fmt.Errorf("room %s needs at least %d players, got %d",
			r.ID, r.Config.MinPlayers, len(r.Players))
	}

	r.State = RoomStateRunning
	r.StartedAt = time.Now()

	// 取消等待计时器
	if r.waitTimer != nil {
		r.waitTimer.Stop()
		r.waitTimer = nil
	}

	// 启动游戏超时计时器
	if r.Config.GameTimeout > 0 {
		r.gameTimer = time.AfterFunc(r.Config.GameTimeout, func() {
			if r.onTimeout != nil {
				r.onTimeout(r.ID, "game_timeout")
			}
		})
	}

	return nil
}

// Finish 结束游戏
func (r *RoomInstance) Finish() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != RoomStateRunning {
		return fmt.Errorf("room %s is not running (state: %s)", r.ID, r.State)
	}

	r.State = RoomStateFinished
	r.EndedAt = time.Now()

	if r.gameTimer != nil {
		r.gameTimer.Stop()
		r.gameTimer = nil
	}

	return nil
}

// Destroy 销毁房间
func (r *RoomInstance) Destroy() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.State = RoomStateStopped
	if r.waitTimer != nil {
		r.waitTimer.Stop()
	}
	if r.gameTimer != nil {
		r.gameTimer.Stop()
	}
}

// Info 返回房间信息快照
func (r *RoomInstance) Info() RoomInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	players := make([]PlayerInfo, 0, len(r.Players))
	for _, p := range r.Players {
		players = append(players, *p)
	}

	return RoomInfo{
		RoomID:    r.ID,
		RoomType:  r.Config.RoomType,
		State:     r.State,
		Players:   players,
		CreatedAt: r.CreatedAt,
		StartedAt: r.StartedAt,
		Config:    r.Config,
	}
}

// PlayerCount 玩家数
func (r *RoomInstance) PlayerCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.Players)
}

// IsFull 是否已满
func (r *RoomInstance) IsFull() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.Players) >= r.Config.MaxPlayers
}

// CanStart 是否可以开始
func (r *RoomInstance) CanStart() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.State == RoomStateWaiting && len(r.Players) >= r.Config.MinPlayers
}
