package room

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// RoomManager 房间管理器
// 管理房间的创建、查找、列表、销毁
type RoomManager struct {
	rooms   map[string]*RoomInstance
	counter uint64
	mu      sync.RWMutex

	// 事件回调
	onEvent func(event RoomEvent)
}

// NewRoomManager 创建房间管理器
func NewRoomManager() *RoomManager {
	return &RoomManager{
		rooms: make(map[string]*RoomInstance),
	}
}

// SetEventHandler 设置事件回调
func (m *RoomManager) SetEventHandler(fn func(event RoomEvent)) {
	m.onEvent = fn
}

// CreateRoom 创建房间
func (m *RoomManager) CreateRoom(config RoomConfig, creator *PlayerInfo) (string, error) {
	roomID := fmt.Sprintf("room_%d_%d", time.Now().UnixMilli(), atomic.AddUint64(&m.counter, 1))

	room := NewRoomInstance(roomID, config)
	room.SetTimeoutCallback(m.handleTimeout)

	m.mu.Lock()
	m.rooms[roomID] = room
	m.mu.Unlock()

	m.publishEvent(RoomEvent{Type: "created", RoomID: roomID, Time: time.Now()})

	// 创建者自动加入
	if creator != nil {
		if err := m.JoinRoom(roomID, *creator); err != nil {
			// 回滚：删除房间
			m.mu.Lock()
			delete(m.rooms, roomID)
			m.mu.Unlock()
			return "", fmt.Errorf("creator join failed: %w", err)
		}
	}

	return roomID, nil
}

// JoinRoom 加入房间
func (m *RoomManager) JoinRoom(roomID string, player PlayerInfo) error {
	room, ok := m.getRoom(roomID)
	if !ok {
		return fmt.Errorf("room %s not found", roomID)
	}

	if err := room.Join(player); err != nil {
		return err
	}

	m.publishEvent(RoomEvent{Type: "player_joined", RoomID: roomID, PlayerID: player.PlayerID, Time: time.Now()})

	// 自动开始：满员时自动触发
	if room.IsFull() && room.CanStart() {
		return m.StartGame(roomID)
	}

	return nil
}

// LeaveRoom 离开房间
func (m *RoomManager) LeaveRoom(roomID, playerID string) error {
	room, ok := m.getRoom(roomID)
	if !ok {
		return fmt.Errorf("room %s not found", roomID)
	}

	if err := room.Leave(playerID); err != nil {
		return err
	}

	m.publishEvent(RoomEvent{Type: "player_left", RoomID: roomID, PlayerID: playerID, Time: time.Now()})

	// 等待阶段所有人离开则销毁
	if room.PlayerCount() == 0 && room.State == RoomStateWaiting {
		m.DestroyRoom(roomID)
	}

	return nil
}

// StartGame 开始游戏
func (m *RoomManager) StartGame(roomID string) error {
	room, ok := m.getRoom(roomID)
	if !ok {
		return fmt.Errorf("room %s not found", roomID)
	}

	if err := room.Start(); err != nil {
		return err
	}

	m.publishEvent(RoomEvent{Type: "started", RoomID: roomID, Time: time.Now()})
	return nil
}

// FinishGame 结束游戏
func (m *RoomManager) FinishGame(roomID string) error {
	room, ok := m.getRoom(roomID)
	if !ok {
		return fmt.Errorf("room %s not found", roomID)
	}

	if err := room.Finish(); err != nil {
		return err
	}

	m.publishEvent(RoomEvent{Type: "finished", RoomID: roomID, Time: time.Now()})
	return nil
}

// DestroyRoom 销毁房间
func (m *RoomManager) DestroyRoom(roomID string) {
	m.mu.Lock()
	room, ok := m.rooms[roomID]
	if ok {
		room.Destroy()
		delete(m.rooms, roomID)
	}
	m.mu.Unlock()

	if ok {
		m.publishEvent(RoomEvent{Type: "destroyed", RoomID: roomID, Time: time.Now()})
	}
}

// FindRoom 查找房间
func (m *RoomManager) FindRoom(roomID string) (*RoomInfo, bool) {
	room, ok := m.getRoom(roomID)
	if !ok {
		return nil, false
	}
	info := room.Info()
	return &info, true
}

// ListRooms 列出房间
func (m *RoomManager) ListRooms(roomType string, state *RoomState) []RoomInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]RoomInfo, 0)
	for _, room := range m.rooms {
		if roomType != "" && room.Config.RoomType != roomType {
			continue
		}
		if state != nil && room.State != *state {
			continue
		}
		result = append(result, room.Info())
	}
	return result
}

// RoomCount 返回房间总数
func (m *RoomManager) RoomCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.rooms)
}

// FindAvailableRoom 查找可加入的房间（按类型）
func (m *RoomManager) FindAvailableRoom(roomType string) (*RoomInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, room := range m.rooms {
		if room.Config.RoomType == roomType && room.State == RoomStateWaiting && !room.IsFull() {
			info := room.Info()
			return &info, true
		}
	}
	return nil, false
}

func (m *RoomManager) getRoom(roomID string) (*RoomInstance, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	room, ok := m.rooms[roomID]
	return room, ok
}

func (m *RoomManager) handleTimeout(roomID string, event string) {
	switch event {
	case "wait_timeout":
		room, ok := m.getRoom(roomID)
		if !ok {
			return
		}
		// 等待超时：如果够人就开始，否则销毁
		if room.CanStart() {
			m.StartGame(roomID)
		} else {
			m.DestroyRoom(roomID)
		}
	case "game_timeout":
		// 游戏超时：强制结算
		m.FinishGame(roomID)
	}
}

func (m *RoomManager) publishEvent(event RoomEvent) {
	if m.onEvent != nil {
		m.onEvent(event)
	}
}
