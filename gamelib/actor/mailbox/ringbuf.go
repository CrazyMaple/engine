package mailbox

import (
	"sync/atomic"

	"engine/actor"
)

// DefaultRingBufferCapacity 默认环形缓冲区容量
const DefaultRingBufferCapacity = 4096

// ringBuffer 基于 MPSC 环形缓冲区的零分配邮箱
// 相比 defaultMailbox（使用 MPSC 链表队列），避免了每条消息的 node 分配；
// 适用于高频消息场景，显著降低 GC 压力。
type ringBuffer struct {
	userMailbox   *mpscRingBuffer
	systemMailbox *mpscQueue // 系统消息频率低，仍使用链表队列
	userInvoker   func(interface{})
	systemInvoker func(interface{})
	status        int32
	scheduler     atomic.Value

	batchBuf []interface{}

	overflowMailbox *mpscQueue
}

// NewRingBuffer 创建环形缓冲区邮箱。
// capacity 将向上取整为 2 的幂（最小 64），0 使用默认值。
func NewRingBuffer(capacity int) actor.Mailbox {
	if capacity <= 0 {
		capacity = DefaultRingBufferCapacity
	}
	rb := newMPSCRingBuffer(capacity)
	return &ringBuffer{
		userMailbox:     rb,
		systemMailbox:   newMpscQueue(),
		overflowMailbox: newMpscQueue(),
		status:          idle,
		batchBuf:        make([]interface{}, 0, 64),
	}
}

func (m *ringBuffer) PostUserMessage(message interface{}) {
	if !m.userMailbox.Push(message) {
		m.overflowMailbox.Push(message)
	}
	m.schedule()
}

func (m *ringBuffer) PostSystemMessage(message interface{}) {
	m.systemMailbox.Push(message)
	m.schedule()
}

func (m *ringBuffer) RegisterHandlers(userInvoker, systemInvoker func(interface{})) {
	m.userInvoker = userInvoker
	m.systemInvoker = systemInvoker
}

func (m *ringBuffer) Start() {}

func (m *ringBuffer) schedule() {
	if atomic.CompareAndSwapInt32(&m.status, idle, scheduled) {
		scheduler := m.scheduler.Load()
		if scheduler != nil {
			scheduler.(actor.Dispatcher).Schedule(m.run)
		}
	}
}

func (m *ringBuffer) run() {
	atomic.StoreInt32(&m.status, running)

	for !m.systemMailbox.Empty() {
		msg := m.systemMailbox.Pop()
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

	m.batchBuf = m.batchBuf[:0]
	m.batchBuf = m.userMailbox.PopBatch(m.batchBuf, throughput)

	for len(m.batchBuf) < throughput && !m.overflowMailbox.Empty() {
		msg := m.overflowMailbox.Pop()
		if msg == nil {
			break
		}
		m.batchBuf = append(m.batchBuf, msg)
	}

	if m.userInvoker != nil {
		for i, msg := range m.batchBuf {
			if msg != nil {
				m.userInvoker(msg)
			}
			m.batchBuf[i] = nil
		}
	}

	for !m.overflowMailbox.Empty() {
		msg := m.overflowMailbox.Pop()
		if msg == nil {
			break
		}
		if !m.userMailbox.Push(msg) {
			m.overflowMailbox.Push(msg)
			break
		}
	}

	atomic.StoreInt32(&m.status, idle)

	if !m.userMailbox.Empty() || !m.systemMailbox.Empty() || !m.overflowMailbox.Empty() {
		m.schedule()
	}
}

// SetScheduler 实现 actor.SchedulerAware。
func (m *ringBuffer) SetScheduler(scheduler actor.Dispatcher) {
	m.scheduler.Store(scheduler)
}

// QueueSize 返回当前队列中的消息数量（近似值）。
func (m *ringBuffer) QueueSize() int {
	return m.userMailbox.Len()
}
