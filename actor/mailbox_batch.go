package actor

import (
	"engine/internal"
	"sync"
	"sync/atomic"
	"time"
)

// BatchActor 批处理 Actor 接口
// 实现此接口的 Actor 将接收批量消息投递，适用于 DB 写入合并、日志聚合、指标汇总等场景
type BatchActor interface {
	Actor
	// BatchReceive 批量消息回调，messages 为累积的用户消息切片
	BatchReceive(ctx Context, messages []interface{})
}

// BatchMailboxConfig 批处理邮箱配置
type BatchMailboxConfig struct {
	BatchSize    int           // 每批最大消息数（默认 100）
	BatchTimeout time.Duration // 最大等待时间，超时强制刷新（默认 10ms）
}

func (c *BatchMailboxConfig) defaults() {
	if c.BatchSize <= 0 {
		c.BatchSize = 100
	}
	if c.BatchTimeout <= 0 {
		c.BatchTimeout = 10 * time.Millisecond
	}
}

// batchMailbox 批处理邮箱
// 累积用户消息到 BatchSize 条或等待 BatchTimeout 后批量投递
// 系统消息不参与批处理，立即投递
type batchMailbox struct {
	config        BatchMailboxConfig
	userBuf       []interface{}
	systemMailbox *internal.Queue
	userInvoker   func(interface{})
	systemInvoker func(interface{})
	batchInvoker  func([]interface{})
	status        int32
	scheduler     atomic.Value
	mu            sync.Mutex
	flushTimer    *time.Timer
	timerRunning  bool
}

// NewBatchMailbox 创建批处理邮箱
func NewBatchMailbox(config BatchMailboxConfig) Mailbox {
	config.defaults()
	return &batchMailbox{
		config:        config,
		userBuf:       make([]interface{}, 0, config.BatchSize),
		systemMailbox: internal.NewQueue(),
		status:        idle,
	}
}

func (m *batchMailbox) PostUserMessage(message interface{}) {
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

func (m *batchMailbox) PostSystemMessage(message interface{}) {
	m.systemMailbox.Push(message)
	m.schedule()
}

func (m *batchMailbox) RegisterHandlers(userInvoker, systemInvoker func(interface{})) {
	m.userInvoker = userInvoker
	m.systemInvoker = systemInvoker
}

// RegisterBatchHandler 注册批量消息回调
func (m *batchMailbox) RegisterBatchHandler(batchInvoker func([]interface{})) {
	m.batchInvoker = batchInvoker
}

func (m *batchMailbox) Start() {}

// SetScheduler 设置调度器
func (m *batchMailbox) SetScheduler(scheduler Dispatcher) {
	m.scheduler.Store(scheduler)
}

func (m *batchMailbox) schedule() {
	if atomic.CompareAndSwapInt32(&m.status, idle, scheduled) {
		scheduler := m.scheduler.Load()
		if scheduler != nil {
			scheduler.(Dispatcher).Schedule(m.run)
		}
	}
}

func (m *batchMailbox) run() {
	atomic.StoreInt32(&m.status, running)

	// 优先处理系统消息
	for !m.systemMailbox.Empty() {
		msg := m.systemMailbox.Pop()
		if msg != nil && m.systemInvoker != nil {
			m.systemInvoker(msg)
		}
	}

	// 刷新批处理缓冲
	m.flush()

	atomic.StoreInt32(&m.status, idle)

	// 检查是否有剩余消息
	m.mu.Lock()
	hasMore := len(m.userBuf) > 0
	m.mu.Unlock()
	if hasMore || !m.systemMailbox.Empty() {
		m.schedule()
	}
}

func (m *batchMailbox) flush() {
	m.mu.Lock()
	if len(m.userBuf) == 0 {
		m.mu.Unlock()
		return
	}
	// 取出当前批次，替换为新切片
	batch := m.userBuf
	m.userBuf = make([]interface{}, 0, m.config.BatchSize)
	// 停止定时器
	if m.flushTimer != nil {
		m.flushTimer.Stop()
		m.timerRunning = false
	}
	m.mu.Unlock()

	// 投递批量消息
	if m.batchInvoker != nil {
		m.batchInvoker(batch)
	} else if m.userInvoker != nil {
		// 降级：逐条投递
		for _, msg := range batch {
			m.userInvoker(msg)
		}
	}
}
