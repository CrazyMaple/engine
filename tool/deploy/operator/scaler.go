package operator

import (
	"sync"
	"time"

	"engine/log"
)

// ScaleDirection 扩缩容方向
type ScaleDirection int

const (
	ScaleNone ScaleDirection = iota
	ScaleUp
	ScaleDown
)

func (d ScaleDirection) String() string {
	switch d {
	case ScaleUp:
		return "ScaleUp"
	case ScaleDown:
		return "ScaleDown"
	default:
		return "None"
	}
}

// ScaleDecision 扩缩容决策
type ScaleDecision struct {
	Direction    ScaleDirection
	Current      int
	Target       int
	Reason       string
	MetricValues map[string]float64
}

// MetricsProvider 扩缩容指标提供者接口
type MetricsProvider interface {
	// NodeConnections 返回指定节点当前连接数
	NodeConnections(addr string) int64
	// NodeActorCount 返回指定节点当前 Actor 数
	NodeActorCount(addr string) int
	// NodeCPUPercent 返回指定节点 CPU 使用率（0-100）
	NodeCPUPercent(addr string) float64
}

// Scaler 自动扩缩容决策器
// 基于连接数/Actor 数/CPU 触发扩缩容决策，由 Controller 执行
type Scaler struct {
	mu     sync.RWMutex
	policy *ScalePolicy

	provider MetricsProvider

	// 冷却状态
	lastScaleTime time.Time

	// 决策历史（最近 10 条）
	history     []ScaleDecision
	historySize int
}

// NewScaler 创建自动扩缩容器
func NewScaler(policy *ScalePolicy, provider MetricsProvider) *Scaler {
	if policy == nil {
		policy = DefaultScalePolicy()
	}
	return &Scaler{
		policy:      policy,
		provider:    provider,
		historySize: 10,
	}
}

// Evaluate 评估当前集群是否需要扩缩容
// nodes 为当前活跃节点地址列表
// 返回扩缩容决策
func (s *Scaler) Evaluate(nodes []string) ScaleDecision {
	s.mu.RLock()
	policy := s.policy
	lastScale := s.lastScaleTime
	s.mu.RUnlock()

	currentCount := len(nodes)
	decision := ScaleDecision{
		Direction:    ScaleNone,
		Current:      currentCount,
		Target:       currentCount,
		MetricValues: make(map[string]float64),
	}

	if policy == nil || s.provider == nil || currentCount == 0 {
		return decision
	}

	// 冷却期检查
	if !lastScale.IsZero() && time.Since(lastScale) < policy.CooldownPeriod {
		return decision
	}

	// 收集各节点指标
	var totalConns int64
	var totalActors int
	var maxCPU float64

	for _, addr := range nodes {
		conns := s.provider.NodeConnections(addr)
		actors := s.provider.NodeActorCount(addr)
		cpu := s.provider.NodeCPUPercent(addr)

		totalConns += conns
		totalActors += actors
		if cpu > maxCPU {
			maxCPU = cpu
		}
	}

	avgConns := totalConns / int64(currentCount)
	avgActors := totalActors / currentCount

	decision.MetricValues["avg_connections"] = float64(avgConns)
	decision.MetricValues["avg_actors"] = float64(avgActors)
	decision.MetricValues["max_cpu_percent"] = maxCPU

	// 扩容判断：任一指标超阈值
	needScaleUp := false
	reason := ""

	if policy.ConnectionThreshold > 0 && avgConns > policy.ConnectionThreshold {
		needScaleUp = true
		reason = "avg connections %d > threshold %d"
		reason = formatReason("avg connections", float64(avgConns), float64(policy.ConnectionThreshold))
	}
	if policy.ActorThreshold > 0 && avgActors > policy.ActorThreshold {
		needScaleUp = true
		reason = formatReason("avg actors", float64(avgActors), float64(policy.ActorThreshold))
	}
	if policy.CPUThreshold > 0 && maxCPU > float64(policy.CPUThreshold) {
		needScaleUp = true
		reason = formatReason("max CPU%", maxCPU, float64(policy.CPUThreshold))
	}

	if needScaleUp && currentCount < policy.MaxReplicas {
		// 计算扩容数量：每次增加 1 个节点（保守策略）
		target := currentCount + 1
		if target > policy.MaxReplicas {
			target = policy.MaxReplicas
		}
		decision.Direction = ScaleUp
		decision.Target = target
		decision.Reason = reason
		s.recordDecision(decision)
		return decision
	}

	// 缩容判断：所有指标均远低于阈值（低于 50%）
	needScaleDown := currentCount > policy.MinReplicas
	if needScaleDown && policy.ConnectionThreshold > 0 {
		needScaleDown = avgConns < policy.ConnectionThreshold/2
	}
	if needScaleDown && policy.ActorThreshold > 0 {
		needScaleDown = avgActors < policy.ActorThreshold/2
	}
	if needScaleDown && policy.CPUThreshold > 0 {
		needScaleDown = maxCPU < float64(policy.CPUThreshold)/2
	}

	if needScaleDown {
		target := currentCount - 1
		if target < policy.MinReplicas {
			target = policy.MinReplicas
		}
		if target < currentCount {
			decision.Direction = ScaleDown
			decision.Target = target
			decision.Reason = "all metrics below 50% threshold"
			s.recordDecision(decision)
			return decision
		}
	}

	return decision
}

// MarkScaled 标记已执行扩缩容（更新冷却计时器）
func (s *Scaler) MarkScaled() {
	s.mu.Lock()
	s.lastScaleTime = time.Now()
	s.mu.Unlock()
}

// UpdatePolicy 更新扩缩容策略
func (s *Scaler) UpdatePolicy(policy *ScalePolicy) {
	s.mu.Lock()
	s.policy = policy
	s.mu.Unlock()
}

// History 返回最近的扩缩容决策记录
func (s *Scaler) History() []ScaleDecision {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]ScaleDecision, len(s.history))
	copy(result, s.history)
	return result
}

// EvaluateAndApply 评估并通过 Controller 执行扩缩容
func (s *Scaler) EvaluateAndApply(nodes []string, ctrl *Controller) {
	decision := s.Evaluate(nodes)
	if decision.Direction == ScaleNone {
		return
	}

	log.Info("operator/scaler: %s decision: %d -> %d (%s)",
		decision.Direction, decision.Current, decision.Target, decision.Reason)

	ctrl.mu.Lock()
	if ctrl.desired != nil {
		ctrl.desired.Spec.Replicas = decision.Target
		now := time.Now()
		ctrl.desired.Status.LastScaleTime = &now
	}
	ctrl.mu.Unlock()

	s.MarkScaled()

	// 触发 Controller Reconcile 执行实际动作
	ctrl.Reconcile()
}

func (s *Scaler) recordDecision(d ScaleDecision) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, d)
	if len(s.history) > s.historySize {
		s.history = s.history[len(s.history)-s.historySize:]
	}
}

func formatReason(metric string, value, threshold float64) string {
	return metric + ": " + formatFloat(value) + " > " + formatFloat(threshold)
}

func formatFloat(v float64) string {
	if v == float64(int64(v)) {
		return intToStr(int64(v))
	}
	// 简单格式化到 1 位小数
	whole := int64(v)
	frac := int64((v - float64(whole)) * 10)
	if frac < 0 {
		frac = -frac
	}
	return intToStr(whole) + "." + intToStr(frac)
}

func intToStr(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	buf := make([]byte, 0, 20)
	for v > 0 {
		buf = append(buf, byte('0'+v%10))
		v /= 10
	}
	if neg {
		buf = append(buf, '-')
	}
	// reverse
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}
