package actor

import (
	"sync"
	"sync/atomic"

	"engine/internal"
)

// boundedMailbox 有界邮箱，使用环形缓冲区
// 当邮箱满时丢弃最旧的消息（背压策略）
type boundedMailbox struct {
	userMessages  []interface{}
	userHead      int
	userTail      int
	userCount     int
	capacity      int

	systemMailbox *internal.Queue
	userInvoker   func(interface{})
	systemInvoker func(interface{})
	status        int32
	scheduler     atomic.Value
	mu            sync.Mutex
}

// NewBoundedMailbox 创建有界邮箱
func NewBoundedMailbox(capacity int) Mailbox {
	if capacity <= 0 {
		capacity = 1024
	}
	return &boundedMailbox{
		userMessages:  make([]interface{}, capacity),
		capacity:      capacity,
		systemMailbox: internal.NewQueue(),
		status:        idle,
	}
}

func (m *boundedMailbox) PostUserMessage(message interface{}) {
	m.mu.Lock()
	if m.userCount >= m.capacity {
		// 丢弃最旧的消息
		m.userTail = (m.userTail + 1) % m.capacity
		m.userCount--
	}
	m.userMessages[m.userHead] = message
	m.userHead = (m.userHead + 1) % m.capacity
	m.userCount++
	m.mu.Unlock()

	m.schedule()
}

func (m *boundedMailbox) PostSystemMessage(message interface{}) {
	m.systemMailbox.Push(message)
	m.schedule()
}

func (m *boundedMailbox) RegisterHandlers(userInvoker, systemInvoker func(interface{})) {
	m.userInvoker = userInvoker
	m.systemInvoker = systemInvoker
}

func (m *boundedMailbox) Start() {}

func (m *boundedMailbox) schedule() {
	if atomic.CompareAndSwapInt32(&m.status, idle, scheduled) {
		scheduler := m.scheduler.Load()
		if scheduler != nil {
			scheduler.(Dispatcher).Schedule(m.run)
		}
	}
}

func (m *boundedMailbox) run() {
	atomic.StoreInt32(&m.status, running)

	// 优先处理系统消息
	for !m.systemMailbox.Empty() {
		msg := m.systemMailbox.Pop()
		if msg != nil && m.systemInvoker != nil {
			m.systemInvoker(msg)
		}
	}

	// 处理用户消息（批量）
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
		m.userMessages[m.userTail] = nil // 避免内存泄漏
		m.userTail = (m.userTail + 1) % m.capacity
		m.userCount--
		m.mu.Unlock()

		if msg != nil && m.userInvoker != nil {
			m.userInvoker(msg)
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

// SetScheduler 设置调度器
func (m *boundedMailbox) SetScheduler(scheduler Dispatcher) {
	m.scheduler.Store(scheduler)
}
