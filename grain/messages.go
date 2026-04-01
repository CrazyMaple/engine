package grain

import "engine/actor"

// GrainIdentity 虚拟 Actor 的唯一标识
type GrainIdentity struct {
	Kind     string // 类型名称，如 "Player", "Room"
	Identity string // 实例 ID，如 "player-12345"
}

// String 返回 "Kind/Identity" 格式
func (gi *GrainIdentity) String() string {
	return gi.Kind + "/" + gi.Identity
}

// GrainInit 通知 Grain 其身份的初始化消息
type GrainInit struct {
	Identity *GrainIdentity
}

// ActivateRequest 请求激活 Grain
type ActivateRequest struct {
	Identity *GrainIdentity
}

// ActivateResponse 激活响应
type ActivateResponse struct {
	PID   *actor.PID // 激活的 Grain PID
	Error string     // 错误信息（为空表示成功）
}

// DeactivateRequest 请求去激活 Grain
type DeactivateRequest struct {
	Identity *GrainIdentity
}
