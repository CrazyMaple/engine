package federation

import (
	"engine/actor"
)

// FederatedRouter 联邦路由器
// 实现 actor.Process 接口，拦截发往联邦 PID 的消息并通过 Gateway 转发
type FederatedRouter struct {
	gateway *Gateway
}

// NewFederatedRouter 创建联邦路由器
func NewFederatedRouter(gw *Gateway) *FederatedRouter {
	return &FederatedRouter{gateway: gw}
}

// SendUserMessage 发送用户消息（通过 Gateway 转发到对端集群）
func (r *FederatedRouter) SendUserMessage(pid *actor.PID, message interface{}) {
	// 解包 envelope 获取真实消息和 sender
	var sender *actor.PID
	var realMsg interface{}

	if env, ok := message.(*actor.MessageEnvelope); ok {
		sender = env.Sender
		realMsg = env.Message
	} else {
		realMsg = message
	}

	r.gateway.Send(pid, sender, realMsg)
}

// SendSystemMessage 发送系统消息（联邦不转发系统消息）
func (r *FederatedRouter) SendSystemMessage(pid *actor.PID, message interface{}) {
	// 系统消息不跨集群转发
}

// Stop 停止（no-op）
func (r *FederatedRouter) Stop(pid *actor.PID) {
	// no-op: 远程 Actor 的生命周期由对端集群管理
}
