package federation

import "time"

// FederationConfig 跨集群联邦配置
type FederationConfig struct {
	// LocalClusterID 本集群唯一标识
	LocalClusterID string
	// GatewayAddress 本网关监听地址（用于集群间通信）
	GatewayAddress string
	// PeerClusters 已知的对端集群: clusterID -> gateway address
	PeerClusters map[string]string
	// HeartbeatInterval 对端健康检查间隔（默认 5s）
	HeartbeatInterval time.Duration
	// RequestTimeout 跨集群请求超时（默认 10s）
	RequestTimeout time.Duration
}

// DefaultFederationConfig 创建默认联邦配置
func DefaultFederationConfig(clusterID, gatewayAddr string) *FederationConfig {
	return &FederationConfig{
		LocalClusterID:    clusterID,
		GatewayAddress:    gatewayAddr,
		PeerClusters:      make(map[string]string),
		HeartbeatInterval: 5 * time.Second,
		RequestTimeout:    10 * time.Second,
	}
}

func (c *FederationConfig) defaults() {
	if c.PeerClusters == nil {
		c.PeerClusters = make(map[string]string)
	}
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = 5 * time.Second
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = 10 * time.Second
	}
}

// WithPeer 添加对端集群
func (c *FederationConfig) WithPeer(clusterID, gatewayAddr string) *FederationConfig {
	if c.PeerClusters == nil {
		c.PeerClusters = make(map[string]string)
	}
	c.PeerClusters[clusterID] = gatewayAddr
	return c
}
