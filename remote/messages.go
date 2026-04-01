package remote

import "engine/actor"

// RemoteMessage 远程消息封装
type RemoteMessage struct {
	Target   *actor.PID  // 目标Actor
	Sender   *actor.PID  // 发送者
	Message  interface{} // 消息内容
	Type     MessageType // 消息类型
	TypeName string      // 消息类型名称，用于反序列化
}

// MessageType 消息类型
type MessageType int

const (
	MessageTypeUser   MessageType = 0 // 用户消息
	MessageTypeSystem MessageType = 1 // 系统消息
)

// RemoteMessageBatch 批量远程消息，用于减少网络 I/O 次数
type RemoteMessageBatch struct {
	Messages []*RemoteMessage
}

// ConnectRequest 连接请求
type ConnectRequest struct {
	Address string // 本地地址
}

// ConnectResponse 连接响应
type ConnectResponse struct {
	Success bool
	Error   string
}

// DisconnectRequest 断开连接请求
type DisconnectRequest struct {
	Address string
}
