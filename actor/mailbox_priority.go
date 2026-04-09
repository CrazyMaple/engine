package actor

import (
	"engine/internal"
	"sync/atomic"
)

// MessagePriority 消息优先级
type MessagePriority int

const (
	// PriorityHigh 高优先级（心跳、关键业务消息）
	PriorityHigh MessagePriority = iota
	// PriorityNormal 普通优先级（常规业务消息）
	PriorityNormal
	// PriorityLow 低优先级（日志、统计等非关键消息）
	PriorityLow
)

// MessagePrioritizer 消息优先级判定接口
// 业务层实现此接口，根据消息类型返回优先级
type MessagePrioritizer interface {
	Priority(msg interface{}) MessagePriority
}

// MessagePrioritizerFunc 函数式优先级判定器
type MessagePrioritizerFunc func(msg interface{}) MessagePriority

func (f MessagePrioritizerFunc) Priority(msg interface{}) MessagePriority {
	return f(msg)
}

// PriorityMailboxConfig 优先级邮箱配置
type PriorityMailboxConfig struct {
	// Prioritizer 消息优先级判定器（必须）
	Prioritizer MessagePrioritizer

	// StarvationThreshold 饥饿阈值：低优先级消息等待次数超过此值后提升优先级
	// 0 表示不启用饥饿检测
	StarvationThreshold int

	// BackpressureConfig 可选背压配置（仅对 Low 优先级生效）
	BackpressureConfig *BackpressureConfig
}

// priorityMailbox 三级优先级邮箱
// 处理顺序：SystemMessages > High > Normal > Low
type priorityMailbox struct {
	systemQueue *internal.Queue
	highQueue   *internal.Queue
	normalQueue *internal.Queue
	lowQueue    *internal.Queue

	prioritizer         MessagePrioritizer
	starvationThreshold int
	lowSkipCount        int32 // 低优先级被跳过次数（原子操作）

	userInvoker   func(interface{})
	systemInvoker func(interface{})
	status        int32
	scheduler     atomic.Value

	// 背压相关（仅 Low 队列）
	backpressure *BackpressureConfig
	lowQueueSize int32
	ownerPID     *PID
	eventStream  *EventStream
}

// NewPriorityMailbox 创建优先级邮箱
func NewPriorityMailbox(config PriorityMailboxConfig) Mailbox {
	if config.Prioritizer == nil {
		config.Prioritizer = MessagePrioritizerFunc(func(msg interface{}) MessagePriority {
			return PriorityNormal
		})
	}

	return &priorityMailbox{
		systemQueue:         internal.NewQueue(),
		highQueue:           internal.NewQueue(),
		normalQueue:         internal.NewQueue(),
		lowQueue:            internal.NewQueue(),
		prioritizer:         config.Prioritizer,
		starvationThreshold: config.StarvationThreshold,
		backpressure:        config.BackpressureConfig,
		status:              idle,
	}
}

func (m *priorityMailbox) PostUserMessage(message interface{}) {
	msg, _ := UnwrapEnvelope(message)
	priority := m.prioritizer.Priority(msg)

	switch priority {
	case PriorityHigh:
		m.highQueue.Push(message)
	case PriorityLow:
		if m.shouldDropLow(message) {
			return
		}
		m.lowQueue.Push(message)
		atomic.AddInt32(&m.lowQueueSize, 1)
	default:
		m.normalQueue.Push(message)
	}

	m.schedule()
}

func (m *priorityMailbox) PostSystemMessage(message interface{}) {
	m.systemQueue.Push(message)
	m.schedule()
}

func (m *priorityMailbox) RegisterHandlers(userInvoker, systemInvoker func(interface{})) {
	m.userInvoker = userInvoker
	m.systemInvoker = systemInvoker
}

func (m *priorityMailbox) Start() {}

// SetScheduler 设置调度器
func (m *priorityMailbox) SetScheduler(scheduler Dispatcher) {
	m.scheduler.Store(scheduler)
}

// SetOwnerPID 设置所有者 PID（用于背压事件）
func (m *priorityMailbox) SetOwnerPID(pid *PID) {
	m.ownerPID = pid
}

