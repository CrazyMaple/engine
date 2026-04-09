package middleware

import "engine/actor"

// DeadLetterMetricsAdapter 将 MetricsRegistry 适配为 actor.DeadLetterMetrics
type DeadLetterMetricsAdapter struct {
	Registry *MetricsRegistry
}

func (a *DeadLetterMetricsAdapter) IncCounter(name, help string, labels map[string]string, delta int64) {
	a.Registry.IncCounter(name, help, labels, delta)
}

// AsDeadLetterMetrics 快捷方法：将 MetricsRegistry 包装为 actor.DeadLetterMetrics
func (r *MetricsRegistry) AsDeadLetterMetrics() actor.DeadLetterMetrics {
	return &DeadLetterMetricsAdapter{Registry: r}
}
