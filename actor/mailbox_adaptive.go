package actor

import (
	"sync"
	"sync/atomic"
	"time"

	"engine/internal"
)

// AdaptiveMailboxConfig 自适应邮箱配置
type AdaptiveMailboxConfig struct {
	// MinThroughput 每次调度至少处理的消息数（低负载延迟优先）
	MinThroughput int
	// MaxThroughput 每次调度最多处理的消息数（高负载吞吐优先）
	MaxThroughput int
	// LowDepthThreshold 低深度阈值（队列小于此值时使用 MinThroughput）
	LowDepthThreshold int
	// HighDepthThreshold 高深度阈值（队列大于此值时使用 MaxThroughput）
	HighDepthThreshold int
	// CooperativeYieldThreshold 协作式调度：处理时间超过阈值后让出 goroutine
	CooperativeYieldThreshold time.Duration
}

// DefaultAdaptiveConfig 默认自适应配置
func DefaultAdaptiveConfig() AdaptiveMailboxConfig {
	return AdaptiveMailboxConfig{
		MinThroughput:             4,
		MaxThroughput:             256,
		LowDepthThreshold:         10,
		HighDepthThreshold:        500,
		CooperativeYieldThreshold: 5 * time.Millisecond,
	}
}

// adaptiveMailbox 自适应吞吐量邮箱
// 根据队列深度动态调整每次处理的消息数，兼顾延迟和吞吐
type adaptiveMailbox struct {
	userMailbox   *internal.Queue
	systemMailbox *internal.Queue
	userInvoker   func(interface{})
	systemInvoker func(interface{})
	status        int32
	scheduler     atomic.Value
	config        AdaptiveMailboxConfig

	// 队列深度计数（原子操作）
	depth int64

	// 调度指标
	stats schedulingStats
}

// schedulingStats 调度统计指标
type schedulingStats struct {
	mu               sync.Mutex
	scheduleCount    int64         // 调度次数
	processedCount   int64         // 已处理消息数
	totalLatency     time.Duration // 累计处理延迟
	maxQueueDepth    int64         // 历史最大队列深度
	yieldCount       int64         // 协作式让出次数
	lastScheduleTime time.Time
}

// NewAdaptiveMailbox 创建自适应邮箱
func NewAdaptiveMailbox(config AdaptiveMailboxConfig) Mailbox {
	if config.MinThroughput <= 0 {
		config.MinThroughput = 4
	}
	if config.MaxThroughput <= 0 {
		config.MaxThroughput = 256
	}
	if config.LowDepthThreshold <= 0 {
		config.LowDepthThreshold = 10
	}
	if config.HighDepthThreshold <= 0 {
		config.HighDepthThreshold = 500
	}
	if config.CooperativeYieldThreshold <= 0 {
		config.CooperativeYieldThreshold = 5 * time.Millisecond
	}
	return &adaptiveMailbox{
		userMailbox:   internal.NewQueue(),
		systemMailbox: internal.NewQueue(),
		status:        idle,
		config:        config,
	}
}

func (m *adaptiveMailbox) PostUserMessage(message interface{}) {
	m.userMailbox.Push(message)
	depth := atomic.AddInt64(&m.depth, 1)

	// 更新最大队列深度
	m.stats.mu.Lock()
	if depth > m.stats.maxQueueDepth {
		m.stats.maxQueueDepth = depth
	}
	m.stats.mu.Unlock()

	m.schedule()
}

func (m *adaptiveMailbox) PostSystemMessage(message interface{}) {
	m.systemMailbox.Push(message)
	m.schedule()
}

func (m *adaptiveMailbox) RegisterHandlers(userInvoker, systemInvoker func(interface{})) {
	m.userInvoker = userInvoker
	m.systemInvoker = systemInvoker
}

func (m *adaptiveMailbox) Start() {}

func (m *adaptiveMailbox) schedule() {
	if atomic.CompareAndSwapInt32(&m.status, idle, scheduled) {
		scheduler := m.scheduler.Load()
		if scheduler != nil {
			scheduler.(Dispatcher).Schedule(m.run)
		}
	}
}

