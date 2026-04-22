package actor

import (
	"sync"
	"sync/atomic"

	"engine/internal"
)

// backpressureMailbox 带背压机制的有界邮箱
// 在 boundedMailbox 基础上增加可配置的背压策略和 EventStream 事件通知
type backpressureMailbox struct {
	userMessages  []interface{}
	userHead      int
	userTail      int
	userCount     int
	capacity      int
	config        BackpressureConfig

	systemMailbox *internal.Queue
	userInvoker   func(interface{})
	systemInvoker func(interface{})
	status        int32
	scheduler     atomic.Value
	mu            sync.Mutex

	eventStream  *EventStream // 用于发布背压事件（可选）
	ownerPID     *PID         // 邮箱所属 Actor 的 PID
	droppedCount int64        // 丢弃消息计数（原子操作）
	resumed      chan struct{}  // Block 策略使用的恢复信号
}

// NewBackpressureMailbox 创建带背压机制的有界邮箱
func NewBackpressureMailbox(capacity int, config BackpressureConfig) Mailbox {
	if capacity <= 0 {
		capacity = 1024
	}
	if config.HighWatermark <= 0 {
		config.HighWatermark = capacity
	}
	if config.LowWatermark <= 0 {
		config.LowWatermark = config.HighWatermark / 2
	}
	if config.LowWatermark >= config.HighWatermark {
		config.LowWatermark = config.HighWatermark / 2
	}

	mb := &backpressureMailbox{
		userMessages:  make([]interface{}, capacity),
		capacity:      capacity,
		config:        config,
		systemMailbox: internal.NewQueue(),
		status:        idle,
	}
	if config.Strategy == StrategyBlock {
		mb.resumed = make(chan struct{}, 1)
	}
	return mb
}

func (m *backpressureMailbox) PostUserMessage(message interface{}) {
	m.mu.Lock()

	if m.userCount >= m.config.HighWatermark {
		switch m.config.Strategy {
		case StrategyDropOldest:
			// 丢弃最旧消息，腾出空间
			m.userTail = (m.userTail + 1) % m.capacity
			m.userCount--
			atomic.AddInt64(&m.droppedCount, 1)
			m.publishOverflowLocked()

		case StrategyDropNewest:
			// 丢弃当前新消息
			atomic.AddInt64(&m.droppedCount, 1)
			m.publishOverflowLocked()
			m.mu.Unlock()
			return

		case StrategyBlock:
			// 释放锁，等待消费者腾出空间
			m.publishOverflowLocked()
			m.mu.Unlock()
			<-m.resumed // 阻塞直到 run() 检测到低水位
			m.mu.Lock()

		case StrategyNotify:
			// 接受消息，但通知 Actor
			m.publishOverflowLocked()
		}
	}

	// 写入消息（环形缓冲区可能已满但通过丢弃/阻塞腾出了空间）
	if m.userCount < m.capacity {
		m.userMessages[m.userHead] = message
		m.userHead = (m.userHead + 1) % m.capacity
		m.userCount++
	}
	m.mu.Unlock()

	m.schedule()
}

func (m *backpressureMailbox) PostSystemMessage(message interface{}) {
	// 系统消息不受背压限制
	m.systemMailbox.Push(message)
	m.schedule()
}

func (m *backpressureMailbox) RegisterHandlers(userInvoker, systemInvoker func(interface{})) {
	m.userInvoker = userInvoker
	m.systemInvoker = systemInvoker
}

func (m *backpressureMailbox) Start() {}

func (m *backpressureMailbox) schedule() {
	if atomic.CompareAndSwapInt32(&m.status, idle, scheduled) {
		scheduler := m.scheduler.Load()
		if scheduler != nil {
			scheduler.(Dispatcher).Schedule(m.run)
		}
	}
}

func (m *backpressureMailbox) run() {
	atomic.StoreInt32(&m.status, running)

	// 优先处理系统消息
	for !m.systemMailbox.Empty() {
		msg := m.systemMailbox.Pop()
		if msg != nil && m.systemInvoker != nil {
			m.systemInvoker(msg)
		}
	}

	// 处理用户消息
	throughput := 10
	if s := m.scheduler.Load(); s != nil {
		if t := s.(Dispatcher).Throughput(); t > 0 {
			throughput = t
		}
	}
	for i := 0; i < throughput; i++ {
		m.mu.Lock()
		if m.userCount == 0 {
			m.mu.Unlock()
			break
		}
		msg := m.userMessages[m.userTail]
		m.userMessages[m.userTail] = nil
		m.userTail = (m.userTail + 1) % m.capacity
		m.userCount--
		count := m.userCount
		m.mu.Unlock()

		if msg != nil && m.userInvoker != nil {
			m.userInvoker(msg)
		}

		// Block 策略：低于低水位时通知被阻塞的发送方
		if m.config.Strategy == StrategyBlock && count <= m.config.LowWatermark {
			select {
			case m.resumed <- struct{}{}:
				m.publishResumed(count)
			default:
			}
		}
	}

	atomic.StoreInt32(&m.status, idle)

	// 如果还有消息，重新调度
	hasUser := false
	m.mu.Lock()
	hasUser = m.userCount > 0
	m.mu.Unlock()
	if hasUser || !m.systemMailbox.Empty() {
		m.schedule()
	}
}

// publishOverflowLocked 发布溢出事件（调用方持有 mu 锁）
func (m *backpressureMailbox) publishOverflowLocked() {
	if m.eventStream != nil {
		event := &MailboxOverflowEvent{
			PID:       m.ownerPID,
			QueueSize: m.userCount,
			Watermark: m.config.HighWatermark,
			Strategy:  m.config.Strategy,
		}
		// 异步发布避免死锁
		go m.eventStream.Publish(event)
	}
}

// publishResumed 发布恢复事件
func (m *backpressureMailbox) publishResumed(count int) {
	if m.eventStream != nil {
		event := &MailboxResumedEvent{
			PID:       m.ownerPID,
			QueueSize: count,
		}
		go m.eventStream.Publish(event)
	}
}

// SetScheduler 设置调度器
func (m *backpressureMailbox) SetScheduler(scheduler Dispatcher) {
	m.scheduler.Store(scheduler)
}

// SetEventStream 设置事件流（用于发布背压事件）
func (m *backpressureMailbox) SetEventStream(es *EventStream) {
	m.eventStream = es
}

// SetOwnerPID 设置所属 Actor PID
func (m *backpressureMailbox) SetOwnerPID(pid *PID) {
	m.ownerPID = pid
}

// DroppedCount 返回已丢弃的消息数量
func (m *backpressureMailbox) DroppedCount() int64 {
	return atomic.LoadInt64(&m.droppedCount)
}

// QueueSize 返回当前队列中的消息数量
func (m *backpressureMailbox) QueueSize() int {
	m.mu.Lock()
	count := m.userCount
	m.mu.Unlock()
	return count
}
