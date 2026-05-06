package mailbox

import (
	"sync"
	"sync/atomic"
	"time"

	"engine/actor"
)

// AdaptiveConfig 自适应邮箱配置
type AdaptiveConfig struct {
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

// DefaultAdaptiveConfig 返回默认自适应配置。
func DefaultAdaptiveConfig() AdaptiveConfig {
	return AdaptiveConfig{
		MinThroughput:             4,
		MaxThroughput:             256,
		LowDepthThreshold:         10,
		HighDepthThreshold:        500,
		CooperativeYieldThreshold: 5 * time.Millisecond,
	}
}

// adaptive 自适应吞吐量邮箱：根据队列深度动态调整每次处理的消息数，兼顾延迟和吞吐。
type adaptive struct {
	userMailbox   *mpscQueue
	systemMailbox *mpscQueue
	userInvoker   func(interface{})
	systemInvoker func(interface{})
	status        int32
	scheduler     atomic.Value
	config        AdaptiveConfig

	depth int64

	stats schedulingStatsState
}

type schedulingStatsState struct {
	mu               sync.Mutex
	scheduleCount    int64
	processedCount   int64
	totalLatency     time.Duration
	maxQueueDepth    int64
	yieldCount       int64
	lastScheduleTime time.Time
}

// NewAdaptive 创建自适应邮箱。
func NewAdaptive(config AdaptiveConfig) actor.Mailbox {
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
	return &adaptive{
		userMailbox:   newMpscQueue(),
		systemMailbox: newMpscQueue(),
		status:        idle,
		config:        config,
	}
}

func (m *adaptive) PostUserMessage(message interface{}) {
	m.userMailbox.Push(message)
	depth := atomic.AddInt64(&m.depth, 1)

	m.stats.mu.Lock()
	if depth > m.stats.maxQueueDepth {
		m.stats.maxQueueDepth = depth
	}
	m.stats.mu.Unlock()

	m.schedule()
}

func (m *adaptive) PostSystemMessage(message interface{}) {
	m.systemMailbox.Push(message)
	m.schedule()
}

func (m *adaptive) RegisterHandlers(userInvoker, systemInvoker func(interface{})) {
	m.userInvoker = userInvoker
	m.systemInvoker = systemInvoker
}

func (m *adaptive) Start() {}

func (m *adaptive) schedule() {
	if atomic.CompareAndSwapInt32(&m.status, idle, scheduled) {
		scheduler := m.scheduler.Load()
		if scheduler != nil {
			scheduler.(actor.Dispatcher).Schedule(m.run)
		}
	}
}

// adaptiveThroughput 根据当前队列深度计算目标吞吐量。
func (m *adaptive) adaptiveThroughput() int {
	depth := int(atomic.LoadInt64(&m.depth))

	if depth <= m.config.LowDepthThreshold {
		return m.config.MinThroughput
	}
	if depth >= m.config.HighDepthThreshold {
		return m.config.MaxThroughput
	}
	span := m.config.HighDepthThreshold - m.config.LowDepthThreshold
	progress := depth - m.config.LowDepthThreshold
	delta := m.config.MaxThroughput - m.config.MinThroughput
	return m.config.MinThroughput + (progress*delta)/span
}

func (m *adaptive) run() {
	atomic.StoreInt32(&m.status, running)
	startTime := time.Now()

	atomic.AddInt64(&m.stats.scheduleCount, 1)

	for !m.systemMailbox.Empty() {
		msg := m.systemMailbox.Pop()
		if msg != nil && m.systemInvoker != nil {
			m.systemInvoker(msg)
		}
	}

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

	if !m.userMailbox.Empty() || !m.systemMailbox.Empty() {
		m.schedule()
	}
}

// SetScheduler 实现 actor.SchedulerAware。
func (m *adaptive) SetScheduler(scheduler actor.Dispatcher) {
	m.scheduler.Store(scheduler)
}

// QueueSize 返回当前队列深度。
func (m *adaptive) QueueSize() int {
	return int(atomic.LoadInt64(&m.depth))
}

// Stats 返回调度统计指标快照。实现 SchedulingMetricsProvider。
func (m *adaptive) Stats() SchedulingStats {
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
