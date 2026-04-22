package cluster

import (
	"math/rand"
	"sync"
	"time"

	"engine/actor"
	"engine/log"
)

// Gossiper 管理 Gossip 协议
type Gossiper struct {
	cluster  *Cluster
	state    *GossipState
	mu       sync.RWMutex
	stopChan chan struct{}
}

// NewGossiper 创建 Gossiper
func NewGossiper(cluster *Cluster) *Gossiper {
	return &Gossiper{
		cluster:  cluster,
		state:    NewGossipState(),
		stopChan: make(chan struct{}),
	}
}

// Start 启动 Gossip 循环和心跳检测
func (g *Gossiper) Start() {
	// 初始化本节点状态
	g.SetMemberState(g.cluster.self)

	go g.gossipLoop()
	go g.heartbeatLoop()
}

// Stop 停止 Gossiper
func (g *Gossiper) Stop() {
	close(g.stopChan)
}

// SetMemberState 更新本地成员状态
func (g *Gossiper) SetMemberState(member *Member) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.state.SetMember(member)
}

// GetState 获取当前 Gossip 状态的副本
func (g *Gossiper) GetState() *GossipState {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.state.Clone()
}

// Merge 合并远程 Gossip 状态
func (g *Gossiper) Merge(remote *GossipState) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.state.Merge(remote)
}

// gossipLoop 定期向随机 peer 发送 Gossip 消息
func (g *Gossiper) gossipLoop() {
	ticker := time.NewTicker(g.cluster.config.GossipInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			g.gossipOnce()
		case <-g.stopChan:
			return
		}
	}
}

// gossipOnce 执行一轮 Gossip
func (g *Gossiper) gossipOnce() {
	peers := g.selectPeers()
	if len(peers) == 0 {
		return
	}

	state := g.GetState()

	for _, peer := range peers {
		g.sendGossip(peer, state)
	}
}

// selectPeers 随机选择 GossipFanOut 个 peer
func (g *Gossiper) selectPeers() []*Member {
	members := g.cluster.memberList.GetMembers()

	// 排除自己
	var peers []*Member
	for _, m := range members {
		if m.Id != g.cluster.self.Id {
			peers = append(peers, m)
		}
	}

	// 如果没有已知存活 peer，回退到种子节点重新尝试连接
	if len(peers) == 0 {
		for _, seed := range g.cluster.config.SeedNodes {
			if seed == g.cluster.config.Address {
				continue
			}
			peers = append(peers, &Member{
				Address: seed,
				Status:  MemberAlive,
			})
		}
		if len(peers) == 0 {
			return nil
		}
	}

	// Fisher-Yates 洗牌
	rand.Shuffle(len(peers), func(i, j int) {
		peers[i], peers[j] = peers[j], peers[i]
	})

	fanOut := g.cluster.config.GossipFanOut
	if fanOut > len(peers) {
		fanOut = len(peers)
	}

	return peers[:fanOut]
}

// sendGossip 向一个 peer 发送 Gossip 请求
func (g *Gossiper) sendGossip(peer *Member, state *GossipState) {
	if g.cluster.remote == nil {
		return
	}

	// 发送到远端的 gossip actor
	target := actor.NewPID(peer.Address, "cluster/gossip")
	g.cluster.remote.Send(target, g.cluster.gossipPID, &GossipRequest{
		ClusterName: g.cluster.config.ClusterName,
		State:       state,
	}, 0)
}

// heartbeatLoop 心跳检测循环
func (g *Gossiper) heartbeatLoop() {
	ticker := time.NewTicker(g.cluster.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			g.heartbeatOnce()
		case <-g.stopChan:
			return
		}
	}
}

// heartbeatOnce 执行一轮心跳
func (g *Gossiper) heartbeatOnce() {
	// 递增本地 Seq 作为心跳
	g.cluster.self.Seq++
	g.SetMemberState(g.cluster.self)

	// 检查其他成员是否超时
	members := g.cluster.memberList.GetAllMembers()
	now := time.Now()

	for _, member := range members {
		if member.Id == g.cluster.self.Id {
			continue
		}

		elapsed := now.Sub(member.LastSeen)

		switch member.Status {
		case MemberAlive:
			if elapsed > g.cluster.config.HeartbeatTimeout {
				g.cluster.memberList.MarkSuspect(member.Id)
			}
		case MemberSuspect:
			if elapsed > g.cluster.config.DeadTimeout {
				g.cluster.memberList.MarkDead(member.Id)
			}
		}
	}
}

// notifyTopologyChange 通知拓扑变更
func (g *Gossiper) notifyTopologyChange() {
	g.mu.RLock()
	defer g.mu.RUnlock()

	for id, state := range g.state.Members {
		_ = id
		g.cluster.memberList.UpdateMember(state)
	}
}

// gossipActor 处理远程 Gossip 消息的 Actor
type gossipActor struct {
	gossiper *Gossiper
}

func (a *gossipActor) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		// 初始化
	case *GossipRequest:
		if msg.ClusterName != a.gossiper.cluster.config.ClusterName {
			log.Debug("Ignoring gossip from different cluster: %s", msg.ClusterName)
			return
		}

		// 合并远程状态
		changed := a.gossiper.Merge(msg.State)
		if changed {
			a.gossiper.notifyTopologyChange()
		}

		// 回复本地状态
		ctx.Respond(&GossipResponse{
			State: a.gossiper.GetState(),
		})

	case *GossipResponse:
		// 合并响应中的状态
		changed := a.gossiper.Merge(msg.State)
		if changed {
			a.gossiper.notifyTopologyChange()
		}
	}
}
