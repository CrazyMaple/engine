package mailbox

import (
	"sync/atomic"

	"engine/actor"
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

// MessagePrioritizer 消息优先级判定接口。业务层实现，根据消息类型返回优先级。
type MessagePrioritizer interface {
	Priority(msg interface{}) MessagePriority
}

// MessagePrioritizerFunc 函数式优先级判定器。
type MessagePrioritizerFunc func(msg interface{}) MessagePriority

func (f MessagePrioritizerFunc) Priority(msg interface{}) MessagePriority {
	return f(msg)
}

// PriorityConfig 优先级邮箱配置
type PriorityConfig struct {
	// Prioritizer 消息优先级判定器（必须）
	Prioritizer MessagePrioritizer

	// StarvationThreshold 饥饿阈值：低优先级消息等待次数超过此值后提升优先级
	// 0 表示不启用饥饿检测
	StarvationThreshold int

	// BackpressureConfig 可选背压配置（仅对 Low 优先级生效）
	BackpressureConfig *actor.BackpressureConfig
}

// priority 三级优先级邮箱：处理顺序 SystemMessages > High > Normal > Low。
type priority struct {
	systemQueue *mpscQueue
	highQueue   *mpscQueue
	normalQueue *mpscQueue
	lowQueue    *mpscQueue

	prioritizer         MessagePrioritizer
	starvationThreshold int
	lowSkipCount        int32

	userInvoker   func(interface{})
	systemInvoker func(interface{})
	status        int32
	scheduler     atomic.Value

	backpressure *actor.BackpressureConfig
	lowQueueSize int32
	ownerPID     *actor.PID
	eventStream  *actor.EventStream
}

// NewPriority 创建优先级邮箱。
func NewPriority(config PriorityConfig) actor.Mailbox {
	if config.Prioritizer == nil {
		config.Prioritizer = MessagePrioritizerFunc(func(msg interface{}) MessagePriority {
			return PriorityNormal
		})
	}

	return &priority{
		systemQueue:         newMpscQueue(),
		highQueue:           newMpscQueue(),
		normalQueue:         newMpscQueue(),
		lowQueue:            newMpscQueue(),
		prioritizer:         config.Prioritizer,
		starvationThreshold: config.StarvationThreshold,
		backpressure:        config.BackpressureConfig,
		status:              idle,
	}
}

func (m *priority) PostUserMessage(message interface{}) {
	msg, _ := actor.UnwrapEnvelope(message)
	p := m.prioritizer.Priority(msg)

	switch p {
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

func (m *priority) PostSystemMessage(message interface{}) {
	m.systemQueue.Push(message)
	m.schedule()
}

func (m *priority) RegisterHandlers(userInvoker, systemInvoker func(interface{})) {
	m.userInvoker = userInvoker
	m.systemInvoker = systemInvoker
}

func (m *priority) Start() {}

// SetScheduler 实现 actor.SchedulerAware。
func (m *priority) SetScheduler(scheduler actor.Dispatcher) {
	m.scheduler.Store(scheduler)
}

// SetOwnerPID 实现 actor.OwnerAware。
func (m *priority) SetOwnerPID(pid *actor.PID) {
	m.ownerPID = pid
}

// SetEventStream 实现 actor.EventStreamAware。
func (m *priority) SetEventStream(es *actor.EventStream) {
	m.eventStream = es
}

func (m *priority) schedule() {
	if atomic.CompareAndSwapInt32(&m.status, idle, scheduled) {
		scheduler := m.scheduler.Load()
		if scheduler != nil {
			scheduler.(actor.Dispatcher).Schedule(m.run)
		}
	}
}

func (m *priority) run() {
	atomic.StoreInt32(&m.status, running)

	for !m.systemQueue.Empty() {
		msg := m.systemQueue.Pop()
		if msg != nil && m.systemInvoker != nil {
			m.systemInvoker(msg)
		}
	}

	throughput := 10
	if s := m.scheduler.Load(); s != nil {
		if t := s.(actor.Dispatcher).Throughput(); t > 0 {
			throughput = t
		}
	}

	processed := 0
	for processed < throughput {
		if !m.highQueue.Empty() {
			msg := m.highQueue.Pop()
			if msg != nil && m.userInvoker != nil {
				m.userInvoker(msg)
				processed++
				continue
			}
		}

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

		if !m.normalQueue.Empty() {
			msg := m.normalQueue.Pop()
			if msg != nil && m.userInvoker != nil {
				m.userInvoker(msg)
				processed++
				if !m.lowQueue.Empty() {
					atomic.AddInt32(&m.lowSkipCount, 1)
				}
				continue
			}
		}

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

		break
	}

	atomic.StoreInt32(&m.status, idle)

	if !m.systemQueue.Empty() || !m.highQueue.Empty() || !m.normalQueue.Empty() || !m.lowQueue.Empty() {
		m.schedule()
	}
}

func (m *priority) shouldDropLow(message interface{}) bool {
	if m.backpressure == nil || m.backpressure.HighWatermark <= 0 {
		return false
	}

	size := int(atomic.LoadInt32(&m.lowQueueSize))
	if size < m.backpressure.HighWatermark {
		return false
	}

	if m.eventStream != nil && m.ownerPID != nil {
		m.eventStream.Publish(actor.MailboxOverflowEvent{
			PID:       m.ownerPID,
			QueueSize: size,
			Watermark: m.backpressure.HighWatermark,
			Strategy:  m.backpressure.Strategy,
		})
	}

	switch m.backpressure.Strategy {
	case actor.StrategyDropNewest:
		return true
	case actor.StrategyDropOldest:
		m.lowQueue.Pop()
		return false
	default:
		return true
	}
}
