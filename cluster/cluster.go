package cluster

import (
	"fmt"
	"sync"
	"time"

	"engine/actor"
	engerr "engine/errors"
	"engine/log"
	"engine/remote"
)

// Cluster 集群管理器
type Cluster struct {
	system     *actor.ActorSystem
	remote     *remote.Remote
	config     *ClusterConfig
	memberList *MemberList
	gossiper   *Gossiper
	hashRing   *ConsistentHash
	self       *Member
	gossipPID  *actor.PID
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

	// 创建本节点信息
	c.self = &Member{
		Address:  config.Address,
		Id:       generateNodeId(config.Address),
		Kinds:    config.Kinds,
		Status:   MemberAlive,
		Seq:      1,
		LastSeen: time.Now(),
	}

	c.memberList = NewMemberList(c)
	c.gossiper = NewGossiper(c)

	return c
}

// Start 启动集群
func (c *Cluster) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return nil
	}

	// 注册 Gossip 消息类型到远程类型注册表
	remote.RegisterType(&GossipRequest{})
	remote.RegisterType(&GossipResponse{})
	remote.RegisterType(&GossipState{})
	remote.RegisterType(&MemberGossipState{})

	// 创建 Gossip Actor
	gossipProps := actor.PropsFromProducer(func() actor.Actor {
		return &gossipActor{gossiper: c.gossiper}
	})
	c.gossipPID = c.system.Root.SpawnNamed(gossipProps, "cluster/gossip")

	// 将自己加入成员列表
	c.memberList.UpdateMember(&MemberGossipState{
		Address: c.self.Address,
		Id:      c.self.Id,
		Kinds:   c.self.Kinds,
		Status:  MemberAlive,
		Seq:     c.self.Seq,
	})

	// 更新哈希环
	c.updateHashRing()

	// 监听拓扑变更事件以更新哈希环
	c.system.EventStream.Subscribe(func(event interface{}) {
		if _, ok := event.(*ClusterTopologyEvent); ok {
			c.updateHashRing()
		}
	})

	// 启动 Gossip 协议
	c.gossiper.Start()

	// 使用 Provider 或种子节点进行成员发现
	if c.config.Provider != nil {
		if err := c.config.Provider.Start(c.config.ClusterName, c.self, func(members []*Member) {
			for _, m := range members {
				if m.Address == c.self.Address {
					continue
				}
				c.memberList.UpdateMember(&MemberGossipState{
					Address: m.Address,
					Id:      m.Id,
					Kinds:   m.Kinds,
					Status:  m.Status,
					Seq:     m.Seq,
				})
			}
			c.updateHashRing()
		}); err != nil {
			log.Error("Failed to start cluster provider: %v", &engerr.ClusterError{
				Op: "provider.start", Node: c.self.Address, Cause: err,
			})
			c.connectToSeeds() // 回退到种子节点
		} else if err := c.config.Provider.Register(); err != nil {
			log.Error("Failed to register with cluster provider: %v", &engerr.ClusterError{
				Op: "provider.register", Node: c.self.Address, Cause: err,
			})
		}
	} else {
		c.connectToSeeds()
	}

	c.started = true
	log.Info("Cluster started: %s, node: %s (%s), kinds: %v",
		c.config.ClusterName, c.self.Address, c.self.Id, c.config.Kinds)

	return nil
}

// Stop 优雅停止集群
func (c *Cluster) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return
	}

	// 设置自身为离开状态
	c.self.Status = MemberLeft
	c.self.Seq++
	c.gossiper.SetMemberState(c.self)

	// 最后一轮 Gossip 通知其他节点
	c.gossiper.gossipOnce()
	time.Sleep(100 * time.Millisecond)

	// 从服务发现中注销
	if c.config.Provider != nil {
		c.config.Provider.Deregister()
		c.config.Provider.Stop()
	}

	// 停止 Gossip
	c.gossiper.Stop()

	// 停止 Gossip Actor
	c.system.Root.Stop(c.gossipPID)

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

// connectToSeeds 连接种子节点，异步重试直到成功或集群停止
func (c *Cluster) connectToSeeds() {
	if len(c.config.SeedNodes) == 0 {
		return
	}

	// 在后台重试连接种子节点，间隔与 GossipInterval 一致
	go func() {
		maxRetries := 30 // 最多重试 30 次
		for i := 0; i < maxRetries; i++ {
			// 检查集群是否已停止
			c.mu.RLock()
			started := c.started
			c.mu.RUnlock()
			if !started && i > 0 {
				return
			}

			// 检查是否已经有其他成员（说明已收敛）
			members := c.memberList.GetMembers()
			otherCount := 0
			for _, m := range members {
				if m.Id != c.self.Id {
					otherCount++
				}
			}
			if otherCount > 0 {
				return // 已经发现其他节点，不再重试
			}

			state := c.gossiper.GetState()
			for _, seed := range c.config.SeedNodes {
				if seed == c.config.Address {
					continue
				}
				target := actor.NewPID(seed, "cluster/gossip")
				c.remote.Send(target, c.gossipPID, &GossipRequest{
					ClusterName: c.config.ClusterName,
					State:       state,
				}, 0)
			}

			if i == 0 {
				log.Info("Connecting to seed nodes: %v", c.config.SeedNodes)
			}

			time.Sleep(c.config.GossipInterval)
		}
	}()
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
