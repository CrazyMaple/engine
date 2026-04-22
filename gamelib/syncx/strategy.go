package syncx

// BroadcastFunc 广播回调函数，由外部（如 RoomActor）提供
// playerIDs 为 nil 表示广播给所有玩家
type BroadcastFunc func(playerIDs []string, msg interface{})

// SyncStrategy 同步策略接口
type SyncStrategy interface {
	// OnPlayerJoin 玩家加入
	OnPlayerJoin(playerID string)
	// OnPlayerLeave 玩家离开
	OnPlayerLeave(playerID string)
	// OnPlayerInput 接收玩家输入
	OnPlayerInput(playerID string, input *PlayerInput)
	// Tick 帧更新
	Tick(frameNum uint64)
	// FrameNum 当前帧号
	FrameNum() uint64
}
