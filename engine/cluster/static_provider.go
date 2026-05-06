package cluster

import (
	"sync"
	"time"
)

// StaticProvider 把一组静态种子地址当成唯一可见的成员快照。
//
// 边界（见 ADR v1.13-001 §2.4）：仅承担"一次性 onChange(seeds)"，不承担
// 心跳探活、成员故障检测、gossip 传播——这些是动态 provider 的职责。
type StaticProvider struct {
	seeds []string

	mu      sync.Mutex
	started bool
}

// NewStaticProvider 构造一个仅承载静态种子节点的 Provider。
// 种子地址会在 Start 时一次性映射成 Member 快照传给 onChange。
func NewStaticProvider(seeds ...string) *StaticProvider {
	out := make([]string, 0, len(seeds))
	for _, s := range seeds {
		if s != "" {
			out = append(out, s)
		}
	}
	return &StaticProvider{seeds: out}
}

// Start 实现 Provider。把 self + seeds 合并去重为成员快照后回调一次。
func (p *StaticProvider) Start(_ string, self *Member, onChange func([]*Member)) error {
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return nil
	}
	p.started = true
	p.mu.Unlock()

	if onChange == nil {
		return nil
	}

	seen := make(map[string]struct{}, len(p.seeds)+1)
	members := make([]*Member, 0, len(p.seeds)+1)

	if self != nil {
		members = append(members, self)
		if self.Address != "" {
			seen[self.Address] = struct{}{}
		}
	}

	now := time.Now()
	for _, addr := range p.seeds {
		if _, dup := seen[addr]; dup {
			continue
		}
		seen[addr] = struct{}{}
		members = append(members, &Member{
			Address:  addr,
			Id:       addr,
			Status:   MemberAlive,
			Seq:      1,
			LastSeen: now,
		})
	}

	onChange(members)
	return nil
}

// Stop 实现 Provider；幂等。
func (p *StaticProvider) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.started = false
	return nil
}
