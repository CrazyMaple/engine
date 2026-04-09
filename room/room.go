package room

import "time"

// RoomState 房间状态
type RoomState int

const (
	RoomStateWaiting  RoomState = iota // 等待玩家加入
	RoomStateRunning                   // 游戏运行中
	RoomStateFinished                  // 结算中
	RoomStateStopped                   // 已销毁
)

func (s RoomState) String() string {
	switch s {
	case RoomStateWaiting:
		return "waiting"
	case RoomStateRunning:
		return "running"
	case RoomStateFinished:
		return "finished"
	case RoomStateStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// RoomConfig 房间配置
type RoomConfig struct {
	// RoomType 房间类型（如 "pvp_1v1", "pvp_5v5"）
	RoomType string
	// MinPlayers 最小玩家数（满足此数可以开始）
	MinPlayers int
	// MaxPlayers 最大玩家数
	MaxPlayers int
	// WaitTimeout 等待超时（超时后可选开始或取消）
	WaitTimeout time.Duration
	// GameTimeout 游戏运行超时（强制结算）
	GameTimeout time.Duration
	// Metadata 自定义房间元数据
	Metadata map[string]interface{}
}

// PlayerInfo 玩家信息
type PlayerInfo struct {
	PlayerID string
	Name     string
	Rating   float64 // ELO 分数
	Level    int
	Team     int // 队伍编号
	Metadata map[string]interface{}
}

// RoomInfo 房间信息快照（只读）
type RoomInfo struct {
	RoomID     string
	RoomType   string
	State      RoomState
	Players    []PlayerInfo
	CreatedAt  time.Time
	StartedAt  time.Time
	Config     RoomConfig
}

// PlayerCount 返回当前玩家数
func (r *RoomInfo) PlayerCount() int {
	return len(r.Players)
}

// IsFull 房间是否已满
func (r *RoomInfo) IsFull() bool {
	return len(r.Players) >= r.Config.MaxPlayers
}

// CanStart 是否可以开始游戏
func (r *RoomInfo) CanStart() bool {
	return r.State == RoomStateWaiting && len(r.Players) >= r.Config.MinPlayers
}

// --- 房间消息定义 ---

// CreateRoomRequest 创建房间请求
type CreateRoomRequest struct {
	Config  RoomConfig
	Creator *PlayerInfo // 创建者（可选，自动加入）
}

// CreateRoomResponse 创建房间响应
type CreateRoomResponse struct {
	RoomID string
	Error  string
}

// JoinRoomRequest 加入房间请求
type JoinRoomRequest struct {
	RoomID string
	Player PlayerInfo
}

// JoinRoomResponse 加入房间响应
type JoinRoomResponse struct {
	Success bool
	Error   string
	Room    *RoomInfo
}

// LeaveRoomRequest 离开房间请求
type LeaveRoomRequest struct {
	RoomID   string
	PlayerID string
}

// LeaveRoomResponse 离开房间响应
type LeaveRoomResponse struct {
	Success bool
	Error   string
}

// StartGameRequest 请求开始游戏
type StartGameRequest struct {
	RoomID string
}

// StartGameResponse 开始游戏响应
type StartGameResponse struct {
	Success bool
	Error   string
}

// FinishGameRequest 结束游戏
type FinishGameRequest struct {
	RoomID  string
	Results map[string]interface{} // 游戏结果
}

// ListRoomsRequest 列出房间
type ListRoomsRequest struct {
	RoomType string // 空字符串表示所有类型
	State    *RoomState // nil 表示所有状态
}

// ListRoomsResponse 房间列表响应
type ListRoomsResponse struct {
	Rooms []RoomInfo
}

// FindRoomRequest 查找房间
type FindRoomRequest struct {
	RoomID string
}

// FindRoomResponse 查找房间响应
type FindRoomResponse struct {
	Room  *RoomInfo
	Found bool
}

// DestroyRoomRequest 销毁房间
type DestroyRoomRequest struct {
	RoomID string
}

// RoomEvent 房间事件（通过 EventStream 发布）
type RoomEvent struct {
	Type     string    // "created", "started", "finished", "destroyed", "player_joined", "player_left"
	RoomID   string
	PlayerID string
	Time     time.Time
}
