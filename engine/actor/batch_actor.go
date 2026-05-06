package actor

// BatchActor 批处理 Actor 接口。
// 实现此接口的 Actor 将接收批量消息投递，适用于 DB 写入合并、日志聚合、指标汇总等场景。
//
// 与 BatchAwareMailbox 联动（见 mailbox.go）：spawn 时 engine 会把 BatchActor.BatchReceive
// 包成闭包传给 mailbox 的 RegisterBatchHandler；批处理邮箱实现位于 gamelib/actor/mailbox/。
type BatchActor interface {
	Actor
	// BatchReceive 批量消息回调，messages 为累积的用户消息切片。
	BatchReceive(ctx Context, messages []interface{})
}
