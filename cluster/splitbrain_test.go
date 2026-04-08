package cluster

import (
	"sync/atomic"
	"testing"
	"time"

	"engine/actor"
)

func makeTestMembers(n int) []*Member {
	members := make([]*Member, n)
	for i := 0; i < n; i++ {
		members[i] = &Member{
			Address:  "host" + string(rune('A'+i)) + ":8080",
			Id:       "node-" + string(rune('a'+i)),
			Kinds:    []string{"test"},
			Status:   MemberAlive,
			Seq:      1,
			LastSeen: time.Now(),
		}
	}
	return members
}

// newTestCluster 创建测试用的最小集群（不启动 Remote/Gossip）
func newTestCluster(selfAddr, selfID string) *Cluster {
	system := actor.NewActorSystem()
	self := &Member{
		Address:  selfAddr,
		Id:       selfID,
		Kinds:    []string{"test"},
		Status:   MemberAlive,
		Seq:      1,
		LastSeen: time.Now(),
	}
	c := &Cluster{
		system:   system,
		config:   DefaultClusterConfig("test", selfAddr),
		hashRing: NewConsistentHash(),
		self:     self,
	}
	c.memberList = NewMemberList(c)
	return c
}

// === Resolver Tests ===

func TestKeepOldestResolverReachable(t *testing.T) {
	members := makeTestMembers(5)

	resolver := &KeepOldestResolver{}
	ctx := ResolverContext{
		Self:      members[0],
		Reachable: members[:3], // node-a, node-b, node-c（含最老 node-a）
		AllKnown:  members,
	}

	if resolver.Resolve(ctx) != DecisionKeepRunning {
		t.Fatal("should keep running when oldest (node-a) is reachable")
	}
}

func TestKeepOldestResolverUnreachable(t *testing.T) {
	members := makeTestMembers(5)
	resolver := &KeepOldestResolver{}

	ctx := ResolverContext{
		Self:      members[3],
		Reachable: members[3:], // node-d, node-e（不含 node-a）
		AllKnown:  members,
	}

	if resolver.Resolve(ctx) != DecisionShutdown {
		t.Fatal("should shutdown when oldest (node-a) is not reachable")
	}
}

func TestKeepMajorityResolverMajority(t *testing.T) {
	members := makeTestMembers(5)
	resolver := &KeepMajorityResolver{}

	ctx := ResolverContext{
		Self:      members[0],
		Reachable: members[:3], // 3 of 5 = majority
		AllKnown:  members,
	}

	if resolver.Resolve(ctx) != DecisionKeepRunning {
		t.Fatal("should keep running with majority (3/5)")
	}
}

func TestKeepMajorityResolverMinority(t *testing.T) {
	members := makeTestMembers(5)
	resolver := &KeepMajorityResolver{}

	ctx := ResolverContext{
		Self:      members[3],
		Reachable: members[3:], // 2 of 5 = minority
		AllKnown:  members,
	}

	if resolver.Resolve(ctx) != DecisionShutdown {
		t.Fatal("should shutdown with minority (2/5)")
	}
}

func TestKeepMajorityResolverTieBreaker(t *testing.T) {
	members := makeTestMembers(4)
	resolver := &KeepMajorityResolver{}

	// 2 of 4：平局
	ctx1 := ResolverContext{
		Self:      members[0],
		Reachable: members[:2], // node-a, node-b（含最小 ID）
		AllKnown:  members,
	}
	if resolver.Resolve(ctx1) != DecisionKeepRunning {
		t.Fatal("tie-break: partition with smallest ID should keep running")
	}

	ctx2 := ResolverContext{
		Self:      members[2],
		Reachable: members[2:], // node-c, node-d
		AllKnown:  members,
	}
	if resolver.Resolve(ctx2) != DecisionShutdown {
		t.Fatal("tie-break: partition without smallest ID should shutdown")
	}
}

func TestShutdownAllResolver(t *testing.T) {
	resolver := &ShutdownAllResolver{}
	ctx := ResolverContext{
		Reachable: makeTestMembers(3),
		AllKnown:  makeTestMembers(5),
	}

	if resolver.Resolve(ctx) != DecisionShutdown {
		t.Fatal("ShutdownAll should always return Shutdown")
	}
}

