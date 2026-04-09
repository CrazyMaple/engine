package actor

import (
	"fmt"
	"sync/atomic"
)

// PoolMetrics 池指标接口，由外部指标系统实现（避免循环依赖）
type PoolMetrics interface {
	// IncCounter 递增计数器
	IncCounter(name, help string, labels map[string]string, delta int64)
	// RegisterGauge 注册 Gauge 指标
	RegisterGauge(name, help string, fn func() []PoolGaugeValue)
}

// PoolGaugeValue Gauge 指标值
type PoolGaugeValue struct {
	Labels map[string]string
	Value  float64
}

// WithMetrics 为 ActorPool 注入指标采集器，自动注册并更新池指标
// 注册的指标：
//   - engine_pool_active_count    (gauge)   当前活跃 Actor 数量
//   - engine_pool_idle_count      (gauge)   当前空闲 Actor 数量
//   - engine_pool_scale_up_total  (counter) 累计扩容次数
//   - engine_pool_scale_down_total(counter) 累计缩容次数
func (p *ActorPool) WithMetrics(metrics PoolMetrics, poolName string) *ActorPool {
	p.metrics = metrics
	p.metricsName = poolName
	p.registerMetrics()
	return p
}

// registerMetrics 注册 Gauge 指标
func (p *ActorPool) registerMetrics() {
	labels := map[string]string{"pool": p.metricsName}

	p.metrics.RegisterGauge(
		"engine_pool_active_count",
		"Number of active actors in the pool",
		func() []PoolGaugeValue {
			return []PoolGaugeValue{{Labels: labels, Value: float64(p.ActiveCount())}}
		},
	)

	p.metrics.RegisterGauge(
		"engine_pool_idle_count",
		"Number of idle actors in the pool",
		func() []PoolGaugeValue {
			p.mu.RLock()
			total := len(p.routees)
			p.mu.RUnlock()
			active := p.ActiveCount()
			idle := total - active
			if idle < 0 {
				idle = 0
			}
			return []PoolGaugeValue{{Labels: labels, Value: float64(idle)}}
		},
	)
}

// incScaleUp 记录一次扩容事件
func (p *ActorPool) incScaleUp() {
	atomic.AddInt64(&p.scaleUpTotal, 1)
	if p.metrics != nil {
		p.metrics.IncCounter(
			"engine_pool_scale_up_total",
			"Total number of pool scale-up events",
			map[string]string{"pool": p.metricsName},
			1,
		)
	}
}

// incScaleDown 记录一次缩容事件
func (p *ActorPool) incScaleDown() {
	atomic.AddInt64(&p.scaleDownTotal, 1)
	if p.metrics != nil {
		p.metrics.IncCounter(
			"engine_pool_scale_down_total",
			"Total number of pool scale-down events",
			map[string]string{"pool": p.metricsName},
			1,
		)
	}
}

// ScaleUpTotal 返回累计扩容次数
func (p *ActorPool) ScaleUpTotal() int64 {
	return atomic.LoadInt64(&p.scaleUpTotal)
}

// ScaleDownTotal 返回累计缩容次数
func (p *ActorPool) ScaleDownTotal() int64 {
	return atomic.LoadInt64(&p.scaleDownTotal)
}

// PoolName 返回池名称
func (p *ActorPool) PoolName() string {
	return p.metricsName
}

// StatsWithScale 返回包含伸缩计数的扩展统计
func (p *ActorPool) StatsWithScale() ActorPoolStatsExt {
	base := p.Stats()
	return ActorPoolStatsExt{
		ActorPoolStats: base,
		ScaleUpTotal:   atomic.LoadInt64(&p.scaleUpTotal),
		ScaleDownTotal: atomic.LoadInt64(&p.scaleDownTotal),
		PoolName:       p.metricsName,
	}
}

// ActorPoolStatsExt 扩展统计（含伸缩次数）
type ActorPoolStatsExt struct {
	ActorPoolStats
	ScaleUpTotal   int64
	ScaleDownTotal int64
	PoolName       string
}

func (s ActorPoolStatsExt) String() string {
	return fmt.Sprintf("Pool[%s] size=%d active=%d routed=%d scaleUp=%d scaleDown=%d",
		s.PoolName, s.CurrentSize, s.ActiveCount, s.TotalRouted, s.ScaleUpTotal, s.ScaleDownTotal)
}
