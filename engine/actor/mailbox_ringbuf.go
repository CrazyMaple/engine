package actor

import (
	"sync/atomic"

	"engine/internal"
)

// DefaultRingBufferCapacity 默认环形缓冲区容量
const DefaultRingBufferCapacity = 4096

// ringBufferMailbox 基于 MPSC 环形缓冲区的零分配邮箱
// 相比 defaultMailbox（使用 MPSC 链表队列），避免了每条消息的 node 分配
// 适用于高频消息场景，显著降低 GC 压力
type ringBufferMailbox struct {
	userMailbox   *mpscRingBuffer
	systemMailbox *internal.Queue // 系统消息仍使用链表队列（频率低，无界）
	userInvoker   func(interface{})
	systemInvoker func(interface{})
	status        int32
	scheduler     atomic.Value

	// 批处理投递缓冲（消费者独占，无需锁）
	batchBuf []interface{}

	// 溢出时的回退队列（避免生产者丢消息）
	overflowMailbox *internal.Queue
}

// NewRingBufferMailbox 创建环形缓冲区邮箱
// capacity 为 Ring Buffer 的槽位数（将向上取整为 2 的幂，最小 64）
func NewRingBufferMailbox(capacity int) Mailbox {
	if capacity <= 0 {
		capacity = DefaultRingBufferCapacity
	}
	rb := newMPSCRingBuffer(capacity)
	return &ringBufferMailbox{
		userMailbox:     rb,
		systemMailbox:   internal.NewQueue(),
		overflowMailbox: internal.NewQueue(),
		status:          idle,
		batchBuf:        make([]interface{}, 0, 64),
	}
}

func (m *ringBufferMailbox) PostUserMessage(message interface{}) {
	// 先尝试写入环形缓冲区（零分配路径）
	if !m.userMailbox.Push(message) {
		// 已满，回退到溢出队列
		m.overflowMailbox.Push(message)
	}
	m.schedule()
}

func (m *ringBufferMailbox) PostSystemMessage(message interface{}) {
	m.systemMailbox.Push(message)
	m.schedule()
}

func (m *ringBufferMailbox) RegisterHandlers(userInvoker, systemInvoker func(interface{})) {
	m.userInvoker = userInvoker
	m.systemInvoker = systemInvoker
}

func (m *ringBufferMailbox) Start() {}

func (m *ringBufferMailbox) schedule() {
	if atomic.CompareAndSwapInt32(&m.status, idle, scheduled) {
		scheduler := m.scheduler.Load()
		if scheduler != nil {
			scheduler.(Dispatcher).Schedule(m.run)
		}
	}
}

func (m *ringBufferMailbox) run() {
	atomic.StoreInt32(&m.status, running)

	// 优先处理系统消息
	for !m.systemMailbox.Empty() {
		msg := m.systemMailbox.Pop()
		if msg != nil && m.systemInvoker != nil {
			m.systemInvoker(msg)
		}
	}

	// 确定吞吐量
	throughput := 10
	if s := m.scheduler.Load(); s != nil {
		if t := s.(Dispatcher).Throughput(); t > 0 {
			throughput = t
		}
	}

	// 批量取出用户消息（零拷贝批处理）
	m.batchBuf = m.batchBuf[:0]
	m.batchBuf = m.userMailbox.PopBatch(m.batchBuf, throughput)

	// 若环形缓冲区不够，从溢出队列补齐并回填到 Ring Buffer
	for len(m.batchBuf) < throughput && !m.overflowMailbox.Empty() {
		msg := m.overflowMailbox.Pop()
		if msg == nil {
			break
		}
		m.batchBuf = append(m.batchBuf, msg)
	}

	// 批量投递给 invoker
	if m.userInvoker != nil {
		for i, msg := range m.batchBuf {
			if msg != nil {
				m.userInvoker(msg)
			}
			m.batchBuf[i] = nil // 帮助 GC
		}
	}

	// 尝试将溢出队列中的消息迁移回 Ring Buffer（避免永久使用溢出队列）
	for !m.overflowMailbox.Empty() {
		msg := m.overflowMailbox.Pop()
		if msg == nil {
			break
		}
		if !m.userMailbox.Push(msg) {
			// Ring Buffer 又满了，放回溢出队列
			m.overflowMailbox.Push(msg)
			break
		}
	}

	atomic.StoreInt32(&m.status, idle)

	// 如果还有消息，重新调度
	if !m.userMailbox.Empty() || !m.systemMailbox.Empty() || !m.overflowMailbox.Empty() {
		m.schedule()
	}
}

// SetScheduler 设置调度器
func (m *ringBufferMailbox) SetScheduler(scheduler Dispatcher) {
	m.scheduler.Store(scheduler)
}

// QueueSize 返回当前队列中的消息数量（近似值）
func (m *ringBufferMailbox) QueueSize() int {
	return m.userMailbox.Len()
}

// WithRingBufferMailbox 为 Props 设置环形缓冲区邮箱
// capacity 将向上取整为 2 的幂（最小 64），0 使用默认值
func (props *Props) WithRingBufferMailbox(capacity int) *Props {
	props.mailbox = func() Mailbox {
		return NewRingBufferMailbox(capacity)
	}
	return props
}
