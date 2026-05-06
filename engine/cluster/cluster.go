package cluster

import (
	"fmt"
	"sync"
	"time"

	"engine/actor"
	"engine/log"
	"engine/remote"
)

// Cluster 集群协调入口
//
// v1.13 收缩后职责：启动 Provider → 接收成员快照 → 更新 MemberList →
// 更新 ConsistentHash → 发布 ClusterTopologyEvent。
// 不再直接持有 gossip / splitbrain / federation / migration 等运维实现，
// 这些一律由外部（gamelib / tool）按 Provider 契约或独立组件提供。
type Cluster struct {
	system     *actor.ActorSystem
	remote     *remote.Remote
	config     *ClusterConfig
	memberList *MemberList
	hashRing   *ConsistentHash
	self       *Member
	provider   Provider
	started    bool
	mu         sync.RWMutex
}

// NewCluster 创建集群
func NewCluster(system *actor.ActorSystem, r *remote.Remote, config *ClusterConfig) *Cluster {
	c := &Cluster{
		system:   system,
		remote:   r,
		config:   config,
		hashRing: NewConsistentHash(),
	}

	c.self = &Member{
		Address:  config.Address,
		Id:       generateNodeId(config.Address),
		Kinds:    config.Kinds,
		Status:   MemberAlive,
		Seq:      1,
		LastSeen: time.Now(),
	}

	c.memberList = NewMemberList(c)

	return c
}

// Start 启动集群。
//
// 行为（见 ADR v1.13-001 §2.2 / §2.5）：
//  1. 选定 Provider：优先 config.Provider，否则用 SeedNodes 构造 StaticProvider；
//  2. 把自己加入 MemberList（保证单节点集群也有可见成员）；
//  3. 订阅 ClusterTopologyEvent 以更新一致性哈希环；
//  4. 在调用 Provider.Start 之前先 type-assert 扩展能力（决定后续是否调
//     Register/Deregister）；扩展能力检测不持久化字段，避免反向耦合；
//  5. 调 Provider.Start：成员快照通过 onChange 回调送达 ApplySnapshot；
//  6. 如果 Provider 是 RegistrableProvider，紧接着调用 Register(self)；
//     注册失败 → fail-fast：回滚 Provider.Stop 并返回错误，避免出现
//     "Cluster.Start 成功但本节点未注册到服务发现后端" 的隐性错误状态；
//  7. Provider 启动失败本身亦 fail-fast，不允许 fallback。
func (c *Cluster) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return nil
	}

	c.memberList.UpdateMember(c.self.Clone())
	c.updateHashRing()

	c.system.EventStream.Subscribe(func(event interface{}) {
		if _, ok := event.(*ClusterTopologyEvent); ok {
			c.updateHashRing()
		}
	})

	provider := c.config.Provider
	if provider == nil {
		provider = NewStaticProvider(c.config.SeedNodes...)
	}

	// ADR 001 §2.2：在 Provider.Start 之前判断扩展能力，
	// 不持有具体扩展接口字段。
	registrable, _ := provider.(RegistrableProvider)

	onChange := func(members []*Member) {
		c.memberList.ApplySnapshot(members, c.self.Id)
		c.updateHashRing()
	}

	if err := provider.Start(c.config.ClusterName, c.self, onChange); err != nil {
		return fmt.Errorf("cluster provider %T start: %w", provider, err)
	}

	if registrable != nil {
		if err := registrable.Register(c.self); err != nil {
			// fail-fast：回滚 Provider.Stop，确保进程态与可观察态一致。
			if stopErr := provider.Stop(); stopErr != nil {
				log.Error("cluster provider stop after register failure: %v", stopErr)
			}
			return fmt.Errorf("cluster provider %T register: %w", provider, err)
		}
	}

	c.provider = provider
	c.started = true
	log.Info("Cluster started: %s, node: %s (%s), kinds: %v",
		c.config.ClusterName, c.self.Address, c.self.Id, c.config.Kinds)

	return nil
}

// Stop 优雅停止集群。
func (c *Cluster) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return
	}

	c.self.Status = MemberLeft
	c.self.Seq++

	if c.provider != nil {
		if reg, ok := c.provider.(RegistrableProvider); ok {
			if err := reg.Deregister(c.self); err != nil {
				log.Error("cluster provider deregister: %v", err)
			}
		}
		if err := c.provider.Stop(); err != nil {
			log.Error("cluster provider stop: %v", err)
		}
		c.provider = nil
	}

	c.started = false
	log.Info("Cluster stopped: %s", c.self.Address)
}

// Members 获取所有存活成员
func (c *Cluster) Members() []*Member {
	return c.memberList.GetMembers()
}

// MembersByKind 获取支持指定 Kind 的存活成员
func (c *Cluster) MembersByKind(kind string) []*Member {
	return c.memberList.GetMembersByKind(kind)
}

// GetMemberForIdentity 使用一致性哈希定位 identity 的归属节点
func (c *Cluster) GetMemberForIdentity(identity, kind string) *Member {
	return c.hashRing.GetMember(identity, kind)
}

// Self 获取本节点信息
func (c *Cluster) Self() *Member {
	return c.self
}

// System 获取 ActorSystem
func (c *Cluster) System() *actor.ActorSystem {
	return c.system
}

// Remote 获取 Remote
func (c *Cluster) Remote() *remote.Remote {
	return c.remote
}

// Config 获取集群配置
func (c *Cluster) Config() *ClusterConfig {
	return c.config
}

// updateHashRing 更新一致性哈希环
func (c *Cluster) updateHashRing() {
	members := c.memberList.GetMembers()
	c.hashRing.UpdateMembers(members)
}

// generateNodeId 基于地址和时间生成节点 ID
func generateNodeId(address string) string {
	return fmt.Sprintf("%s-%d", address, time.Now().UnixNano()%100000)
}
