package actor

import "time"

// Context 是Actor的上下文接口组合
type Context interface {
	InfoContext
	MessageContext
	SenderContext
	ReceiverContext
	SpawnerContext
	StashContext
}

// StashContext 提供消息暂存能力
type StashContext interface {
	// Stash 将当前消息放入暂存栈，等待后续 UnstashAll 重新投递
	Stash() error
	// UnstashAll 将暂存栈中的消息按 FIFO 顺序重新投递到 Mailbox
	UnstashAll()
	// StashSize 返回当前暂存消息数量
	StashSize() int
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
	// TraceID 返回当前消息的链路追踪 ID（可能为空）
	TraceID() string
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
