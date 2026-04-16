package stress

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// --- BotPool 批量管理虚拟玩家 ---

// BotPool 虚拟玩家池，管理批量 Bot 的创建、启动、停止和指标聚合
type BotPool struct {
	mu       sync.RWMutex
	bots     []*ManagedBot
	builder  *BotBuilder
	metrics  *Metrics
	running  int32
	cancel   context.CancelFunc
	wg       sync.WaitGroup

	// 统计
	totalCreated  int64
	totalFailed   int64
	totalFinished int64
}

// NewBotPool 创建 Bot 池
func NewBotPool(builder *BotBuilder) *BotPool {
	m := NewMetrics()
	builder.metrics = m
	return &BotPool{
		builder: builder,
		metrics: m,
	}
}

// Spawn 创建 N 个 Bot（不启动）
func (p *BotPool) Spawn(count int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := 0; i < count; i++ {
		id := len(p.bots)
		bot := p.builder.Build(id)
		p.bots = append(p.bots, bot)
		atomic.AddInt64(&p.totalCreated, 1)
	}
}

// StartAll 启动所有 Bot
// rampUp 指定预热时间，Bot 将在此时间内逐步启动
func (p *BotPool) StartAll(ctx context.Context, rampUp time.Duration) {
	p.mu.RLock()
	bots := make([]*ManagedBot, len(p.bots))
	copy(bots, p.bots)
	p.mu.RUnlock()

	if len(bots) == 0 {
		return
	}

	atomic.StoreInt32(&p.running, 1)
	ctx, cancel := context.WithCancel(ctx)
	p.cancel = cancel

	p.metrics.startTime = time.Now()

	var rampDelay time.Duration
	if rampUp > 0 && len(bots) > 1 {
		rampDelay = rampUp / time.Duration(len(bots))
	}

	for i, bot := range bots {
		if rampDelay > 0 && i > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(rampDelay):
			}
		}

		p.wg.Add(1)
		go func(b *ManagedBot) {
			defer p.wg.Done()
			err := b.Run(ctx)
			if err != nil {
				atomic.AddInt64(&p.totalFailed, 1)
			}
			atomic.AddInt64(&p.totalFinished, 1)
		}(bot)
	}
}

// StopAll 停止所有 Bot
func (p *BotPool) StopAll() {
	if !atomic.CompareAndSwapInt32(&p.running, 1, 0) {
		return
	}
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
	p.metrics.endTime = time.Now()
}

// Wait 等待所有 Bot 完成
func (p *BotPool) Wait() {
	p.wg.Wait()
	p.metrics.endTime = time.Now()
}

// Stats 获取池状态快照
func (p *BotPool) Stats() BotPoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := BotPoolStats{
		TotalCreated:  atomic.LoadInt64(&p.totalCreated),
		TotalFailed:   atomic.LoadInt64(&p.totalFailed),
		TotalFinished: atomic.LoadInt64(&p.totalFinished),
		Running:       atomic.LoadInt32(&p.running) == 1,
	}

	for _, bot := range p.bots {
		state := bot.GetState()
		switch state {
		case BotStateIdle:
			stats.Idle++
		case BotStateLogin:
			stats.LoggingIn++
		case BotStatePlaying:
			stats.Playing++
		case BotStateLogout:
			stats.LoggingOut++
		case BotStateStopped:
			stats.Stopped++
		case BotStateError:
			stats.Errored++
		}
	}
	return stats
}

// BotPoolStats 池统计快照
type BotPoolStats struct {
	TotalCreated  int64 `json:"total_created"`
	TotalFailed   int64 `json:"total_failed"`
	TotalFinished int64 `json:"total_finished"`
	Running       bool  `json:"running"`

	Idle       int `json:"idle"`
	LoggingIn  int `json:"logging_in"`
	Playing    int `json:"playing"`
	LoggingOut int `json:"logging_out"`
	Stopped    int `json:"stopped"`
	Errored    int `json:"errored"`
}

// String 格式化输出统计信息
func (s BotPoolStats) String() string {
	return fmt.Sprintf(
		"Pool[created=%d running=%v idle=%d login=%d play=%d logout=%d stop=%d err=%d]",
		s.TotalCreated, s.Running, s.Idle, s.LoggingIn, s.Playing, s.LoggingOut, s.Stopped, s.Errored,
	)
}

// Report 生成压测报告
func (p *BotPool) Report(scenarioName string) *Report {
	cfg := &ScenarioConfig{
		Name:        scenarioName,
		Concurrency: int(atomic.LoadInt64(&p.totalCreated)),
	}
	return p.metrics.Snapshot(cfg)
}

// Bots 返回所有 Bot 的快照
func (p *BotPool) Bots() []*ManagedBot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]*ManagedBot, len(p.bots))
	copy(result, p.bots)
	return result
}

// BotCount 返回 Bot 总数
func (p *BotPool) BotCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.bots)
}