// SetEventStream 设置事件流（用于背压事件发布）
func (m *priorityMailbox) SetEventStream(es *EventStream) {
	m.eventStream = es
}

func (m *priorityMailbox) schedule() {
	if atomic.CompareAndSwapInt32(&m.status, idle, scheduled) {
		scheduler := m.scheduler.Load()
		if scheduler != nil {
			scheduler.(Dispatcher).Schedule(m.run)
		}
	}
}

func (m *priorityMailbox) run() {
	atomic.StoreInt32(&m.status, running)

	// 1. 系统消息（最高优先级）
	for !m.systemQueue.Empty() {
		msg := m.systemQueue.Pop()
		if msg != nil && m.systemInvoker != nil {
			m.systemInvoker(msg)
		}
	}

	// 2. 获取吞吐量配置
	throughput := 10
	if s := m.scheduler.Load(); s != nil {
		if t := s.(Dispatcher).Throughput(); t > 0 {
			throughput = t
		}
	}

	// 3. 按优先级处理用户消息
	processed := 0
	for processed < throughput {
		// High 优先级
		if !m.highQueue.Empty() {
			msg := m.highQueue.Pop()
			if msg != nil && m.userInvoker != nil {
				m.userInvoker(msg)
				processed++
				continue
			}
		}

		// 饥饿检测：低优先级等待过久时提升
		if m.starvationThreshold > 0 && atomic.LoadInt32(&m.lowSkipCount) >= int32(m.starvationThreshold) {
			if !m.lowQueue.Empty() {
				msg := m.lowQueue.Pop()
				if msg != nil && m.userInvoker != nil {
					m.userInvoker(msg)
					atomic.StoreInt32(&m.lowSkipCount, 0)
					atomic.AddInt32(&m.lowQueueSize, -1)
					processed++
					continue
				}
			}
		}

		// Normal 优先级
		if !m.normalQueue.Empty() {
			msg := m.normalQueue.Pop()
			if msg != nil && m.userInvoker != nil {
				m.userInvoker(msg)
				processed++
				// 记录低优先级被跳过
				if !m.lowQueue.Empty() {
					atomic.AddInt32(&m.lowSkipCount, 1)
				}
				continue
			}
		}

		// Low 优先级
		if !m.lowQueue.Empty() {
			msg := m.lowQueue.Pop()
			if msg != nil && m.userInvoker != nil {
				m.userInvoker(msg)
				atomic.StoreInt32(&m.lowSkipCount, 0)
				atomic.AddInt32(&m.lowQueueSize, -1)
				processed++
				continue
			}
		}

		break // 所有队列为空
	}

	atomic.StoreInt32(&m.status, idle)

	// 如果还有消息，重新调度
	if !m.systemQueue.Empty() || !m.highQueue.Empty() || !m.normalQueue.Empty() || !m.lowQueue.Empty() {
		m.schedule()
	}
}

// shouldDropLow 背压策略：判断是否丢弃低优先级消息
func (m *priorityMailbox) shouldDropLow(message interface{}) bool {
	if m.backpressure == nil || m.backpressure.HighWatermark <= 0 {
		return false
	}

	size := int(atomic.LoadInt32(&m.lowQueueSize))
	if size < m.backpressure.HighWatermark {
		return false
	}

	// 触发背压：丢弃低优先级消息
	if m.eventStream != nil && m.ownerPID != nil {
		m.eventStream.Publish(MailboxOverflowEvent{
			PID:       m.ownerPID,
			QueueSize: size,
			Watermark: m.backpressure.HighWatermark,
			Strategy:  m.backpressure.Strategy,
		})
	}

	switch m.backpressure.Strategy {
	case StrategyDropNewest:
		return true // 丢弃当前新消息
	case StrategyDropOldest:
		m.lowQueue.Pop() // 丢弃最旧消息，接受新消息
		return false
	default:
		return true // 其他策略默认丢弃
	}
}

// WithPriorityMailbox 创建优先级邮箱
func (props *Props) WithPriorityMailbox(config PriorityMailboxConfig) *Props {
	props.mailbox = func() Mailbox {
		return NewPriorityMailbox(config)
	}
	return props
}
