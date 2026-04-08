package cluster

import (
	"fmt"
	"sync"
	"time"

	"engine/actor"
	"engine/log"
)

// SingletonConfig 集群单例配置
type SingletonConfig struct {
	// Kind 单例 Actor 的 Kind 名称
	Kind string
	// Props Actor 配置
	Props *actor.Props
	// HandoverTimeout 节点切换时等待旧实例停止的超时时间
	HandoverTimeout time.Duration
}

// ClusterSingleton 集群单例管理器
// 保证整个集群内某种 Actor 只有一个实例运行
// 通过一致性哈希 + 固定 identity 实现确定性选主
// 当负责节点变化时，自动在新节点激活、旧节点去激活
type ClusterSingleton struct {
	cluster    *Cluster
	singletons map[string]*singletonEntry
	mu         sync.RWMutex
	stopCh     chan struct{}
}

// singletonEntry 单例条目
type singletonEntry struct {
	config   SingletonConfig
	pid      *actor.PID // 本地实例的 PID（只有负责节点才非 nil）
	isOwner  bool       // 本节点是否为当前负责节点
	identity string     // 用于一致性哈希的固定 identity
}

// NewClusterSingleton 创建集群单例管理器
func NewClusterSingleton(c *Cluster) *ClusterSingleton {
	return &ClusterSingleton{
		cluster:    c,
		singletons: make(map[string]*singletonEntry),
		stopCh:     make(chan struct{}),
	}
}

// Register 注册一个集群单例
// 注册后会根据当前集群拓扑决定是否在本节点激活
func (cs *ClusterSingleton) Register(config SingletonConfig) error {
	if config.Kind == "" {
		return fmt.Errorf("singleton kind cannot be empty")
	}
	if config.Props == nil {
		return fmt.Errorf("singleton props cannot be nil")
	}
	if config.HandoverTimeout <= 0 {
		config.HandoverTimeout = 10 * time.Second
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()

	if _, exists := cs.singletons[config.Kind]; exists {
		return fmt.Errorf("singleton %s already registered", config.Kind)
	}

	entry := &singletonEntry{
		config:   config,
		identity: "singleton:" + config.Kind,
	}
	cs.singletons[config.Kind] = entry

	// 立即检查是否应在本节点激活
	cs.checkOwnership(entry)

	return nil
}

// Unregister 注销一个集群单例
func (cs *ClusterSingleton) Unregister(kind string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	entry, ok := cs.singletons[kind]
	if !ok {
		return
	}

	if entry.isOwner && entry.pid != nil {
		cs.cluster.System().Root.Stop(entry.pid)
		entry.pid = nil
		entry.isOwner = false
	}

	delete(cs.singletons, kind)
}

// Get 获取单例的 PID
// 如果本节点是负责节点，返回本地 PID
// 否则返回远程 PID（指向负责节点上的 Actor）
func (cs *ClusterSingleton) Get(kind string) (*actor.PID, error) {
	cs.mu.RLock()
	entry, ok := cs.singletons[kind]
	cs.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("singleton %s not registered", kind)
	}

	// 确定负责节点
	member := cs.cluster.GetMemberForIdentity(entry.identity, kind)
	if member == nil {
		return nil, fmt.Errorf("no member available for singleton %s", kind)
	}

	singletonID := "singleton/" + kind

	if member.Address == cs.cluster.Self().Address {
		// 本地
		if entry.pid != nil {
			return entry.pid, nil
		}
		return nil, fmt.Errorf("singleton %s not yet activated locally", kind)
	}

	// 远程 PID
	return actor.NewPID(member.Address, singletonID), nil
}

// Start 启动单例管理器，监听拓扑变更
func (cs *ClusterSingleton) Start() {
	// 监听集群拓扑变更
	cs.cluster.System().EventStream.Subscribe(func(event interface{}) {
		if _, ok := event.(*ClusterTopologyEvent); ok {
			cs.onTopologyChange()
		}
	})

	log.Info("ClusterSingleton started on %s", cs.cluster.Self().Address)
}

// Stop 停止单例管理器，去激活所有本地单例
func (cs *ClusterSingleton) Stop() {
	close(cs.stopCh)

	cs.mu.Lock()
	defer cs.mu.Unlock()

	for _, entry := range cs.singletons {
		if entry.isOwner && entry.pid != nil {
			cs.cluster.System().Root.Stop(entry.pid)
			entry.pid = nil
			entry.isOwner = false
			log.Info("ClusterSingleton: deactivated %s on shutdown", entry.config.Kind)
		}
	}
}

// onTopologyChange 拓扑变更时重新检查所有单例的归属
func (cs *ClusterSingleton) onTopologyChange() {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	for _, entry := range cs.singletons {
		cs.checkOwnership(entry)
	}
}

// checkOwnership 检查单例是否应该在本节点运行（调用方需持有锁）
func (cs *ClusterSingleton) checkOwnership(entry *singletonEntry) {
	member := cs.cluster.GetMemberForIdentity(entry.identity, entry.config.Kind)

	shouldOwn := member != nil && member.Address == cs.cluster.Self().Address

	if shouldOwn && !entry.isOwner {
		// 需要在本节点激活
		cs.activateLocal(entry)
	} else if !shouldOwn && entry.isOwner {
		// 需要去激活本地实例
		cs.deactivateLocal(entry)
	}
}

// activateLocal 在本节点激活单例
func (cs *ClusterSingleton) activateLocal(entry *singletonEntry) {
	singletonID := "singleton/" + entry.config.Kind
	pid := cs.cluster.System().Root.SpawnNamed(entry.config.Props, singletonID)
	entry.pid = pid
	entry.isOwner = true

	log.Info("ClusterSingleton: activated %s on %s (PID: %s)",
		entry.config.Kind, cs.cluster.Self().Address, pid.String())

	// 发布单例激活事件
	cs.cluster.System().EventStream.Publish(&SingletonActivated{
		Kind: entry.config.Kind,
		PID:  pid,
		Node: cs.cluster.Self().Address,
	})
}

// deactivateLocal 去激活本节点上的单例
func (cs *ClusterSingleton) deactivateLocal(entry *singletonEntry) {
	if entry.pid != nil {
		cs.cluster.System().Root.Stop(entry.pid)
		log.Info("ClusterSingleton: deactivated %s on %s",
			entry.config.Kind, cs.cluster.Self().Address)

		cs.cluster.System().EventStream.Publish(&SingletonDeactivated{
			Kind: entry.config.Kind,
			Node: cs.cluster.Self().Address,
		})
	}
	entry.pid = nil
	entry.isOwner = false
}

// --- 单例事件 ---

// SingletonActivated 单例激活事件
type SingletonActivated struct {
	Kind string
	PID  *actor.PID
	Node string
}

// SingletonDeactivated 单例去激活事件
type SingletonDeactivated struct {
	Kind string
	Node string
}
