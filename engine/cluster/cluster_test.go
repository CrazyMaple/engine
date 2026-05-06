package cluster_test

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"engine/actor"
	"engine/cluster"
	"engine/cluster/testkit"
)

func newTestCluster(t *testing.T, addr string, kinds []string, provider cluster.Provider) *cluster.Cluster {
	t.Helper()
	system := actor.NewActorSystem()
	cfg := cluster.DefaultClusterConfig("cluster-test", addr).WithKinds(kinds...)
	cfg.Provider = provider
	c := cluster.NewCluster(system, nil, cfg)
	if err := c.Start(); err != nil {
		t.Fatalf("cluster start: %v", err)
	}
	t.Cleanup(c.Stop)
	return c
}

// TestClusterStartStop 验证 Start/Stop 与 Provider 生命周期。
func TestClusterStartStop(t *testing.T) {
	fp := testkit.NewFakeProvider()
	c := newTestCluster(t, "127.0.0.1:9100", []string{"player"}, fp)

	if !fp.Started() {
		t.Fatal("provider should be started after Cluster.Start")
	}
	if fp.Self() == nil || fp.Self().Address != "127.0.0.1:9100" {
		t.Fatalf("provider should receive self, got %+v", fp.Self())
	}

	c.Stop()
	if fp.Started() {
		t.Fatal("provider should be stopped after Cluster.Stop")
	}

	c.Stop() // 二次 Stop 必须幂等
}

// TestClusterStartFailFast 验证 Provider 启动失败时 Cluster.Start 必须返回错误，
// 不允许 fallback 到旧 seed gossip 路径。
func TestClusterStartFailFast(t *testing.T) {
	system := actor.NewActorSystem()
	cfg := cluster.DefaultClusterConfig("fail-fast", "127.0.0.1:9101").WithKinds("player")
	cfg.Provider = errProvider{}
	c := cluster.NewCluster(system, nil, cfg)

	if err := c.Start(); err == nil {
		t.Fatal("expected Start to fail when Provider.Start returns error")
	}
	c.Stop() // 启动失败后 Stop 仍须幂等
}

// TestClusterRegisterFailFast 验证 RegistrableProvider.Register 失败时
// Cluster.Start 必须返回错误，并把 Provider 回滚（Stop 被调用）。
func TestClusterRegisterFailFast(t *testing.T) {
	system := actor.NewActorSystem()
	cfg := cluster.DefaultClusterConfig("reg-fail", "127.0.0.1:9105").WithKinds("player")

	rp := &registrableProviderStub{registerErr: errors.New("denied")}
	cfg.Provider = rp
	c := cluster.NewCluster(system, nil, cfg)

	if err := c.Start(); err == nil {
		t.Fatal("expected Start to fail when Register returns error")
	}
	if !rp.startCalled {
		t.Fatal("Provider.Start should be called before Register")
	}
	if !rp.registerCalled {
		t.Fatal("Register should be called after Start")
	}
	if !rp.stopCalled {
		t.Fatal("Provider.Stop should be invoked to roll back failed registration")
	}

	c.Stop() // 失败后 Stop 仍须幂等
}

// TestClusterMemberJoinLeave 验证成员上下线事件链路。
func TestClusterMemberJoinLeave(t *testing.T) {
	fp := testkit.NewFakeProvider()
	c := newTestCluster(t, "127.0.0.1:9110", []string{"player"}, fp)

	var (
		joins atomic.Int32
		lefts atomic.Int32
	)
	c.System().EventStream.Subscribe(func(event interface{}) {
		switch event.(type) {
		case *cluster.MemberJoinedEvent:
			joins.Add(1)
		case *cluster.MemberLeftEvent:
			lefts.Add(1)
		}
	})

	peer := &cluster.Member{
		Address: "127.0.0.1:9111",
		Id:      "peer-1",
		Kinds:   []string{"player"},
		Status:  cluster.MemberAlive,
		Seq:     1,
	}

	fp.PushMembers([]*cluster.Member{peer})
	waitFor(t, func() bool { return joins.Load() == 1 })

	if got := len(c.Members()); got != 2 {
		t.Fatalf("expect 2 members after join, got %d", got)
	}
	if c.GetMemberForIdentity("any", "player") == nil {
		t.Fatal("hash ring should have at least one Player member")
	}

	// 推送一次空快照 → peer 应被标 Left
	fp.PushMembers([]*cluster.Member{})
	waitFor(t, func() bool { return lefts.Load() == 1 })

	for _, m := range c.Members() {
		if m.Id == peer.Id {
			t.Fatalf("peer-1 should be removed from alive members, got %+v", c.Members())
		}
	}
}

