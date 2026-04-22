package cluster

import (
	"testing"
	"time"

	"engine/actor"
)

func TestGossipStateMerge(t *testing.T) {
	state1 := NewGossipState()
	state1.Members["node1"] = &MemberGossipState{
		Address: "127.0.0.1:8001",
		Id:      "node1",
		Status:  MemberAlive,
		Seq:     5,
	}

	state2 := NewGossipState()
	state2.Members["node1"] = &MemberGossipState{
		Address: "127.0.0.1:8001",
		Id:      "node1",
		Status:  MemberAlive,
		Seq:     3, // 较低版本号
	}
	state2.Members["node2"] = &MemberGossipState{
		Address: "127.0.0.1:8002",
		Id:      "node2",
		Status:  MemberAlive,
		Seq:     1,
	}

	changed := state1.Merge(state2)

	if !changed {
		t.Error("Merge should report change (new node2)")
	}

	// node1 应保持 Seq=5（不被低版本覆盖）
	if state1.Members["node1"].Seq != 5 {
		t.Errorf("node1 Seq should be 5, got %d", state1.Members["node1"].Seq)
	}

	// node2 应被添加
	if _, ok := state1.Members["node2"]; !ok {
		t.Error("node2 should be added")
	}
}

func TestGossipStateClone(t *testing.T) {
	state := NewGossipState()
	state.Members["node1"] = &MemberGossipState{
		Address: "127.0.0.1:8001",
		Id:      "node1",
		Kinds:   []string{"Player", "Room"},
		Status:  MemberAlive,
		Seq:     1,
	}

	clone := state.Clone()

	// 修改原始不应影响克隆
	state.Members["node1"].Seq = 99

	if clone.Members["node1"].Seq != 1 {
		t.Error("Clone should be independent")
	}
}

func TestMemberSet(t *testing.T) {
	ms := NewMemberSet()

	m1 := &Member{Address: "127.0.0.1:8001", Id: "n1", Status: MemberAlive, Kinds: []string{"Player"}}
	m2 := &Member{Address: "127.0.0.1:8002", Id: "n2", Status: MemberAlive, Kinds: []string{"Room"}}
	m3 := &Member{Address: "127.0.0.1:8003", Id: "n3", Status: MemberDead, Kinds: []string{"Player"}}

	ms.Add(m1)
	ms.Add(m2)
	ms.Add(m3)

	if ms.Len() != 3 {
		t.Errorf("Expected 3 members, got %d", ms.Len())
	}

	alive := ms.GetAlive()
	if len(alive) != 2 {
		t.Errorf("Expected 2 alive, got %d", len(alive))
	}

	byKind := ms.GetByKind("Player")
	if len(byKind) != 1 { // n1 is alive+Player, n3 is dead
		t.Errorf("Expected 1 alive Player member, got %d", len(byKind))
	}

	ms.Remove("n2")
	if ms.Len() != 2 {
		t.Errorf("After remove, expected 2, got %d", ms.Len())
	}
}

func TestConsistentHash(t *testing.T) {
	ch := NewConsistentHash()

	members := []*Member{
		{Address: "127.0.0.1:8001", Id: "n1", Kinds: []string{"Player"}},
		{Address: "127.0.0.1:8002", Id: "n2", Kinds: []string{"Player"}},
		{Address: "127.0.0.1:8003", Id: "n3", Kinds: []string{"Player"}},
	}
	ch.UpdateMembers(members)

	// 相同 identity 应总是映射到同一节点
	m1 := ch.GetMember("player-123", "Player")
	m2 := ch.GetMember("player-123", "Player")
	if m1.Address != m2.Address {
		t.Error("Same identity should map to same member")
	}

	// 不同 identity 应分布到不同节点（统计意义上）
	distribution := make(map[string]int)
	for i := 0; i < 100; i++ {
		m := ch.GetMember(string(rune('A'+i)), "Player")
		distribution[m.Address]++
	}

	if len(distribution) < 2 {
		t.Error("Expected distribution across at least 2 nodes")
	}

	// 不存在的 Kind
	m := ch.GetMember("player-123", "NonExistent")
	if m != nil {
		t.Error("Should return nil for unknown kind")
	}
}

func TestMemberListTopologyEvent(t *testing.T) {
	system := actor.NewActorSystem()
	config := DefaultClusterConfig("test", "127.0.0.1:8001")
	config.Kinds = []string{"Player"}

	c := NewCluster(system, nil, config)

	// 监听拓扑事件
	joinedCount := 0
	system.EventStream.Subscribe(func(event interface{}) {
		if _, ok := event.(*MemberJoinedEvent); ok {
			joinedCount++
		}
	})

	// 添加成员
	c.memberList.UpdateMember(&MemberGossipState{
		Address: "127.0.0.1:8002",
		Id:      "node2",
		Kinds:   []string{"Player"},
		Status:  MemberAlive,
		Seq:     1,
	})

	time.Sleep(10 * time.Millisecond)

	if joinedCount != 1 {
		t.Errorf("Expected 1 join event, got %d", joinedCount)
	}

	// 重复添加相同版本不应触发事件
	c.memberList.UpdateMember(&MemberGossipState{
		Address: "127.0.0.1:8002",
		Id:      "node2",
		Kinds:   []string{"Player"},
		Status:  MemberAlive,
		Seq:     1,
	})

	time.Sleep(10 * time.Millisecond)

	if joinedCount != 1 {
		t.Errorf("Same seq should not trigger event, got %d", joinedCount)
	}
}

func TestMemberHasKind(t *testing.T) {
	m := &Member{Kinds: []string{"Player", "Room"}}

	if !m.HasKind("Player") {
		t.Error("Should have Player kind")
	}
	if !m.HasKind("Room") {
		t.Error("Should have Room kind")
	}
	if m.HasKind("NPC") {
		t.Error("Should not have NPC kind")
	}
}
