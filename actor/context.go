package actor

import "time"

// Context 是Actor的上下文接口组合
type Context interface {
	InfoContext
	MessageContext
	SenderContext
	ReceiverContext
	SpawnerContext
}

// InfoContext 提供Actor信息
type InfoContext interface {
	Self() *PID
	Parent() *PID
	Children() []*PID
}

// MessageContext 提供消息访问
type MessageContext interface {
	Message() interface{}
}

// SenderContext 提供消息发送能力
type SenderContext interface {
	Send(pid *PID, message interface{})
	Request(pid *PID, message interface{})
	RequestFuture(pid *PID, message interface{}, timeout time.Duration) *Future
}

// ReceiverContext 提供消息接收控制
type ReceiverContext interface {
	Respond(message interface{})
	Sender() *PID
	SetReceiveTimeout(timeout time.Duration)
	CancelReceiveTimeout()
}

// SpawnerContext 提供Actor创建能力
type SpawnerContext interface {
	Spawn(props *Props) *PID
	SpawnNamed(props *Props, name string) *PID
	StopActor(pid *PID)
	Watch(pid *PID)
	Unwatch(pid *PID)
}
