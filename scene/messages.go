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
