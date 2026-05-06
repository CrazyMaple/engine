package gossip

import (
	"sync/atomic"
	"testing"
	"time"

	"engine/actor"
	"engine/cluster"
)

func TestGossipState_MergeKeepsHighestSeq(t *testing.T) {
	a := newGossipState()
	a.setMember(&memberGossipState{Id: "n1", Seq: 5, Address: "a", Status: int32(cluster.MemberAlive)})

	b := newGossipState()
	b.setMember(&memberGossipState{Id: "n1", Seq: 3, Address: "a", Status: int32(cluster.MemberAlive)})
	b.setMember(&memberGossipState{Id: "n2", Seq: 1, Address: "b", Status: int32(cluster.MemberAlive)})

	if !a.merge(b) {
		t.Fatal("merge should report change (n2 added)")
	}
	if a.Members["n1"].Seq != 5 {
		t.Errorf("n1 should keep Seq=5, got %d", a.Members["n1"].Seq)
	}
	if _, ok := a.Members["n2"]; !ok {
		t.Error("n2 should be added")
	}
}

func TestGossipState_Clone_Independent(t *testing.T) {
	a := newGossipState()
	a.setMember(&memberGossipState{Id: "n1", Seq: 1, Kinds: []string{"player"}})
	c := a.clone()
	a.Members["n1"].Seq = 99
	if c.Members["n1"].Seq != 1 {
		t.Error("clone must be independent of original")
	}
}

// TestGossipProvider_StartStop 验证 Start/Stop 生命周期 + 至少一次 onChange。
func TestGossipProvider_StartStop(t *testing.T) {
	system := actor.NewActorSystem()

	cfg := DefaultConfig()
	p := New(system, nil, cfg)

	var calls atomic.Int32
	self := &cluster.Member{
		Address: "127.0.0.1:9001",
		Id:      "n1",
		Kinds:   []string{"player"},
		Status:  cluster.MemberAlive,
		Seq:     1,
	}

	if err := p.Start("c1", self, func([]*cluster.Member) {
		calls.Add(1)
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = p.Stop() })

	if calls.Load() == 0 {
		t.Fatal("expected at least one onChange after Start")
	}

	// 第二次 Start 幂等
	if err := p.Start("c1", self, func([]*cluster.Member) {}); err != nil {
		t.Fatalf("Start again: %v", err)
	}

	if err := p.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// 二次 Stop 幂等
	if err := p.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

// TestGossipProvider_MergePublishes 验证收到远端 state 后 onChange 被触发。
func TestGossipProvider_MergePublishes(t *testing.T) {
	system := actor.NewActorSystem()

	p := New(system, nil, DefaultConfig())
	var calls atomic.Int32
	if err := p.Start("c1", &cluster.Member{
		Address: "127.0.0.1:9101", Id: "n1", Kinds: []string{"player"},
		Status: cluster.MemberAlive, Seq: 1,
	}, func([]*cluster.Member) { calls.Add(1) }); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = p.Stop() })

	before := calls.Load()

	remote := newGossipState()
	remote.setMember(&memberGossipState{
		Id: "n2", Address: "127.0.0.1:9102",
		Kinds: []string{"player"}, Status: int32(cluster.MemberAlive), Seq: 1,
	})
	if !p.merge(remote) {
		t.Fatal("merge should report change for new member")
	}
	p.publish()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if calls.Load() > before {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() <= before {
		t.Fatalf("expected onChange after merge, got calls=%d", calls.Load())
	}

	active := p.activeMembers()
	if len(active) != 2 {
		t.Fatalf("expected 2 active members, got %d", len(active))
	}
}

// TestGossipProvider_RegisterDeregister 验证扩展接口的 setMember 行为。
func TestGossipProvider_RegisterDeregister(t *testing.T) {
	system := actor.NewActorSystem()

	p := New(system, nil, DefaultConfig())
	if err := p.Start("c1", &cluster.Member{
		Address: "127.0.0.1:9201", Id: "n1",
		Kinds: []string{"player"}, Status: cluster.MemberAlive, Seq: 1,
	}, func([]*cluster.Member) {}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = p.Stop() })

	other := &cluster.Member{
		Address: "127.0.0.1:9202", Id: "n2",
		Kinds: []string{"player"}, Status: cluster.MemberAlive, Seq: 1,
	}
	if err := p.Register(other); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got := len(p.activeMembers()); got != 2 {
		t.Errorf("expected 2 active after Register, got %d", got)
	}

	if err := p.Deregister(other); err != nil {
		t.Fatalf("Deregister: %v", err)
	}
	for _, m := range p.activeMembers() {
		if m.Id == other.Id {
			t.Fatalf("n2 should be Left after Deregister, got %+v", m)
		}
	}
}
