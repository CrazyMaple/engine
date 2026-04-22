package federation

import (
	"sync"
	"time"
)

// ClusterEntry 集群注册表条目
type ClusterEntry struct {
	ClusterID      string
	GatewayAddress string
	Status         string // "alive", "suspect", "dead"
	Kinds          []string
	LastSeen       time.Time
}

// ClusterRegistry 集群注册表，维护 clusterID → 网关地址映射
type ClusterRegistry struct {
	clusters map[string]*ClusterEntry
	mu       sync.RWMutex
}

// NewClusterRegistry 创建集群注册表
func NewClusterRegistry() *ClusterRegistry {
	return &ClusterRegistry{
		clusters: make(map[string]*ClusterEntry),
	}
}

// Register 注册或更新集群信息
func (r *ClusterRegistry) Register(clusterID, gatewayAddr string, kinds []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.clusters[clusterID] = &ClusterEntry{
		ClusterID:      clusterID,
		GatewayAddress: gatewayAddr,
		Status:         "alive",
		Kinds:          kinds,
		LastSeen:       time.Now(),
	}
}

// Unregister 移除集群
func (r *ClusterRegistry) Unregister(clusterID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clusters, clusterID)
}

// Lookup 查找集群
func (r *ClusterRegistry) Lookup(clusterID string) (*ClusterEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.clusters[clusterID]
	return e, ok
}

// All 返回所有已注册的集群
func (r *ClusterRegistry) All() []*ClusterEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entries := make([]*ClusterEntry, 0, len(r.clusters))
	for _, e := range r.clusters {
		entries = append(entries, e)
	}
	return entries
}

// UpdateStatus 更新集群状态
func (r *ClusterRegistry) UpdateStatus(clusterID, status string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if e, ok := r.clusters[clusterID]; ok {
		e.Status = status
		if status == "alive" {
			e.LastSeen = time.Now()
		}
	}
}

// Count 返回已注册集群数
func (r *ClusterRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.clusters)
}
