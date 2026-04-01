package actor

import "time"

// MessageEnvelope 消息信封，携带发送者信息
type MessageEnvelope struct {
	Message interface{}
	Sender  *PID
}

// WrapEnvelope 包装消息为信封（如果有 sender），使用对象池减少 GC
func WrapEnvelope(message interface{}, sender *PID) interface{} {
	if sender == nil {
		return message
	}
	return AcquireEnvelope(message, sender)
}

// UnwrapEnvelope 解包消息信封，返回消息和发送者
func UnwrapEnvelope(message interface{}) (interface{}, *PID) {
	if env, ok := message.(*MessageEnvelope); ok {
		return env.Message, env.Sender
	}
	return message, nil
}

// 生命周期消息

// Started 在Actor启动时发送
type Started struct{}

// Stopping 在Actor停止前发送
type Stopping struct{}

// Stopped 在Actor停止后发送
type Stopped struct{}

// Restarting 在Actor重启时发送
type Restarting struct{}

// ReceiveTimeout 在Actor超时时发送
type ReceiveTimeout struct{}

// PoisonPill 优雅停止Actor的消息
type PoisonPill struct{}

// Watch 监视另一个Actor
type Watch struct {
	Watcher *PID
}

// Unwatch 取消监视
type Unwatch struct {
	Watcher *PID
}

// Terminated 当被监视的Actor终止时发送
type Terminated struct {
	Who *PID
}

// Failure 表示Actor失败
type Failure struct {
	Who    *PID
	Reason interface{}
	RestartStats *RestartStatistics
}

// RestartStatistics 重启统计
type RestartStatistics struct {
	FailureCount int
	LastFailureTime time.Time
}
