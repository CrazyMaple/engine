package mailbox

import (
	"sync"
	"sync/atomic"
	"time"

	"engine/actor"
)

// BatchConfig 批处理邮箱配置
type BatchConfig struct {
	BatchSize    int           // 每批最大消息数（默认 100）
	BatchTimeout time.Duration // 最大等待时间，超时强制刷新（默认 10ms）
}

func (c *BatchConfig) defaults() {
	if c.BatchSize <= 0 {
		c.BatchSize = 100
	}
	if c.BatchTimeout <= 0 {
		c.BatchTimeout = 10 * time.Millisecond
	}
}

// batch 批处理邮箱
// 累积用户消息到 BatchSize 条或等待 BatchTimeout 后批量投递；
// 系统消息不参与批处理，立即投递。
type batch struct {
	config        BatchConfig
	userBuf       []interface{}
	systemMailbox *mpscQueue
	userInvoker   func(interface{})
	systemInvoker func(interface{})
	batchInvoker  func([]interface{})
	status        int32
	scheduler     atomic.Value
	mu            sync.Mutex
	flushTimer    *time.Timer
	timerRunning  bool
}

// NewBatch 创建批处理邮箱
func NewBatch(config BatchConfig) actor.Mailbox {
	config.defaults()
	return &batch{
		config:        config,
		userBuf:       make([]interface{}, 0, config.BatchSize),
		systemMailbox: newMpscQueue(),
		status:        idle,
	}
}

func (m *batch) PostUserMessage(message interface{}) {
	m.mu.Lock()
	m.userBuf = append(m.userBuf, message)
	shouldFlush := len(m.userBuf) >= m.config.BatchSize
	if !shouldFlush && !m.timerRunning {
		m.timerRunning = true
		m.flushTimer = time.AfterFunc(m.config.BatchTimeout, func() {
			m.mu.Lock()
			m.timerRunning = false
			needSchedule := len(m.userBuf) > 0
			m.mu.Unlock()
			if needSchedule {
				m.schedule()
			}
		})
	}
	m.mu.Unlock()

	if shouldFlush {
		m.schedule()
	}
}

func (m *batch) PostSystemMessage(message interface{}) {
	m.systemMailbox.Push(message)
	m.schedule()
}

func (m *batch) RegisterHandlers(userInvoker, systemInvoker func(interface{})) {
	m.userInvoker = userInvoker
	m.systemInvoker = systemInvoker
}

// RegisterBatchHandler 实现 actor.BatchAwareMailbox。
func (m *batch) RegisterBatchHandler(batchInvoker func([]interface{})) {
	m.batchInvoker = batchInvoker
}

func (m *batch) Start() {}

// SetScheduler 实现 actor.SchedulerAware。
func (m *batch) SetScheduler(scheduler actor.Dispatcher) {
	m.scheduler.Store(scheduler)
}

func (m *batch) schedule() {
	if atomic.CompareAndSwapInt32(&m.status, idle, scheduled) {
		scheduler := m.scheduler.Load()
		if scheduler != nil {
			scheduler.(actor.Dispatcher).Schedule(m.run)
		}
	}
}

func (m *batch) run() {
	atomic.StoreInt32(&m.status, running)

	for !m.systemMailbox.Empty() {
		msg := m.systemMailbox.Pop()
		if msg != nil && m.systemInvoker != nil {
			m.systemInvoker(msg)
		}
	}

	m.flush()

	atomic.StoreInt32(&m.status, idle)

	m.mu.Lock()
	hasMore := len(m.userBuf) > 0
	m.mu.Unlock()
	if hasMore || !m.systemMailbox.Empty() {
		m.schedule()
	}
}

func (m *batch) flush() {
	m.mu.Lock()
	if len(m.userBuf) == 0 {
		m.mu.Unlock()
		return
	}
	bb := m.userBuf
	m.userBuf = make([]interface{}, 0, m.config.BatchSize)
	if m.flushTimer != nil {
		m.flushTimer.Stop()
		m.timerRunning = false
	}
	m.mu.Unlock()

	if m.batchInvoker != nil {
		m.batchInvoker(bb)
	} else if m.userInvoker != nil {
		for _, msg := range bb {
			m.userInvoker(msg)
		}
	}
}
