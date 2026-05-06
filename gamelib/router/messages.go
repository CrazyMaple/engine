package router

import "engine/actor"

// AddRoutee 添加路由目标
type AddRoutee struct {
	PID *actor.PID
}

// RemoveRoutee 移除路由目标
type RemoveRoutee struct {
	PID *actor.PID
}

// GetRoutees 获取所有路由目标
type GetRoutees struct{}

// Routees 路由目标列表响应
type Routees struct {
	PIDs []*actor.PID
}

// Hasher 消息哈希接口，ConsistentHash 路由器使用
type Hasher interface {
	Hash() string
}