// TestClusterTopologyEvent 验证每次成员变更都会发布 ClusterTopologyEvent。
func TestClusterTopologyEvent(t *testing.T) {
	fp := testkit.NewFakeProvider()
	c := newTestCluster(t, "127.0.0.1:9120", []string{"room"}, fp)

	var topo atomic.Int32
	c.System().EventStream.Subscribe(func(event interface{}) {
		if _, ok := event.(*cluster.ClusterTopologyEvent); ok {
			topo.Add(1)
		}
	})

	fp.PushMembers([]*cluster.Member{
		{Address: "127.0.0.1:9121", Id: "node-2", Kinds: []string{"room"}, Status: cluster.MemberAlive, Seq: 1},
	})
	waitFor(t, func() bool { return topo.Load() >= 1 })

	fp.PushMembers([]*cluster.Member{})
	waitFor(t, func() bool { return topo.Load() >= 2 })
}

// TestClusterConsistentHashStability 验证 GetMemberForIdentity 是确定性映射。
func TestClusterConsistentHashStability(t *testing.T) {
	fp := testkit.NewFakeProvider()
	c := newTestCluster(t, "127.0.0.1:9130", []string{"player"}, fp)

	fp.PushMembers([]*cluster.Member{
		{Address: "127.0.0.1:9131", Id: "n1", Kinds: []string{"player"}, Status: cluster.MemberAlive, Seq: 1},
		{Address: "127.0.0.1:9132", Id: "n2", Kinds: []string{"player"}, Status: cluster.MemberAlive, Seq: 1},
		{Address: "127.0.0.1:9133", Id: "n3", Kinds: []string{"player"}, Status: cluster.MemberAlive, Seq: 1},
	})
	waitFor(t, func() bool { return len(c.Members()) >= 4 })

	first := c.GetMemberForIdentity("alice", "player")
	if first == nil {
		t.Fatal("expected hash ring to resolve 'alice'")
	}
	for i := 0; i < 50; i++ {
		got := c.GetMemberForIdentity("alice", "player")
		if got == nil || got.Address != first.Address {
			t.Fatalf("identity mapping not stable: first=%v got=%v", first, got)
		}
	}

	if c.GetMemberForIdentity("alice", "missing-kind") != nil {
		t.Fatal("unknown kind should resolve to nil member")
	}
}

// TestClusterSingletonEndToEnd 通过 ClusterSingleton + FakeProvider 推送的成员快照
// 验证单例在拓扑变更时仍可被检索。
func TestClusterSingletonEndToEnd(t *testing.T) {
	fp := testkit.NewFakeProvider()
	c := newTestCluster(t, "127.0.0.1:9140", []string{"global"}, fp)

	cs := cluster.NewClusterSingleton(c)
	cs.Start()
	t.Cleanup(cs.Stop)

	if err := cs.Register(cluster.SingletonConfig{
		Kind:  "global",
		Props: actor.PropsFromProducer(func() actor.Actor { return &noopActor{} }),
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	pid, err := cs.Get("global")
	if err != nil {
		t.Fatalf("get singleton: %v", err)
	}
	if pid == nil {
		t.Fatal("singleton PID should be non-nil on single-node cluster")
	}

	fp.PushMembers([]*cluster.Member{
		{Address: "127.0.0.1:9141", Id: "remote", Kinds: []string{"global"}, Status: cluster.MemberAlive, Seq: 1},
	})
	waitFor(t, func() bool { return len(c.Members()) >= 2 })

	if _, err := cs.Get("global"); err != nil {
		t.Fatalf("singleton get after topology change: %v", err)
	}
}

// --- helpers ---

type noopActor struct{}

func (n *noopActor) Receive(actor.Context) {}

type errProvider struct{}

func (errProvider) Start(string, *cluster.Member, func([]*cluster.Member)) error {
	return errors.New("fake start failure")
}
func (errProvider) Stop() error { return nil }

// registrableProviderStub 实现 cluster.RegistrableProvider，可在 Register 阶段注入失败。
type registrableProviderStub struct {
	registerErr error

	startCalled    bool
	stopCalled     bool
	registerCalled bool
}

func (r *registrableProviderStub) Start(string, *cluster.Member, func([]*cluster.Member)) error {
	r.startCalled = true
	return nil
}
func (r *registrableProviderStub) Stop() error {
	r.stopCalled = true
	return nil
}
func (r *registrableProviderStub) Register(*cluster.Member) error {
	r.registerCalled = true
	return r.registerErr
}
func (r *registrableProviderStub) Deregister(*cluster.Member) error { return nil }

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met in time")
}