// adaptiveThroughput 根据当前队列深度计算目标吞吐量
// 低深度 → MinThroughput；高深度 → MaxThroughput；之间线性插值
func (m *adaptiveMailbox) adaptiveThroughput() int {
	depth := int(atomic.LoadInt64(&m.depth))

	if depth <= m.config.LowDepthThreshold {
		return m.config.MinThroughput
	}
	if depth >= m.config.HighDepthThreshold {
		return m.config.MaxThroughput
	}
	// 线性插值
	span := m.config.HighDepthThreshold - m.config.LowDepthThreshold
	progress := depth - m.config.LowDepthThreshold
	delta := m.config.MaxThroughput - m.config.MinThroughput
	return m.config.MinThroughput + (progress*delta)/span
}

func (m *adaptiveMailbox) run() {
	atomic.StoreInt32(&m.status, running)
	startTime := time.Now()

	atomic.AddInt64(&m.stats.scheduleCount, 1)

	// 优先处理系统消息
	for !m.systemMailbox.Empty() {
		msg := m.systemMailbox.Pop()
		if msg != nil && m.systemInvoker != nil {
			m.systemInvoker(msg)
		}
	}

	// 自适应吞吐量
	throughput := m.adaptiveThroughput()
	processed := 0

	for i := 0; i < throughput; i++ {
		if m.userMailbox.Empty() {
			break
		}
		msg := m.userMailbox.Pop()
		if msg != nil && m.userInvoker != nil {
			m.userInvoker(msg)
			atomic.AddInt64(&m.depth, -1)
			processed++
		}

		// 协作式调度：处理时间过长时主动让出
		if m.config.CooperativeYieldThreshold > 0 {
			if time.Since(startTime) >= m.config.CooperativeYieldThreshold {
				atomic.AddInt64(&m.stats.yieldCount, 1)
				break
			}
		}
	}

	elapsed := time.Since(startTime)
	atomic.AddInt64(&m.stats.processedCount, int64(processed))
	m.stats.mu.Lock()
	m.stats.totalLatency += elapsed
	m.stats.lastScheduleTime = time.Now()
	m.stats.mu.Unlock()

	atomic.StoreInt32(&m.status, idle)

	// 如果还有消息，重新调度
	if !m.userMailbox.Empty() || !m.systemMailbox.Empty() {
		m.schedule()
	}
}

// SetScheduler 设置调度器
func (m *adaptiveMailbox) SetScheduler(scheduler Dispatcher) {
	m.scheduler.Store(scheduler)
}

// QueueSize 返回当前队列深度
func (m *adaptiveMailbox) QueueSize() int {
	return int(atomic.LoadInt64(&m.depth))
}

// Stats 返回调度统计指标快照
func (m *adaptiveMailbox) Stats() SchedulingStats {
	m.stats.mu.Lock()
	defer m.stats.mu.Unlock()

	scheduleCount := atomic.LoadInt64(&m.stats.scheduleCount)
	processedCount := atomic.LoadInt64(&m.stats.processedCount)
	yieldCount := atomic.LoadInt64(&m.stats.yieldCount)

	var avgLatency time.Duration
	if scheduleCount > 0 {
		avgLatency = m.stats.totalLatency / time.Duration(scheduleCount)
	}

	return SchedulingStats{
		ScheduleCount:  scheduleCount,
		ProcessedCount: processedCount,
		AvgLatency:     avgLatency,
		MaxQueueDepth:  m.stats.maxQueueDepth,
		YieldCount:     yieldCount,
		CurrentDepth:   atomic.LoadInt64(&m.depth),
	}
}

// WithAdaptiveMailbox 为 Props 设置自适应邮箱
func (props *Props) WithAdaptiveMailbox(config AdaptiveMailboxConfig) *Props {
	props.mailbox = func() Mailbox {
		return NewAdaptiveMailbox(config)
	}
	return props
}
