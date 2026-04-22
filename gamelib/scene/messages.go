package scene

import "engine/actor"

// --- 请求消息（外部 → Scene Actor）---

// EnterScene 进入场景
type EnterScene struct {
	EntityID string
	PID      *actor.PID
	X, Y     float32
	Data     interface{}
}

// LeaveScene 离开场景
type LeaveScene struct {
	EntityID string
}

// MoveInScene 在场景中移动
type MoveInScene struct {
	EntityID string
	X, Y     float32
}

// BroadcastToScene 全场景广播
type BroadcastToScene struct {
	Message   interface{}
	ExcludeID string // 可选：排除的实体
}

// BroadcastToAOI 向实体 AOI 范围广播
type BroadcastToAOI struct {
	EntityID    string
	Message     interface{}
	IncludeSelf bool
}

// GetSceneInfo 查询场景信息
type GetSceneInfo struct{}

// TransferEntity 跨场景实体转移请求
// 由外部发给源场景，源场景执行离场 + 状态打包，然后通知目标场景接收
type TransferEntity struct {
	EntityID      string
	TargetSceneID string
	TargetX, TargetY float32
}

// TransferIn 转移入场消息（源场景 → 目标场景）
type TransferIn struct {
	EntityID string
	PID      *actor.PID
	X, Y     float32
	Data     interface{}
}

// TransferResult 转移结果通知（目标场景 → 实体 PID）
type TransferResult struct {
	Success       bool
	SourceSceneID string
	TargetSceneID string
	EntityID      string
	Reason        string // 失败原因
}

// StashMessage 暂存消息包装，转移期间的消息会被暂存
type StashMessage struct {
	EntityID string
	Message  interface{}
}

// FlushStash 冲刷暂存消息（转移完成后回放）
type FlushStash struct {
	EntityID string
}

// --- 通知消息（Scene Actor → 实体）---

// EntityEntered 有实体进入了你的视野
type EntityEntered struct {
	EntityID string
	X, Y     float32
	Data     interface{}
}

// EntityLeft 有实体离开了你的视野
type EntityLeft struct {
	EntityID string
}

// EntityMoved 有实体在你视野内移动
type EntityMoved struct {
	EntityID string
	X, Y     float32
}

// SceneInfo 场景信息
type SceneInfo struct {
	SceneID  string
	Entities []EntitySnapshot
}

// EntitySnapshot 实体快照
type EntitySnapshot struct {
	ID   string
	X, Y float32
	Data interface{}
}

// --- 跨场景 AOI 边界消息 ---

// RegisterAdjacentScene 注册相邻场景（用于边界 AOI）
type RegisterAdjacentScene struct {
	SceneID   string
	ScenePID  *actor.PID
	Direction AdjacentDirection // 相邻方向
	Overlap   float32           // 边界重叠区域宽度
}

// UnregisterAdjacentScene 注销相邻场景
type UnregisterAdjacentScene struct {
	SceneID string
}

// BorderEntityUpdate 边界实体更新（发给相邻场景）
type BorderEntityUpdate struct {
	SourceSceneID string
	EntityID      string
	X, Y          float32
	Data          interface{}
	Entered       bool // true=进入边界区, false=离开边界区
}

// AdjacentDirection 相邻方向
type AdjacentDirection int

const (
	AdjacentNorth     AdjacentDirection = iota // 上
	AdjacentSouth                              // 下
	AdjacentEast                               // 右
	AdjacentWest                               // 左
	AdjacentNorthEast                          // 右上
	AdjacentNorthWest                          // 左上
	AdjacentSouthEast                          // 右下
	AdjacentSouthWest                          // 左下
)
