package actor

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ActorPoolConfig Actor 池配置
type ActorPoolConfig struct {
	// MinSize 最小 Actor 数量（池启动后始终保持）
	MinSize int
	// MaxSize 最大 Actor 数量（弹性伸缩上限）
	MaxSize int
	// ScaleThreshold 扩容阈值，当待处理消息数 > 当前 Actor 数 * ScaleThreshold 时触发扩容
	ScaleThreshold int
	// IdleTimeout 空闲超时，超过此时间无消息的 Actor 会被回收（不低于 MinSize）
	IdleTimeout time.Duration
	// ScaleInterval 弹性伸缩检查间隔
	ScaleInterval time.Duration
}

// DefaultActorPoolConfig 默认池配置
func DefaultActorPoolConfig() ActorPoolConfig {
	return ActorPoolConfig{
		MinSize:        2,
		MaxSize:        10,
		ScaleThreshold: 5,
		IdleTimeout:    60 * time.Second,
		ScaleInterval:  5 * time.Second,
	}
}

// ActorPool 弹性伸缩 Actor 池
// 在 Pool 内维护一组同类 Actor，通过 RoundRobin 分发消息
// 支持根据负载自动扩缩容
type ActorPool struct {
	config   ActorPoolConfig
	props    *Props
	system   *ActorSystem
	routees  []*poolRoutee
	counter  uint64 // RoundRobin 计数器
	stopCh   chan struct{}
	stopped  bool
	selfPID  *PID
	mu       sync.RWMutex

	// 指标统计
	totalCreated   int64
	totalDestroyed int64
	totalRouted    int64
	scaleUpTotal   int64
	scaleDownTotal int64

	// MetricsRegistry 集成（可选）
	metrics     PoolMetrics
	metricsName string
}

// poolRoutee 池中的 Actor 条目
type poolRoutee struct {
	pid      *PID
	lastUsed time.Time
	msgCount int64 // 该 routee 处理的消息计数
}

// poolCounter 全局 Pool 计数器
var poolCounter uint64

// NewActorPool 创建弹性伸缩 Actor 池
func NewActorPool(system *ActorSystem, props *Props, config ActorPoolConfig) *ActorPool {
	if config.MinSize <= 0 {
		config.MinSize = 1
	}
	if config.MaxSize < config.MinSize {
		config.MaxSize = config.MinSize
	}
	if config.ScaleThreshold <= 0 {
		config.ScaleThreshold = 5
	}
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = 60 * time.Second
	}
	if config.ScaleInterval <= 0 {
		config.ScaleInterval = 5 * time.Second
	}

	pool := &ActorPool{
		config:  config,
		props:   props,
		system:  system,
		stopCh:  make(chan struct{}),
	}

	return pool
}

// Start 启动 Actor 池，创建初始 Actor 并开始弹性伸缩监控
func (p *ActorPool) Start() *PID {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 创建初始 Actor
	for i := 0; i < p.config.MinSize; i++ {
		p.spawnRoutee()
	}

	// 创建 Pool 的虚拟 PID 用于消息路由
	id := fmt.Sprintf("$pool/%d", atomic.AddUint64(&poolCounter, 1))
	p.selfPID = NewLocalPID(id)

	proc := &actorPoolProcess{pool: p}
	p.system.ProcessRegistry.Add(p.selfPID, proc)

	// 启动弹性伸缩协程
	go p.scaleLoop()

	return p.selfPID
}

// Stop 停止 Actor 池，销毁所有 Actor
func (p *ActorPool) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stopped {
		return
	}
	p.stopped = true
	close(p.stopCh)

	for _, r := range p.routees {
		p.system.Root.Stop(r.pid)
		atomic.AddInt64(&p.totalDestroyed, 1)
	}
	p.routees = nil

	if p.selfPID != nil {
		p.system.ProcessRegistry.Remove(p.selfPID)
	}
}

// RouteMessage 使用 RoundRobin 将消息路由到池中的 Actor
func (p *ActorPool) RouteMessage(message interface{}, sender *PID) {
	p.mu.RLock()
	n := len(p.routees)
	if n == 0 {
		p.mu.RUnlock()
		return
	}
	idx := atomic.AddUint64(&p.counter, 1) - 1
	routee := p.routees[idx%uint64(n)]
	p.mu.RUnlock()

	routee.lastUsed = time.Now()
	atomic.AddInt64(&routee.msgCount, 1)
	atomic.AddInt64(&p.totalRouted, 1)

	sendMessage(routee.pid, message, sender)
}

