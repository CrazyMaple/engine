package cluster

import "time"

// ClusterConfig 集群配置
type ClusterConfig struct {
	// ClusterName 集群名称，同名节点才能互相发现
	ClusterName string

	// Address 本节点地址 "host:port"
	Address string

	// SeedNodes 种子节点地址列表，用于引导集群发现
	SeedNodes []string

	// GossipInterval Gossip 协议的发送间隔
	GossipInterval time.Duration

	// GossipFanOut 每轮 Gossip 选择的 peer 数量
	GossipFanOut int

	// HeartbeatInterval 心跳间隔
	HeartbeatInterval time.Duration

	// HeartbeatTimeout 节点超时时间，超过此时间无心跳则标记为 Suspect
	HeartbeatTimeout time.Duration

	// DeadTimeout 节点死亡超时，Suspect 状态超过此时间标记为 Dead
	DeadTimeout time.Duration

	// Kinds 本节点支持的 Actor Kind 列表
	Kinds []string

	// Provider 可选的集群服务发现提供者（Consul、etcd、K8s 等）
	// 如果设置则使用 Provider 发现成员，否则使用 SeedNodes + Gossip
	Provider ClusterProvider
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

// WithSeedNodes 设置种子节点
func (c *ClusterConfig) WithSeedNodes(seeds ...string) *ClusterConfig {
	c.SeedNodes = seeds
	return c
}

// WithKinds 设置支持的 Actor Kind
func (c *ClusterConfig) WithKinds(kinds ...string) *ClusterConfig {
	c.Kinds = kinds
	return c
}

// WithGossipInterval 设置 Gossip 间隔
func (c *ClusterConfig) WithGossipInterval(d time.Duration) *ClusterConfig {
	c.GossipInterval = d
	return c
}

// WithHeartbeatInterval 设置心跳间隔
func (c *ClusterConfig) WithHeartbeatInterval(d time.Duration) *ClusterConfig {
	c.HeartbeatInterval = d
	return c
}

// WithHeartbeatTimeout 设置心跳超时
func (c *ClusterConfig) WithHeartbeatTimeout(d time.Duration) *ClusterConfig {
	c.HeartbeatTimeout = d
	return c
}

// WithProvider 设置集群服务发现提供者
func (c *ClusterConfig) WithProvider(provider ClusterProvider) *ClusterConfig {
	c.Provider = provider
	return c
}
