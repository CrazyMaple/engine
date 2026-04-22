package cluster

import "time"

// SplitBrainConfig 脑裂检测配置
type SplitBrainConfig struct {
	// Enabled 是否启用脑裂检测
	Enabled bool

	// CheckInterval 检测间隔（默认 5s）
	CheckInterval time.Duration

	// StableWindow 稳定窗口：检测到潜在脑裂后等待此时长再确认
	// 避免瞬时网络抖动导致误判（默认 10s）
	StableWindow time.Duration

	// Resolver 脑裂解决策略（默认 KeepMajorityResolver）
	Resolver SplitBrainResolver
}

// DefaultSplitBrainConfig 返回默认脑裂检测配置
func DefaultSplitBrainConfig() *SplitBrainConfig {
	return &SplitBrainConfig{
		Enabled:       true,
		CheckInterval: 5 * time.Second,
		StableWindow:  10 * time.Second,
		Resolver:      &KeepMajorityResolver{},
	}
}

// WithResolver 设置解决策略
func (c *SplitBrainConfig) WithResolver(r SplitBrainResolver) *SplitBrainConfig {
	c.Resolver = r
	return c
}

// WithCheckInterval 设置检测间隔
func (c *SplitBrainConfig) WithCheckInterval(d time.Duration) *SplitBrainConfig {
	c.CheckInterval = d
	return c
}

// WithStableWindow 设置稳定窗口
func (c *SplitBrainConfig) WithStableWindow(d time.Duration) *SplitBrainConfig {
	c.StableWindow = d
	return c
}