func TestKeepOldestResolverEmpty(t *testing.T) {
	resolver := &KeepOldestResolver{}
	ctx := ResolverContext{AllKnown: []*Member{}, Reachable: []*Member{}}

	if resolver.Resolve(ctx) != DecisionShutdown {
		t.Fatal("empty should shutdown")
	}
}

func TestKeepMajorityResolverEmpty(t *testing.T) {
	resolver := &KeepMajorityResolver{}
	ctx := ResolverContext{AllKnown: []*Member{}, Reachable: []*Member{}}

	if resolver.Resolve(ctx) != DecisionShutdown {
		t.Fatal("empty should shutdown")
	}
}

// === Config Tests ===

func TestDefaultSplitBrainConfig(t *testing.T) {
	cfg := DefaultSplitBrainConfig()
	if !cfg.Enabled {
		t.Fatal("should be enabled by default")
	}
	if cfg.CheckInterval != 5*time.Second {
		t.Fatalf("expected 5s check interval, got %v", cfg.CheckInterval)
	}
	if cfg.StableWindow != 10*time.Second {
		t.Fatalf("expected 10s stable window, got %v", cfg.StableWindow)
	}
	if cfg.Resolver == nil {
		t.Fatal("resolver should not be nil")
	}
}

func TestSplitBrainConfigBuilders(t *testing.T) {
	cfg := DefaultSplitBrainConfig().
		WithCheckInterval(1 * time.Second).
		WithStableWindow(3 * time.Second).
		WithResolver(&ShutdownAllResolver{})

	if cfg.CheckInterval != 1*time.Second {
		t.Fatalf("unexpected check interval: %v", cfg.CheckInterval)
	}
	if _, ok := cfg.Resolver.(*ShutdownAllResolver); !ok {
		t.Fatalf("expected ShutdownAllResolver, got %T", cfg.Resolver)
	}
}

// === Detector Tests ===

func TestSplitBrainDetectorStartStop(t *testing.T) {
	cluster := newTestCluster("localhost:19090", "self-node")

	sbConfig := DefaultSplitBrainConfig().
		WithCheckInterval(50 * time.Millisecond).
		WithStableWindow(100 * time.Millisecond)

	detector := NewSplitBrainDetector(cluster, sbConfig)
	detector.Start()

	if detector.State() != PartitionNormal {
		t.Fatalf("initial state should be Normal, got %v", detector.State())
	}

	detector.Stop()
}

func TestSplitBrainDetectorQuorumNormal(t *testing.T) {
	cluster := newTestCluster("localhost:19091", "self-node")

	// 添加 5 个成员（全部 Alive）
	for i := 0; i < 5; i++ {
		cluster.memberList.UpdateMember(&MemberGossipState{
			Address: "host" + string(rune('A'+i)) + ":8080",
			Id:      "node-" + string(rune('a'+i)),
			Kinds:   []string{"test"},
			Status:  MemberAlive,
			Seq:     1,
		})
	}

	sbConfig := DefaultSplitBrainConfig().
		WithCheckInterval(50 * time.Millisecond)

	detector := NewSplitBrainDetector(cluster, sbConfig)

	// 全部存活时，应正常
	detector.checkQuorum()
	if detector.State() != PartitionNormal {
		t.Fatalf("should be Normal with all members alive, got %v", detector.State())
	}
}

func TestSplitBrainDetectorQuorumLost(t *testing.T) {
	cluster := newTestCluster("localhost:19092", "self-node")

	// 添加 5 个成员
	for i := 0; i < 5; i++ {
		cluster.memberList.UpdateMember(&MemberGossipState{
			Address: "host" + string(rune('A'+i)) + ":8080",
			Id:      "node-" + string(rune('a'+i)),
			Kinds:   []string{"test"},
			Status:  MemberAlive,
			Seq:     1,
		})
	}

	sbConfig := DefaultSplitBrainConfig().
		WithCheckInterval(50 * time.Millisecond).
		WithStableWindow(100 * time.Millisecond)

	detector := NewSplitBrainDetector(cluster, sbConfig)

	// 标记 3 个为 Dead（只剩 2 个存活，无 Quorum）
	cluster.memberList.MarkDead("node-c")
	cluster.memberList.MarkDead("node-d")
	cluster.memberList.MarkDead("node-e")

	detector.checkQuorum()
	if detector.State() != PartitionSuspected {
		t.Fatalf("should be Suspected after quorum lost, got %v", detector.State())
	}
}

