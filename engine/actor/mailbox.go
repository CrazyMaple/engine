package actor

import (
	"engine/internal"
	"sync/atomic"
)

// Mailbox 消息邮箱接口
type Mailbox interface {
	PostUserMessage(message interface{})
	PostSystemMessage(message interface{})
	RegisterHandlers(userInvoker, systemInvoker func(interface{}))
	Start()
}

// 以下为 Mailbox 的可选扩展接口（v1.13 契约化，外迁到 gamelib 的高级 mailbox 按需选择实现）。
// engine 在 spawn 路径用接口断言判断 mailbox 是否实现某能力，从而避免对私有类型的硬编码断言。

// SchedulerAware 邮箱可接收调度器注入。defaultMailbox / backpressureMailbox 等内置实现都实现该接口。
type SchedulerAware interface {
	SetScheduler(Dispatcher)
}

// OwnerAware 邮箱可接收所属 Actor PID 注入（用于背压等需要 PID 标识的事件发布）。
type OwnerAware interface {
	SetOwnerPID(*PID)
}

// EventStreamAware 邮箱可接收事件流注入（用于发布背压溢出等可观测事件）。
type EventStreamAware interface {
	SetEventStream(*EventStream)
}

// BatchAwareMailbox 邮箱可接收批处理回调注册。
//
// 注：actor 侧契约仍为 BatchActor.BatchReceive(ctx Context, []interface{})；
// Mailbox 不直接持有 actor 接口，二者通过 RegisterBatchHandler 传入的闭包桥接，
// 该闭包在 spawn 时由 engine 注入，闭包内会回调 actor.BatchReceive(ctx, msgs)。
type BatchAwareMailbox interface {
	RegisterBatchHandler(func([]interface{}))
}

// defaultMailbox 默认邮箱实现
type defaultMailbox struct {
	userMailbox   *internal.Queue
	systemMailbox *internal.Queue
	userInvoker   func(interface{})
	systemInvoker func(interface{})
	status        int32
	scheduler     atomic.Value
}

const (
	idle      int32 = 0
	running   int32 = 1
	scheduled int32 = 2
)

// NewDefaultMailbox 创建默认邮箱
func NewDefaultMailbox() Mailbox {
	return &defaultMailbox{
		userMailbox:   internal.NewQueue(),
		systemMailbox: internal.NewQueue(),
		status:        idle,
	}
}

func (m *defaultMailbox) PostUserMessage(message interface{}) {
	m.userMailbox.Push(message)
	m.schedule()
}

func (m *defaultMailbox) PostSystemMessage(message interface{}) {
	m.systemMailbox.Push(message)
	m.schedule()
}

func (m *defaultMailbox) RegisterHandlers(userInvoker, systemInvoker func(interface{})) {
	m.userInvoker = userInvoker
	m.systemInvoker = systemInvoker
}

func (m *defaultMailbox) Start() {
	// 邮箱已准备好接收消息
}

func (m *defaultMailbox) schedule() {
	if atomic.CompareAndSwapInt32(&m.status, idle, scheduled) {
		scheduler := m.scheduler.Load()
		if scheduler != nil {
			scheduler.(Dispatcher).Schedule(m.run)
		}
	}
}

func (m *defaultMailbox) run() {
	atomic.StoreInt32(&m.status, running)

	// 优先处理系统消息
	for !m.systemMailbox.Empty() {
		msg := m.systemMailbox.Pop()
		if msg != nil && m.systemInvoker != nil {
			m.systemInvoker(msg)
		}
	}

	// 处理用户消息（批量处理，使用 Dispatcher 的 Throughput 配置）
	throughput := 10
	if s := m.scheduler.Load(); s != nil {
		if t := s.(Dispatcher).Throughput(); t > 0 {
			throughput = t
		}
	}
	for i := 0; i < throughput; i++ {
		if m.userMailbox.Empty() {
			break
		}
		msg := m.userMailbox.Pop()
		if msg != nil && m.userInvoker != nil {
			m.userInvoker(msg)
		}
	}

	atomic.StoreInt32(&m.status, idle)

	// 如果还有消息，重新调度
	if !m.userMailbox.Empty() || !m.systemMailbox.Empty() {
		m.schedule()
	}
}

// SetScheduler 设置调度器
func (m *defaultMailbox) SetScheduler(scheduler Dispatcher) {
	m.scheduler.Store(scheduler)
}
