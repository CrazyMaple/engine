// Package gossip 实现 engine/cluster.Provider 的 gossip 风格成员发现。
//
// 设计要点：
//   - 通过注入的 ActorSystem + Remote 在 cluster/gossip 路径上 spawn 一个本地 Actor，
//     与远端节点交换 GossipRequest/GossipResponse。
//   - 内部维护 SWIM 风格的 CRDT（按 Seq 取大值合并）。
//   - 心跳：定期 ++self.Seq；发现成员超时则推进 Suspect/Dead 状态。
//   - 每次状态收敛 → onChange(<当前活跃 Member 全量快照>)，符合 ADR 001 §2.3。
package gossip

import (
	"math/rand"
	"sync"
	"time"

	"engine/actor"
	"engine/cluster"
	"engine/log"
	"engine/remote"
)

// Config gossip Provider 配置。零值不可用。
type Config struct {
	GossipInterval    time.Duration
	GossipFanOut      int
	HeartbeatInterval time.Duration
	HeartbeatTimeout  time.Duration
	DeadTimeout       time.Duration

	// SeedNodes 初始已知 peer 地址列表（用于冷启动连接）。
	SeedNodes []string
}

// DefaultConfig 推荐默认值。
func DefaultConfig() Config {
	return Config{
		GossipInterval:    500 * time.Millisecond,
		GossipFanOut:      3,
		HeartbeatInterval: 2 * time.Second,
		HeartbeatTimeout:  10 * time.Second,
		DeadTimeout:       15 * time.Second,
	}
}

// Provider gossip 风格的集群成员发现实现。
type Provider struct {
	system *actor.ActorSystem
	remote *remote.Remote
	cfg    Config

	mu          sync.Mutex
	started     bool
	clusterName string
	self        *cluster.Member
	onChange    func([]*cluster.Member)

	state    *gossipState
	lastSeen map[string]time.Time

	gossipPID *actor.PID
	stopChan  chan struct{}
}

// New 创建一个 gossip Provider。system 和 r 必须非 nil；测试可注入 fake。
func New(system *actor.ActorSystem, r *remote.Remote, cfg Config) *Provider {
	if cfg.GossipInterval <= 0 {
		cfg = DefaultConfig()
	}
	return &Provider{
		system:   system,
		remote:   r,
		cfg:      cfg,
		state:    newGossipState(),
		lastSeen: make(map[string]time.Time),
	}
}

// Start 实现 cluster.Provider。
func (p *Provider) Start(clusterName string, self *cluster.Member, onChange func([]*cluster.Member)) error {
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return nil
	}
	p.started = true
	p.clusterName = clusterName
	p.self = self
	p.onChange = onChange
	p.stopChan = make(chan struct{})
	p.mu.Unlock()

	remote.RegisterType(&GossipRequest{})
	remote.RegisterType(&GossipResponse{})

	props := actor.PropsFromProducer(func() actor.Actor {
		return &gossipActor{provider: p}
	})
	p.gossipPID = p.system.Root.SpawnNamed(props, "cluster/gossip")

	p.setMember(p.self)
	p.publish()

	go p.gossipLoop()
	go p.heartbeatLoop()

	return nil
}

// Stop 实现 cluster.Provider；幂等。
func (p *Provider) Stop() error {
	p.mu.Lock()
	if !p.started {
		p.mu.Unlock()
		return nil
	}
	p.started = false
	stop := p.stopChan
	pid := p.gossipPID
	p.stopChan = nil
	p.gossipPID = nil
	p.mu.Unlock()

	if stop != nil {
		close(stop)
	}
	if pid != nil && p.system != nil {
		p.system.Root.Stop(pid)
	}
	return nil
}

// Register 实现 cluster.RegistrableProvider：把自己加进 gossip state。
func (p *Provider) Register(self *cluster.Member) error {
	p.setMember(self)
	return nil
}

// Deregister 实现 cluster.RegistrableProvider：把自己标 Left 并 gossip 一轮。
func (p *Provider) Deregister(self *cluster.Member) error {
	self.Status = cluster.MemberLeft
	self.Seq++
	p.setMember(self)
	p.gossipOnce()
	return nil
}

// --- 内部状态管理 ---

func (p *Provider) setMember(m *cluster.Member) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state.setMember(toState(m))
	p.lastSeen[m.Id] = time.Now()
}

func (p *Provider) snapshot() *gossipState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state.clone()
}

// merge 把远端 state 合并进本地，返回是否有变化。
func (p *Provider) merge(remoteState *gossipState) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	changed := p.state.merge(remoteState)
	if changed {
		now := time.Now()
		for id := range remoteState.Members {
			p.lastSeen[id] = now
		}
	}
	return changed
}

// activeMembers 返回当前 gossip state 中所有非 Left 成员（含 Suspect、Dead 也排除）。
// onChange 推送的是"活跃成员全量快照"，与 ADR 001 §2.3 一致。
func (p *Provider) activeMembers() []*cluster.Member {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*cluster.Member, 0, len(p.state.Members))
	for _, s := range p.state.Members {
		if cluster.MemberStatus(s.Status) != cluster.MemberAlive {
			continue
		}
		out = append(out, fromState(s))
	}
	return out
}