func TestSplitBrainDetectorEventPublished(t *testing.T) {
	cluster := newTestCluster("localhost:19093", "self-node")

	// 添加 3 个成员
	for i := 0; i < 3; i++ {
		cluster.memberList.UpdateMember(&MemberGossipState{
			Address: "host" + string(rune('A'+i)) + ":8080",
			Id:      "node-" + string(rune('a'+i)),
			Kinds:   []string{"test"},
			Status:  MemberAlive,
			Seq:     1,
		})
	}

	var detectedCount int32
	cluster.system.EventStream.Subscribe(func(event interface{}) {
		if _, ok := event.(*SplitBrainDetectedEvent); ok {
			atomic.AddInt32(&detectedCount, 1)
		}
	})

	sbConfig := DefaultSplitBrainConfig().
		WithCheckInterval(50 * time.Millisecond).
		WithStableWindow(0). // 立即确认
		WithResolver(&KeepMajorityResolver{})

	detector := NewSplitBrainDetector(cluster, sbConfig)

	// 制造脑裂
	cluster.memberList.MarkDead("node-b")
	cluster.memberList.MarkDead("node-c")

	detector.checkQuorum() // → Suspected
	detector.checkQuorum() // StableWindow=0 → Detected

	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&detectedCount) == 0 {
		t.Fatal("expected SplitBrainDetectedEvent to be published")
	}
}

func TestSplitBrainDetectorSingleNode(t *testing.T) {
	cluster := newTestCluster("localhost:19094", "self-node")

	// 只有自己
	cluster.memberList.UpdateMember(&MemberGossipState{
		Address: "localhost:19094",
		Id:      "self-node",
		Kinds:   []string{"test"},
		Status:  MemberAlive,
		Seq:     1,
	})

	sbConfig := DefaultSplitBrainConfig().WithCheckInterval(50 * time.Millisecond)
	detector := NewSplitBrainDetector(cluster, sbConfig)

	detector.checkQuorum()
	if detector.State() != PartitionNormal {
		t.Fatalf("single node should be Normal, got %v", detector.State())
	}
}

func TestSplitBrainDetectorQuorumRestored(t *testing.T) {
	cluster := newTestCluster("localhost:19095", "self-node")

	// 添加 3 个成员
	for i := 0; i < 3; i++ {
		cluster.memberList.UpdateMember(&MemberGossipState{
			Address: "host" + string(rune('A'+i)) + ":8080",
			Id:      "node-" + string(rune('a'+i)),
			Kinds:   []string{"test"},
			Status:  MemberAlive,
			Seq:     1,
		})
	}

	sbConfig := DefaultSplitBrainConfig().
		WithCheckInterval(50 * time.Millisecond).
		WithStableWindow(100 * time.Millisecond)

	detector := NewSplitBrainDetector(cluster, sbConfig)

	// 制造脑裂嫌疑
	cluster.memberList.MarkDead("node-b")
	cluster.memberList.MarkDead("node-c")
	detector.checkQuorum() // → Suspected

	// 恢复成员
	cluster.memberList.UpdateMember(&MemberGossipState{
		Address: "hostB:8080", Id: "node-b", Kinds: []string{"test"}, Status: MemberAlive, Seq: 10,
	})
	cluster.memberList.UpdateMember(&MemberGossipState{
		Address: "hostC:8080", Id: "node-c", Kinds: []string{"test"}, Status: MemberAlive, Seq: 10,
	})

	detector.checkQuorum() // → Normal
	if detector.State() != PartitionNormal {
		t.Fatalf("should be Normal after quorum restored, got %v", detector.State())
	}
}

func TestClusterConfigWithSplitBrain(t *testing.T) {
	config := DefaultClusterConfig("test", "localhost:19096").
		WithSplitBrain(DefaultSplitBrainConfig())

	if config.SplitBrain == nil || !config.SplitBrain.Enabled {
		t.Fatal("split-brain config should be set and enabled")
	}
}
