package actor

// BackpressureStrategy 背压策略类型
type BackpressureStrategy int

const (
	// StrategyDropOldest 丢弃最旧消息，接受新消息
	StrategyDropOldest BackpressureStrategy = iota
	// StrategyDropNewest 丢弃当前新到的消息
	StrategyDropNewest
	// StrategyBlock 阻塞发送方，直到邮箱水位降低
	StrategyBlock
	// StrategyNotify 接受消息，但向 Actor 发送 MailboxOverflow 系统消息通知
	StrategyNotify
)

// BackpressureConfig 邮箱背压配置
type BackpressureConfig struct {
	// HighWatermark 高水位标记，达到此阈值触发背压；0 表示不启用
	HighWatermark int
	// LowWatermark 低水位标记，降到此阈值解除 Block 策略的阻塞
	// 仅 StrategyBlock 使用，默认为 HighWatermark 的 50%
	LowWatermark int
	// Strategy 背压策略
	Strategy BackpressureStrategy
}

// MailboxOverflowEvent 邮箱溢出事件，发布到 EventStream
type MailboxOverflowEvent struct {
	PID       *PID
	QueueSize int
	Watermark int
	Strategy  BackpressureStrategy
}

// MailboxResumedEvent 邮箱恢复事件（Block 策略解除阻塞时发布）
type MailboxResumedEvent struct {
	PID       *PID
	QueueSize int
}
