package actor

import "time"

// SchedulingStats Mailbox 调度指标快照
type SchedulingStats struct {
	// ScheduleCount 总调度次数
	ScheduleCount int64
	// ProcessedCount 已处理消息总数
	ProcessedCount int64
	// AvgLatency 每次调度的平均处理延迟
	AvgLatency time.Duration
	// MaxQueueDepth 历史最大队列深度
	MaxQueueDepth int64
	// YieldCount 协作式让出次数
	YieldCount int64
	// CurrentDepth 当前队列深度
	CurrentDepth int64
}

// SchedulingMetricsProvider 提供调度指标的 Mailbox
type SchedulingMetricsProvider interface {
	Stats() SchedulingStats
}
