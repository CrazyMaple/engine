package cluster

import (
	"fmt"
	"sync"
	"time"
)

// MemberStatus 节点状态
type MemberStatus int

const (
	MemberAlive   MemberStatus = iota // 正常运行
	MemberSuspect                     // 疑似故障（错过心跳）
	MemberDead                        // 已确认死亡
	MemberLeft                        // 主动离开
)

func (s MemberStatus) String() string {
	switch s {
	case MemberAlive:
		return "Alive"
	case MemberSuspect:
		return "Suspect"
	case MemberDead:
		return "Dead"
	case MemberLeft:
		return "Left"
	default:
		return fmt.Sprintf("Unknown(%d)", int(s))
	}
}

// Member 集群成员
type Member struct {
	// Address 节点网络地址 "host:port"
	Address string

	// Id 节点唯一标识（启动时生成）
	Id string

	// Kinds 该节点支持的 Actor Kind 列表
	Kinds []string

	// Status 节点当前状态
	Status MemberStatus

	// Seq 状态版本号（Lamport 风格），每次状态变更递增
	Seq uint64

	// LastSeen 最后一次观察到活跃的时间（本地跟踪，不 gossip）
	LastSeen time.Time
}

// HasKind 检查节点是否支持指定 Kind
func (m *Member) HasKind(kind string) bool {
	for _, k := range m.Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// Clone 复制 Member
func (m *Member) Clone() *Member {
	kinds := make([]string, len(m.Kinds))
	copy(kinds, m.Kinds)
	return &Member{
		Address:  m.Address,
		Id:       m.Id,
		Kinds:    kinds,
		Status:   m.Status,
		Seq:      m.Seq,
		LastSeen: m.LastSeen,
	}
}

// MemberSet 成员集合
type MemberSet struct {
	members map[string]*Member // 以 Id 为 key
	mu      sync.RWMutex
}

// NewMemberSet 创建成员集合
func NewMemberSet() *MemberSet {
	return &MemberSet{
		members: make(map[string]*Member),
	}
}

// Add 添加或更新成员
func (ms *MemberSet) Add(member *Member) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.members[member.Id] = member
}

// Remove 移除成员
func (ms *MemberSet) Remove(id string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	delete(ms.members, id)
}

// Get 获取成员
func (ms *MemberSet) Get(id string) (*Member, bool) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	m, ok := ms.members[id]
	return m, ok
}

// GetByAddress 根据地址获取成员
func (ms *MemberSet) GetByAddress(address string) (*Member, bool) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	for _, m := range ms.members {
		if m.Address == address {
			return m, true
		}
	}
	return nil, false
}

// GetAll 获取所有成员
func (ms *MemberSet) GetAll() []*Member {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	result := make([]*Member, 0, len(ms.members))
	for _, m := range ms.members {
		result = append(result, m)
	}
	return result
}

// GetAlive 获取所有存活成员
func (ms *MemberSet) GetAlive() []*Member {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	result := make([]*Member, 0, len(ms.members))
	for _, m := range ms.members {
		if m.Status == MemberAlive {
			result = append(result, m)
		}
	}
	return result
}

// GetByKind 获取支持指定 Kind 的存活成员
func (ms *MemberSet) GetByKind(kind string) []*Member {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	result := make([]*Member, 0)
	for _, m := range ms.members {
		if m.Status == MemberAlive && m.HasKind(kind) {
			result = append(result, m)
		}
	}
	return result
}

// Len 返回成员数量
func (ms *MemberSet) Len() int {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return len(ms.members)
}
