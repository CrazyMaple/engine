package remote

import (
	"sync"
	"time"

	"engine/log"
)

// NodeInfo 节点信息
type NodeInfo struct {
	Address    string    // 节点地址
	LastSeen   time.Time // 最后心跳时间
}

// Registry 节点注册表（AutoManaged模式）
type Registry struct {
	nodes      map[string]*NodeInfo
	mu         sync.RWMutex
	heartbeat  time.Duration // 心跳间隔
	timeout    time.Duration // 超时时间
	stopChan   chan struct{}
}

// NewRegistry 创建节点注册表
func NewRegistry() *Registry {
	return &Registry{
		nodes:     make(map[string]*NodeInfo),
		heartbeat: 5 * time.Second,
		timeout:   15 * time.Second,
		stopChan:  make(chan struct{}),
	}
}

// Start 启动注册表
func (r *Registry) Start() {
	go r.checkLoop()
}

// Stop 停止注册表
func (r *Registry) Stop() {
	close(r.stopChan)
}

// Register 注册节点
func (r *Registry) Register(address string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if node, ok := r.nodes[address]; ok {
		node.LastSeen = time.Now()
	} else {
		r.nodes[address] = &NodeInfo{
			Address:  address,
			LastSeen: time.Now(),
		}
		log.Info("Node registered: %s", address)
	}
}

// Unregister 注销节点
func (r *Registry) Unregister(address string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.nodes, address)
	log.Info("Node unregistered: %s", address)
}

// GetNodes 获取所有节点
func (r *Registry) GetNodes() []*NodeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	nodes := make([]*NodeInfo, 0, len(r.nodes))
	for _, node := range r.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

// GetNode 获取节点信息
func (r *Registry) GetNode(address string) (*NodeInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	node, ok := r.nodes[address]
	return node, ok
}

// checkLoop 检查节点超时
func (r *Registry) checkLoop() {
	ticker := time.NewTicker(r.heartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.checkTimeout()
		case <-r.stopChan:
			return
		}
	}
}

func (r *Registry) checkTimeout() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	for address, node := range r.nodes {
		if now.Sub(node.LastSeen) > r.timeout {
			delete(r.nodes, address)
			log.Warn("Node timeout: %s", address)
		}
	}
}
