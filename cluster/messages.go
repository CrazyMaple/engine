package cluster

// MemberJoinedEvent 成员加入集群事件
type MemberJoinedEvent struct {
	Member *Member
}

// MemberLeftEvent 成员离开集群事件
type MemberLeftEvent struct {
	Member *Member
}

// MemberSuspectEvent 成员疑似故障事件
type MemberSuspectEvent struct {
	Member *Member
}

// MemberDeadEvent 成员确认死亡事件
type MemberDeadEvent struct {
	Member *Member
}

// ClusterTopologyEvent 集群拓扑变更事件
type ClusterTopologyEvent struct {
	Members []*Member // 当前全部存活成员
	Joined  []*Member // 新加入的成员
	Left    []*Member // 离开/死亡的成员
}

// GossipRequest Gossip 请求消息
type GossipRequest struct {
	ClusterName string
	State       *GossipState
}

// GossipResponse Gossip 响应消息
type GossipResponse struct {
	State *GossipState
}

// GossipState Gossip 状态（CRDT：按 Seq 最大值合并）
type GossipState struct {
	Members map[string]*MemberGossipState // Id -> 状态
}

// MemberGossipState 单个成员的 Gossip 状态
type MemberGossipState struct {
	Address string
	Id      string
	Kinds   []string
	Status  MemberStatus
	Seq     uint64
}

// NewGossipState 创建空 Gossip 状态
func NewGossipState() *GossipState {
	return &GossipState{
		Members: make(map[string]*MemberGossipState),
	}
}

// SetMember 设置成员状态
func (gs *GossipState) SetMember(member *Member) {
	gs.Members[member.Id] = &MemberGossipState{
		Address: member.Address,
		Id:      member.Id,
		Kinds:   member.Kinds,
		Status:  member.Status,
		Seq:     member.Seq,
	}
}

// Merge 合并远程 Gossip 状态，返回是否有变更
func (gs *GossipState) Merge(remote *GossipState) bool {
	changed := false
	for id, remoteState := range remote.Members {
		localState, exists := gs.Members[id]
		if !exists || remoteState.Seq > localState.Seq {
			gs.Members[id] = remoteState
			changed = true
		}
	}
	return changed
}

// Clone 复制 Gossip 状态
func (gs *GossipState) Clone() *GossipState {
	clone := NewGossipState()
	for id, state := range gs.Members {
		kinds := make([]string, len(state.Kinds))
		copy(kinds, state.Kinds)
		clone.Members[id] = &MemberGossipState{
			Address: state.Address,
			Id:      state.Id,
			Kinds:   kinds,
			Status:  state.Status,
			Seq:     state.Seq,
		}
	}
	return clone
}
