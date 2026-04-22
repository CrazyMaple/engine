package k8s

import (
	"gamelib/middleware"
)

// GateMetricsSource 网关指标来源（解耦 gate 包依赖）
type GateMetricsSource interface {
	ConnCount() int64
}

// SystemMetricsSource ActorSystem 指标来源（解耦 actor 包依赖）
type SystemMetricsSource interface {
	ActorCount() int
}

// HPAMetricsConfig HPA 自动伸缩指标配置
type HPAMetricsConfig struct {
	// Gate 网关实例（可选，nil 则不注册连接数指标）
	Gate GateMetricsSource
	// System ActorSystem 进程注册表（可选，nil 则不注册 Actor 数指标）
	System SystemMetricsSource
	// Registry 指标注册中心
	Registry *middleware.MetricsRegistry
	// NodeRole 节点角色标签（如 "gate"、"game"、"cluster"）
	NodeRole string
	// NodeVersion 节点版本标签（如 "v1.8.0"）
	NodeVersion string
}

// RegisterHPAMetrics 注册 HPA 自动伸缩所需的 Prometheus 指标
// 这些指标通过 prometheus-adapter 或 KEDA 暴露为 K8s 自定义指标，
// 供 HorizontalPodAutoscaler 做弹性伸缩决策。
func RegisterHPAMetrics(cfg HPAMetricsConfig) {
	if cfg.Registry == nil {
		return
	}

	// 客户端连接数 — HPA 可根据连接数扩缩 gate 节点
	if cfg.Gate != nil {
		gate := cfg.Gate
		cfg.Registry.RegisterGauge(
			"engine_gate_connection_count",
			"Current number of client connections to the gate",
			func() []middleware.GaugeValue {
				return []middleware.GaugeValue{
					{Value: float64(gate.ConnCount())},
				}
			},
		)
	}

	// Actor 数量 — HPA 可根据 Actor 数扩缩 game 节点
	if cfg.System != nil {
		system := cfg.System
		cfg.Registry.RegisterGauge(
			"engine_actor_count",
			"Current number of registered actors in the system",
			func() []middleware.GaugeValue {
				return []middleware.GaugeValue{
					{Value: float64(system.ActorCount())},
				}
			},
		)
	}

	// 节点角色信息 — 用于 Prometheus 标签区分节点类型
	if cfg.NodeRole != "" {
		role := cfg.NodeRole
		version := cfg.NodeVersion
		cfg.Registry.RegisterGauge(
			"engine_node_info",
			"Engine node metadata (role, version)",
			func() []middleware.GaugeValue {
				labels := map[string]string{"role": role}
				if version != "" {
					labels["version"] = version
				}
				return []middleware.GaugeValue{
					{Labels: labels, Value: 1},
				}
			},
		)
	}
}

// ActorSystemAdapter 将 ProcessRegistry 适配为 SystemMetricsSource
type ActorSystemAdapter struct {
	CountFn func() int
}

func (a *ActorSystemAdapter) ActorCount() int {
	return a.CountFn()
}
