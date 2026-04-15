package syncx

// PlayerInput 玩家输入数据
type PlayerInput struct {
	PlayerID string
	FrameNum uint64
	Actions  []InputAction
}

// InputAction 单个输入动作
type InputAction struct {
	Type uint16                 // 动作类型（业务层定义）
	Data map[string]interface{} // 动作参数
}

// FrameData 帧同步广播数据（一帧中所有玩家输入）
type FrameData struct {
	FrameNum uint64
	Inputs   []PlayerInput
	Hash     uint32 // 帧数据哈希（用于校验）
}

// StateDelta 状态同步增量数据
type StateDelta struct {
	FrameNum uint64
	Changed  map[string]interface{} // 变化的状态 key→value
	Removed  []string               // 被删除的状态 key
}

// StateSnapshot 完整状态快照（用于新加入玩家或重连）
type StateSnapshot struct {
	FrameNum uint64
	State    map[string]interface{}
}

// PingMsg RTT 测量请求
type PingMsg struct {
	PlayerID  string
	Timestamp int64 // 发送时间（UnixNano）
}

// PongMsg RTT 测量响应
type PongMsg struct {
	PlayerID  string
	Timestamp int64 // 原始发送时间
}

// SyncConfig 同步配置
type SyncConfig struct {
	// TickRate 服务端帧率
	TickRate int
	// InputBufferSize 输入缓冲帧数（帧同步）
	InputBufferSize int
	// SnapshotInterval 状态快照间隔帧数（状态同步）
	SnapshotInterval int
}

// DefaultSyncConfig 返回默认同步配置
func DefaultSyncConfig() SyncConfig {
	return SyncConfig{
		TickRate:         20,
		InputBufferSize:  3,
		SnapshotInterval: 60,
	}
}
