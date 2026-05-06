package mailbox

import "time"

// 邮箱调度状态机常量。
// 由 engine/actor 内部状态常量复制而来——外迁后 gamelib 邮箱独立维护。
const (
	idle      int32 = 0
	running   int32 = 1
	scheduled int32 = 2
)

// SchedulingStats Mailbox 调度指标快照。
// 与 v1.12 engine/actor.SchedulingStats 字段一致；adaptive 邮箱外迁后随同迁出。
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

// SchedulingMetricsProvider 提供调度指标的 Mailbox。
type SchedulingMetricsProvider interface {
	Stats() SchedulingStats
}
