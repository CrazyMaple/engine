package cluster

import (
	"sync"
	"time"

	"engine/actor"
	"engine/log"
)

// MemberList 集群成员列表管理
//
// 上游通过 Provider.onChange 推送"当前活跃成员全量快照"；MemberList
// 负责在 ApplySnapshot / UpdateMember 中做版本去重、状态对比，并发布
// MemberJoined / MemberLeft / MemberDead / ClusterTopologyEvent 事件。
type MemberList struct {
	cluster     *Cluster
	members     *MemberSet
	eventStream *actor.EventStream
	mu          sync.Mutex
}

// NewMemberList 创建成员列表
func NewMemberList(cluster *Cluster) *MemberList {
	return &MemberList{
		cluster:     cluster,
		members:     NewMemberSet(),
		eventStream: cluster.system.EventStream,
	}
}

// UpdateMember 用一份成员状态更新本地视图，返回是否产生变更。
// state 视为该成员的最新已知版本：仅当本地不存在或本地版本号更低时才接纳。
func (ml *MemberList) UpdateMember(state *Member) bool {
	if state == nil {
		return false
	}

	ml.mu.Lock()
	defer ml.mu.Unlock()

	existing, exists := ml.members.Get(state.Id)
	if !exists {
		if state.Status == MemberDead || state.Status == MemberLeft {
			return false
		}

		member := &Member{
			Address:  state.Address,
			Id:       state.Id,
			Kinds:    append([]string(nil), state.Kinds...),
			Status:   state.Status,
			Seq:      state.Seq,
			LastSeen: time.Now(),
		}
		ml.members.Add(member)

		log.Info("Member joined: %s (%s)", member.Address, member.Id)
		ml.eventStream.Publish(&MemberJoinedEvent{Member: member.Clone()})
		ml.publishTopology()
		return true
	}

	if state.Seq <= existing.Seq {
		return false
	}

	oldStatus := existing.Status
	existing.Status = state.Status
	existing.Seq = state.Seq
	existing.LastSeen = time.Now()
	if len(state.Kinds) > 0 {
		existing.Kinds = append([]string(nil), state.Kinds...)
	}

	if oldStatus != state.Status {
		switch state.Status {
		case MemberSuspect:
			log.Info("Member suspect: %s (%s)", existing.Address, existing.Id)
			ml.eventStream.Publish(&MemberSuspectEvent{Member: existing.Clone()})
		case MemberDead:
			log.Info("Member dead: %s (%s)", existing.Address, existing.Id)
			ml.eventStream.Publish(&MemberDeadEvent{Member: existing.Clone()})
			ml.publishTopology()
		case MemberLeft:
			log.Info("Member left: %s (%s)", existing.Address, existing.Id)
			ml.eventStream.Publish(&MemberLeftEvent{Member: existing.Clone()})
			ml.publishTopology()
		case MemberAlive:
			if oldStatus == MemberSuspect {
				log.Info("Member alive again: %s (%s)", existing.Address, existing.Id)
			}
		}
	}

	return true
}

// MarkDead 将指定成员标记为 Dead
func (ml *MemberList) MarkDead(id string) {
	ml.mu.Lock()
	defer ml.mu.Unlock()

	member, ok := ml.members.Get(id)
	if !ok || member.Status == MemberDead || member.Status == MemberLeft {
		return
	}

	member.Status = MemberDead
	member.Seq++

	log.Info("Member dead: %s (%s)", member.Address, member.Id)
	ml.eventStream.Publish(&MemberDeadEvent{Member: member.Clone()})
	ml.publishTopology()
}

// MarkLeft 将指定成员标记为 Left（主动离开或被 Provider 移除）
func (ml *MemberList) MarkLeft(id string) {
	ml.mu.Lock()
	defer ml.mu.Unlock()

	member, ok := ml.members.Get(id)
	if !ok || member.Status == MemberLeft {
		return
	}

	member.Status = MemberLeft
	member.Seq++

	log.Info("Member left: %s (%s)", member.Address, member.Id)
	ml.eventStream.Publish(&MemberLeftEvent{Member: member.Clone()})
	ml.publishTopology()
}

// ApplySnapshot 用一份全量成员快照同步本地视图：
//   - 新成员或更高版本 -> 走 UpdateMember，发布 Joined / 状态变化事件；
//   - 既有成员未在快照中出现 -> 标记为 Left，发布 MemberLeftEvent；
//   - 至少一项变化时同步发布一次 ClusterTopologyEvent。
//
// self 不会被快照外的"消失"逻辑影响——本节点状态由 Cluster 自己维护。
func (ml *MemberList) ApplySnapshot(snapshot []*Member, selfID string) {
	for _, m := range snapshot {
		if m == nil {
			continue
		}
		ml.UpdateMember(m)
	}

	keep := make(map[string]struct{}, len(snapshot))
	for _, m := range snapshot {
		if m != nil {
			keep[m.Id] = struct{}{}
		}
	}

	for _, existing := range ml.members.GetAll() {
		if existing.Id == selfID {
			continue
		}
		if _, ok := keep[existing.Id]; ok {
			continue
		}
		if existing.Status == MemberLeft || existing.Status == MemberDead {
			continue
		}
		ml.MarkLeft(existing.Id)
	}
}

// GetMembers 获取所有存活成员
func (ml *MemberList) GetMembers() []*Member {
	return ml.members.GetAlive()
}

// GetMembersByKind 获取支持指定 Kind 的存活成员
func (ml *MemberList) GetMembersByKind(kind string) []*Member {
	return ml.members.GetByKind(kind)
}

// GetAllMembers 获取所有成员（含非存活）
func (ml *MemberList) GetAllMembers() []*Member {
	return ml.members.GetAll()
}

// publishTopology 发布拓扑变更事件
func (ml *MemberList) publishTopology() {
	alive := ml.members.GetAlive()
	ml.eventStream.Publish(&ClusterTopologyEvent{
		Members: alive,
	})
}
