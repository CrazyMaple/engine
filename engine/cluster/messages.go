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
