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