// Size 返回当前池中 Actor 数量
func (p *ActorPool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.routees)
}

// ActiveCount 返回活跃 Actor 数量（IdleTimeout 内有消息的）
func (p *ActorPool) ActiveCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	threshold := time.Now().Add(-p.config.IdleTimeout)
	count := 0
	for _, r := range p.routees {
		if r.lastUsed.After(threshold) {
			count++
		}
	}
	return count
}

// Stats 返回池的统计信息
func (p *ActorPool) Stats() ActorPoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return ActorPoolStats{
		CurrentSize:    len(p.routees),
		ActiveCount:    p.ActiveCount(),
		TotalCreated:   atomic.LoadInt64(&p.totalCreated),
		TotalDestroyed: atomic.LoadInt64(&p.totalDestroyed),
		TotalRouted:    atomic.LoadInt64(&p.totalRouted),
	}
}

// ActorPoolStats 池统计
type ActorPoolStats struct {
	CurrentSize    int
	ActiveCount    int
	TotalCreated   int64
	TotalDestroyed int64
	TotalRouted    int64
}

// spawnRoutee 创建一个新的 routee（调用方需持有写锁）
func (p *ActorPool) spawnRoutee() {
	pid := p.system.Root.Spawn(p.props)
	p.routees = append(p.routees, &poolRoutee{
		pid:      pid,
		lastUsed: time.Now(),
	})
	atomic.AddInt64(&p.totalCreated, 1)
}

// scaleLoop 弹性伸缩循环
func (p *ActorPool) scaleLoop() {
	ticker := time.NewTicker(p.config.ScaleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.checkScale()
		}
	}
}

// checkScale 检查并执行弹性伸缩
func (p *ActorPool) checkScale() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stopped {
		return
	}

	currentSize := len(p.routees)

	// 扩容检查：根据最近消息吞吐量判断
	totalRecent := int64(0)
	now := time.Now()
	for _, r := range p.routees {
		if now.Sub(r.lastUsed) < p.config.ScaleInterval*2 {
			totalRecent += atomic.LoadInt64(&r.msgCount)
		}
	}

	// 如果平均每个 Actor 负载超过阈值，且未达到上限，则扩容
	if currentSize > 0 && currentSize < p.config.MaxSize {
		avgLoad := totalRecent / int64(currentSize)
		if avgLoad > int64(p.config.ScaleThreshold) {
			// 扩容一个
			p.spawnRoutee()
			p.incScaleUp()
			return
		}
	}

	// 缩容检查：空闲超时的 Actor 回收
	if currentSize > p.config.MinSize {
		threshold := now.Add(-p.config.IdleTimeout)
		// 从后往前检查，回收空闲 routee
		for i := len(p.routees) - 1; i >= 0; i-- {
			if len(p.routees) <= p.config.MinSize {
				break
			}
			r := p.routees[i]
			if r.lastUsed.Before(threshold) {
				p.system.Root.Stop(r.pid)
				p.routees = append(p.routees[:i], p.routees[i+1:]...)
				atomic.AddInt64(&p.totalDestroyed, 1)
				p.incScaleDown()
			}
		}
	}
}

// actorPoolProcess 实现 Process 接口，将消息路由到池中的 Actor
type actorPoolProcess struct {
	pool *ActorPool
}

func (p *actorPoolProcess) SendUserMessage(pid *PID, message interface{}) {
	msg, sender := UnwrapEnvelope(message)
	p.pool.RouteMessage(msg, sender)
}

func (p *actorPoolProcess) SendSystemMessage(pid *PID, message interface{}) {
	// 系统消息广播到所有 routee
	p.pool.mu.RLock()
	routees := make([]*PID, len(p.pool.routees))
	for i, r := range p.pool.routees {
		routees[i] = r.pid
	}
	p.pool.mu.RUnlock()

	for _, rpid := range routees {
		sendSystemMessage(rpid, message)
	}
}

func (p *actorPoolProcess) Stop(pid *PID) {
	p.pool.Stop()
}
