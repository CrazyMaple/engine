package canary

import (
	"sync"
	"time"
)

// VersionMetrics 单个版本的聚合指标
type VersionMetrics struct {
	Version      string    `json:"version"`
	RequestCount int64     `json:"request_count"`
	ErrorCount   int64     `json:"error_count"`
	ErrorRate    float64   `json:"error_rate"`
	AvgLatencyNs int64    `json:"avg_latency_ns"`
	P99LatencyNs int64    `json:"p99_latency_ns"`
	CollectedAt  time.Time `json:"collected_at"`
}

// MetricsSource 版本指标数据源（适配器接口）
type MetricsSource interface {
	GetVersionMetrics(version string) *VersionMetrics
}

// CompareReport 版本对比报告
type CompareReport struct {
	Baseline       *VersionMetrics `json:"baseline"`
	Canary         *VersionMetrics `json:"canary"`
	ErrorRateDelta float64         `json:"error_rate_delta"`  // 正值表示 canary 更差
	LatencyDelta   float64         `json:"latency_delta_pct"` // 延迟变化百分比
	Recommendation string          `json:"recommendation"`    // "promote" | "rollback" | "continue"
}

// CompareThresholds 自动决策阈值
type CompareThresholds struct {
	MaxErrorRateDelta float64 // 错误率差异上限（如 0.01 = 1%）
	MaxLatencyDelta   float64 // 延迟增长上限百分比（如 10.0 = 10%）
	MinRequestCount   int64   // 最少请求数才做决策
}

// DefaultThresholds 默认决策阈值
var DefaultThresholds = CompareThresholds{
	MaxErrorRateDelta: 0.01,
	MaxLatencyDelta:   10.0,
	MinRequestCount:   100,
}

// Comparator 版本指标对比器
type Comparator struct {
	source     MetricsSource
	snapshots  map[string]*VersionMetrics
	thresholds CompareThresholds
	mu         sync.RWMutex
}

// NewComparator 创建对比器
func NewComparator(src MetricsSource, thresholds CompareThresholds) *Comparator {
	return &Comparator{
		source:     src,
		snapshots:  make(map[string]*VersionMetrics),
		thresholds: thresholds,
	}
}

// Collect 采集指定版本的最新指标
func (c *Comparator) Collect(version string) {
	if c.source == nil {
		return
	}
	metrics := c.source.GetVersionMetrics(version)
	if metrics == nil {
		return
	}
	c.mu.Lock()
	c.snapshots[version] = metrics
	c.mu.Unlock()
}

// GetSnapshot 获取已采集的版本指标快照
func (c *Comparator) GetSnapshot(version string) *VersionMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshots[version]
}

// Compare 对比两个版本的指标
func (c *Comparator) Compare(baselineVersion, canaryVersion string) CompareReport {
	c.mu.RLock()
	baseline := c.snapshots[baselineVersion]
	canaryM := c.snapshots[canaryVersion]
	c.mu.RUnlock()

	report := CompareReport{
		Baseline: baseline,
		Canary:   canaryM,
	}

	if baseline == nil || canaryM == nil {
		report.Recommendation = "continue"
		return report
	}

	// 错误率差异
	report.ErrorRateDelta = canaryM.ErrorRate - baseline.ErrorRate

	// 延迟变化百分比
	if baseline.AvgLatencyNs > 0 {
		report.LatencyDelta = float64(canaryM.AvgLatencyNs-baseline.AvgLatencyNs) / float64(baseline.AvgLatencyNs) * 100
	}

	// 自动决策
	report.Recommendation = c.recommend(report)
	return report
}

func (c *Comparator) recommend(report CompareReport) string {
	if report.Canary == nil || report.Baseline == nil {
		return "continue"
	}

	// 请求量不足，继续观察
	if report.Canary.RequestCount < c.thresholds.MinRequestCount {
		return "continue"
	}

	// 错误率超标 → 回滚
	if report.ErrorRateDelta > c.thresholds.MaxErrorRateDelta {
		return "rollback"
	}

	// 延迟增长超标 → 回滚
	if report.LatencyDelta > c.thresholds.MaxLatencyDelta {
		return "rollback"
	}

	// 指标正常 → 可以全量
	return "promote"
}

// SimpleMetricsSource 简单的内存指标源（用于测试和手动设置）
type SimpleMetricsSource struct {
	mu      sync.RWMutex
	metrics map[string]*VersionMetrics
}

// NewSimpleMetricsSource 创建简单指标源
func NewSimpleMetricsSource() *SimpleMetricsSource {
	return &SimpleMetricsSource{
		metrics: make(map[string]*VersionMetrics),
	}
}

// Set 设置版本指标
func (s *SimpleMetricsSource) Set(version string, m *VersionMetrics) {
	s.mu.Lock()
	s.metrics[version] = m
	s.mu.Unlock()
}

// GetVersionMetrics 获取版本指标
func (s *SimpleMetricsSource) GetVersionMetrics(version string) *VersionMetrics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.metrics[version]
}
