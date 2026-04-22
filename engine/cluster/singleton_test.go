package cluster

import (
	"testing"
	"time"

	"engine/actor"
)

// testSingletonActor 测试用单例 Actor
type testSingletonActor struct {
	started bool
}

func (a *testSingletonActor) Receive(ctx actor.Context) {
	switch ctx.Message().(type) {
	case *actor.Started:
		a.started = true
	}
}

func newSingletonTestCluster(addr string, kinds []string) *Cluster {
	system := actor.NewActorSystem()
	self := &Member{
		Address:  addr,
		Id:       generateNodeId(addr),
		Kinds:    kinds,
		Status:   MemberAlive,
		Seq:      1,
		LastSeen: time.Now(),
	}
	c := &Cluster{
		system:   system,
		config:   DefaultClusterConfig("singleton-test", addr),
		hashRing: NewConsistentHash(),
		self:     self,
		started:  true,
	}
	c.config.Kinds = kinds
	c.memberList = NewMemberList(c)
	// 将自己加入成员列表
	c.memberList.UpdateMember(&MemberGossipState{
		Address: self.Address,
		Id:      self.Id,
		Kinds:   self.Kinds,
		Status:  MemberAlive,
		Seq:     self.Seq,
	})
	c.updateHashRing()
	return c
}

func TestClusterSingletonRegister(t *testing.T) {
	c := newSingletonTestCluster("127.0.0.1:9200", []string{"leaderboard"})
	cs := NewClusterSingleton(c)

	// 注册单例
	err := cs.Register(SingletonConfig{
		Kind:  "leaderboard",
		Props: actor.PropsFromProducer(func() actor.Actor { return &testSingletonActor{} }),
	})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	// 重复注册应失败
	err = cs.Register(SingletonConfig{
		Kind:  "leaderboard",
		Props: actor.PropsFromProducer(func() actor.Actor { return &testSingletonActor{} }),
	})
	if err == nil {
		t.Error("duplicate register should fail")
	}

	// 空 Kind 应失败
	err = cs.Register(SingletonConfig{
		Props: actor.PropsFromProducer(func() actor.Actor { return &testSingletonActor{} }),
	})
	if err == nil {
		t.Error("empty kind should fail")
	}

	// nil Props 应失败
	err = cs.Register(SingletonConfig{Kind: "test"})
	if err == nil {
		t.Error("nil props should fail")
	}

	cs.Stop()
}

func TestClusterSingletonActivation(t *testing.T) {
	c := newSingletonTestCluster("127.0.0.1:9201", []string{"broadcast"})
	cs := NewClusterSingleton(c)

	err := cs.Register(SingletonConfig{
		Kind:  "broadcast",
		Props: actor.PropsFromProducer(func() actor.Actor { return &testSingletonActor{} }),
	})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	// 单节点集群，应该在本地激活
	pid, err := cs.Get("broadcast")
	if err != nil {
		t.Fatalf("get singleton failed: %v", err)
	}
	if pid == nil {
		t.Fatal("singleton PID should not be nil")
	}

	// 注销
	cs.Unregister("broadcast")

	_, err = cs.Get("broadcast")
	if err == nil {
		t.Error("get after unregister should fail")
	}

	cs.Stop()
}

func TestClusterSingletonGetUnregistered(t *testing.T) {
	c := newSingletonTestCluster("127.0.0.1:9202", []string{"test"})
	cs := NewClusterSingleton(c)

	_, err := cs.Get("nonexistent")
	if err == nil {
		t.Error("get unregistered should fail")
	}

	cs.Stop()
}

func TestClusterSingletonTopologyChange(t *testing.T) {
	c := newSingletonTestCluster("127.0.0.1:9203", []string{"global"})
	cs := NewClusterSingleton(c)
	cs.Start()

	err := cs.Register(SingletonConfig{
		Kind:  "global",
		Props: actor.PropsFromProducer(func() actor.Actor { return &testSingletonActor{} }),
	})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	// 模拟拓扑变更事件
	cs.onTopologyChange()

	pid, err := cs.Get("global")
	if err != nil {
		t.Fatalf("after topology change: %v", err)
	}
	if pid == nil {
		t.Fatal("singleton should still be active")
	}

	cs.Stop()
}
