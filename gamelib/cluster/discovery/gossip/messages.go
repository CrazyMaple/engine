package gossip

// gossipState SWIM 风格的 CRDT：按 Seq 最大值合并。
type gossipState struct {
	Members map[string]*memberGossipState
}

// memberGossipState 单个成员的 gossip 状态。
type memberGossipState struct {
	Address string
	Id      string
	Kinds   []string
	Status  int32
	Seq     uint64
}

// GossipRequest gossip 请求。
type GossipRequest struct {
	ClusterName string
	State       *gossipState
}

// GossipResponse gossip 响应。
type GossipResponse struct {
	State *gossipState
}

func newGossipState() *gossipState {
	return &gossipState{Members: make(map[string]*memberGossipState)}
}

func (gs *gossipState) clone() *gossipState {
	out := newGossipState()
	for id, s := range gs.Members {
		kinds := append([]string(nil), s.Kinds...)
		out.Members[id] = &memberGossipState{
			Address: s.Address, Id: s.Id, Kinds: kinds,
			Status: s.Status, Seq: s.Seq,
		}
	}
	return out
}

// merge 把 remote 状态合并到本地，返回是否有变更（CRDT：按 Seq 最大值）。
func (gs *gossipState) merge(remote *gossipState) bool {
	changed := false
	for id, rs := range remote.Members {
		ls, ok := gs.Members[id]
		if !ok || rs.Seq > ls.Seq {
			gs.Members[id] = rs
			changed = true
		}
	}
	return changed
}

func (gs *gossipState) setMember(m *memberGossipState) {
	gs.Members[m.Id] = m
}
