package cluster

import (
	"sync"
	"time"

	"engine/actor"
	"engine/log"
)

// MemberList 集群成员列表管理
type MemberList struct {
	cluster     *Cluster
	members     *MemberSet
	eventStream *actor.EventStream
	mu          sync.RWMutex
}

// NewMemberList 创建成员列表
func NewMemberList(cluster *Cluster) *MemberList {
	return &MemberList{
		cluster:     cluster,
		members:     NewMemberSet(),
		eventStream: cluster.system.EventStream,
	}
}

// UpdateMember 更新成员状态，返回是否有变更
func (ml *MemberList) UpdateMember(state *MemberGossipState) bool {
	ml.mu.Lock()
	defer ml.mu.Unlock()

	existing, exists := ml.members.Get(state.Id)
	if !exists {
		// 新成员
		if state.Status == MemberDead || state.Status == MemberLeft {
			return false // 已离开的不添加
		}

		member := &Member{
			Address:  state.Address,
			Id:       state.Id,
			Kinds:    state.Kinds,
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

	// 只处理更高版本
	if state.Seq <= existing.Seq {
		return false
	}

	oldStatus := existing.Status
	existing.Status = state.Status
	existing.Seq = state.Seq
	existing.LastSeen = time.Now()

	// 状态变更通知
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

// MarkSuspect 将指定成员标记为 Suspect
func (ml *MemberList) MarkSuspect(id string) {
	ml.mu.Lock()
	defer ml.mu.Unlock()

	member, ok := ml.members.Get(id)
	if !ok || member.Status != MemberAlive {
		return
	}

	member.Status = MemberSuspect
	member.Seq++

	log.Info("Member suspect: %s (%s)", member.Address, member.Id)
	ml.eventStream.Publish(&MemberSuspectEvent{Member: member.Clone()})
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

// RefreshLastSeen 刷新成员的最后活跃时间
func (ml *MemberList) RefreshLastSeen(id string) {
	member, ok := ml.members.Get(id)
	if ok {
		member.LastSeen = time.Now()
	}
}

// publishTopology 发布拓扑变更事件
func (ml *MemberList) publishTopology() {
	alive := ml.members.GetAlive()
	ml.eventStream.Publish(&ClusterTopologyEvent{
		Members: alive,
	})
}
