package cluster

import (
	"hash/fnv"
	"sync"
)

// ConsistentHash 一致性哈希（Rendezvous Hashing / HRW）
// 使用 Rendezvous Hashing 算法将 identity 映射到节点
// 优点：简单、均匀分布、节点增减时影响范围最小
type ConsistentHash struct {
	members []*Member
	mu      sync.RWMutex
}

// NewConsistentHash 创建一致性哈希
func NewConsistentHash() *ConsistentHash {
	return &ConsistentHash{}
}

// UpdateMembers 更新成员列表
func (ch *ConsistentHash) UpdateMembers(members []*Member) {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	ch.members = members
}

// GetMember 根据 identity 和 kind 获取归属节点
// 使用 Rendezvous Hashing：对每个候选节点计算 hash(identity + address)，选最大值
func (ch *ConsistentHash) GetMember(identity string, kind string) *Member {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	// 筛选支持该 Kind 的成员
	var candidates []*Member
	for _, m := range ch.members {
		if m.HasKind(kind) {
			candidates = append(candidates, m)
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	if len(candidates) == 1 {
		return candidates[0]
	}

	// Rendezvous Hashing
	var bestMember *Member
	var bestHash uint32

	for _, m := range candidates {
		h := hashCombine(identity, m.Address)
		if bestMember == nil || h > bestHash {
			bestHash = h
			bestMember = m
		}
	}

	return bestMember
}

// GetMembers 获取当前成员列表
func (ch *ConsistentHash) GetMembers() []*Member {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	result := make([]*Member, len(ch.members))
	copy(result, ch.members)
	return result
}

// hashCombine 组合 identity 和 address 计算 FNV-1a 哈希
func hashCombine(identity, address string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(identity))
	h.Write([]byte(":"))
	h.Write([]byte(address))
	return h.Sum32()
}
