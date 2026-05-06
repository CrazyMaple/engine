package cluster

import "time"

// ClusterConfig 集群配置
type ClusterConfig struct {
	// ClusterName 集群名称，同名节点才能互相发现
	ClusterName string

	// Address 本节点地址 "host:port"
	Address string

	// SeedNodes 种子节点地址列表。
	//
	// Deprecated: v1.13 起改用 Provider 注入。SeedNodes 仅在 Provider 为 nil
	// 时通过 StaticProvider 兜底，将在后续版本移除。
	SeedNodes []string

	// GossipInterval Gossip 协议的发送间隔。
	//
	// Deprecated: engine 不再内嵌 gossip。Phase 4 决定移除时机。
	GossipInterval time.Duration

	// GossipFanOut 每轮 Gossip 选择的 peer 数量。
	//
	// Deprecated: engine 不再内嵌 gossip。Phase 4 决定移除时机。
	GossipFanOut int

	// HeartbeatInterval 心跳间隔。
	//
	// Deprecated: 心跳改由具体 Provider 实现承担。Phase 4 决定移除时机。
	HeartbeatInterval time.Duration

	// HeartbeatTimeout 节点超时时间。
	//
	// Deprecated: 心跳改由具体 Provider 实现承担。Phase 4 决定移除时机。
	HeartbeatTimeout time.Duration

	// DeadTimeout 节点死亡超时。
	//
	// Deprecated: 心跳改由具体 Provider 实现承担。Phase 4 决定移除时机。
	DeadTimeout time.Duration

	// Kinds 本节点支持的 Actor Kind 列表
	Kinds []string

	// Provider 集群成员发现提供者；为 nil 时使用 SeedNodes + StaticProvider 兜底。
	Provider Provider
}

// DefaultClusterConfig 创建默认配置
func DefaultClusterConfig(clusterName, address string) *ClusterConfig {
	return &ClusterConfig{
		ClusterName:       clusterName,
		Address:           address,
		GossipInterval:    500 * time.Millisecond,
		GossipFanOut:      3,
		HeartbeatInterval: 2 * time.Second,
		HeartbeatTimeout:  10 * time.Second,
		DeadTimeout:       15 * time.Second,
		Kinds:             []string{},
	}
}

// WithSeedNodes 设置种子节点。
//
// Deprecated: v1.13 起改用 WithProvider 注入。Phase 4 决定移除时机。
func (c *ClusterConfig) WithSeedNodes(seeds ...string) *ClusterConfig {
	c.SeedNodes = seeds
	return c
}

// WithKinds 设置支持的 Actor Kind
func (c *ClusterConfig) WithKinds(kinds ...string) *ClusterConfig {
	c.Kinds = kinds
	return c
}

// WithGossipInterval 设置 Gossip 间隔。
//
// Deprecated: engine 不再内嵌 gossip。Phase 4 决定移除时机。
func (c *ClusterConfig) WithGossipInterval(d time.Duration) *ClusterConfig {
	c.GossipInterval = d
	return c
}

// WithHeartbeatInterval 设置心跳间隔。
//
// Deprecated: 心跳改由具体 Provider 承担。Phase 4 决定移除时机。
func (c *ClusterConfig) WithHeartbeatInterval(d time.Duration) *ClusterConfig {
	c.HeartbeatInterval = d
	return c
}

// WithHeartbeatTimeout 设置心跳超时。
//
// Deprecated: 心跳改由具体 Provider 承担。Phase 4 决定移除时机。
func (c *ClusterConfig) WithHeartbeatTimeout(d time.Duration) *ClusterConfig {
	c.HeartbeatTimeout = d
	return c
}

// WithProvider 设置集群成员发现提供者
func (c *ClusterConfig) WithProvider(provider Provider) *ClusterConfig {
	c.Provider = provider
	return c
}