func (p *Provider) publish() {
	cb := p.onChange
	if cb == nil {
		return
	}
	cb(p.activeMembers())
}

// --- gossip 循环 ---

func (p *Provider) gossipLoop() {
	ticker := time.NewTicker(p.cfg.GossipInterval)
	defer ticker.Stop()

	p.mu.Lock()
	stop := p.stopChan
	p.mu.Unlock()
	if stop == nil {
		return
	}

	for {
		select {
		case <-ticker.C:
			p.gossipOnce()
		case <-stop:
			return
		}
	}
}

func (p *Provider) gossipOnce() {
	if p.remote == nil {
		return
	}

	peers := p.selectPeers()
	if len(peers) == 0 {
		return
	}

	state := p.snapshot()

	p.mu.Lock()
	pid := p.gossipPID
	clusterName := p.clusterName
	p.mu.Unlock()
	if pid == nil {
		return
	}

	for _, peer := range peers {
		target := actor.NewPID(peer, "cluster/gossip")
		p.remote.Send(target, pid, &GossipRequest{
			ClusterName: clusterName,
			State:       state,
		}, 0)
	}
}

// selectPeers 随机选择最多 fanOut 个候选 peer 地址（不含自己；若没有已知活跃成员，
// 退回 SeedNodes）。
func (p *Provider) selectPeers() []string {
	p.mu.Lock()
	selfID := ""
	if p.self != nil {
		selfID = p.self.Id
	}
	var addrs []string
	for id, s := range p.state.Members {
		if id == selfID {
			continue
		}
		if cluster.MemberStatus(s.Status) != cluster.MemberAlive {
			continue
		}
		addrs = append(addrs, s.Address)
	}
	p.mu.Unlock()

	if len(addrs) == 0 {
		// fallback：回退到 seeds
		selfAddr := ""
		if p.self != nil {
			selfAddr = p.self.Address
		}
		for _, s := range p.cfg.SeedNodes {
			if s != "" && s != selfAddr {
				addrs = append(addrs, s)
			}
		}
		if len(addrs) == 0 {
			return nil
		}
	}

	rand.Shuffle(len(addrs), func(i, j int) { addrs[i], addrs[j] = addrs[j], addrs[i] })

	fanOut := p.cfg.GossipFanOut
	if fanOut <= 0 {
		fanOut = 1
	}
	if fanOut > len(addrs) {
		fanOut = len(addrs)
	}
	return addrs[:fanOut]
}

// --- 心跳与故障检测 ---

func (p *Provider) heartbeatLoop() {
	ticker := time.NewTicker(p.cfg.HeartbeatInterval)
	defer ticker.Stop()

	p.mu.Lock()
	stop := p.stopChan
	p.mu.Unlock()
	if stop == nil {
		return
	}

	for {
		select {
		case <-ticker.C:
			p.heartbeatOnce()
		case <-stop:
			return
		}
	}
}

func (p *Provider) heartbeatOnce() {
	p.mu.Lock()
	if p.self != nil {
		p.self.Seq++
		p.state.setMember(toState(p.self))
		p.lastSeen[p.self.Id] = time.Now()
	}
	now := time.Now()
	changed := false
	for id, s := range p.state.Members {
		if p.self != nil && id == p.self.Id {
			continue
		}
		last, ok := p.lastSeen[id]
		if !ok {
			continue
		}
		elapsed := now.Sub(last)
		switch cluster.MemberStatus(s.Status) {
		case cluster.MemberAlive:
			if elapsed > p.cfg.HeartbeatTimeout {
				s.Status = int32(cluster.MemberSuspect)
				s.Seq++
				changed = true
				log.Info("[gossip] suspect: %s (%s)", s.Address, s.Id)
			}
		case cluster.MemberSuspect:
			if elapsed > p.cfg.DeadTimeout {
				s.Status = int32(cluster.MemberDead)
				s.Seq++
				changed = true
				log.Info("[gossip] dead: %s (%s)", s.Address, s.Id)
			}
		}
	}
	p.mu.Unlock()
	if changed {
		p.publish()
	}
}

// --- gossip Actor ---

type gossipActor struct {
	provider *Provider
}

func (a *gossipActor) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
	case *GossipRequest:
		if msg.ClusterName != a.provider.clusterName {
			return
		}
		if a.provider.merge(msg.State) {
			a.provider.publish()
		}
		ctx.Respond(&GossipResponse{State: a.provider.snapshot()})
	case *GossipResponse:
		if a.provider.merge(msg.State) {
			a.provider.publish()
		}
	}
}

// --- 编解码辅助 ---

func toState(m *cluster.Member) *memberGossipState {
	return &memberGossipState{
		Address: m.Address,
		Id:      m.Id,
		Kinds:   append([]string(nil), m.Kinds...),
		Status:  int32(m.Status),
		Seq:     m.Seq,
	}
}

func fromState(s *memberGossipState) *cluster.Member {
	return &cluster.Member{
		Address:  s.Address,
		Id:       s.Id,
		Kinds:    append([]string(nil), s.Kinds...),
		Status:   cluster.MemberStatus(s.Status),
		Seq:      s.Seq,
		LastSeen: time.Now(),
	}
}

// 编译期断言
var (
	_ cluster.Provider            = (*Provider)(nil)
	_ cluster.RegistrableProvider = (*Provider)(nil)
)
