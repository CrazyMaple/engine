package middleware

import "engine/actor"

// MetricsRegistryAdapter 将 MetricsRegistry 适配为 actor.PoolMetrics 接口
// 用于 ActorPool.WithMetrics() 注入，避免 actor ↔ middleware 循环依赖
type MetricsRegistryAdapter struct {
	Registry *MetricsRegistry
}

func (a *MetricsRegistryAdapter) IncCounter(name, help string, labels map[string]string, delta int64) {
	a.Registry.IncCounter(name, help, labels, delta)
}

func (a *MetricsRegistryAdapter) RegisterGauge(name, help string, fn func() []actor.PoolGaugeValue) {
	a.Registry.RegisterGauge(name, help, func() []GaugeValue {
		poolValues := fn()
		values := make([]GaugeValue, len(poolValues))
		for i, pv := range poolValues {
			values[i] = GaugeValue{Labels: pv.Labels, Value: pv.Value}
		}
		return values
	})
}

// AsPoolMetrics 快捷方法：将 MetricsRegistry 包装为 actor.PoolMetrics
func (r *MetricsRegistry) AsPoolMetrics() actor.PoolMetrics {
	return &MetricsRegistryAdapter{Registry: r}
}
