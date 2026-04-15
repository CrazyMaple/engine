package cluster

import (
	"encoding/json"
	"testing"
	"time"

	"engine/actor"
	"engine/remote"
)

// testMigratableActor 用于测试的可迁移 Actor
type testMigratableActor struct {
	state         testActorState
	migStarted    bool
	migCompleted  bool
	paused        bool
}

type testActorState struct {
	Name  string `json:"name"`
	Score int    `json:"score"`
}

func (a *testMigratableActor) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		// init
	case *MigrationSerialize:
		data, err := a.MarshalState()
		if err != nil {
			return
		}
		ctx.Respond(&MigrationStateData{
			ActorID:  ctx.Self().Id,
			KindName: "test",
			State:    data,
		})
	case *MigrationRestore:
		_ = a.UnmarshalState(msg.State)
		a.OnMigrationComplete()
	case *MigrationResume:
		a.paused = false
	}
}

func (a *testMigratableActor) MarshalState() ([]byte, error) {
	return json.Marshal(a.state)
}

func (a *testMigratableActor) UnmarshalState(data []byte) error {
	return json.Unmarshal(data, &a.state)
}

func (a *testMigratableActor) OnMigrationStart() {
	a.migStarted = true
}

func (a *testMigratableActor) OnMigrationComplete() {
	a.migCompleted = true
}

func TestMigratableInterface(t *testing.T) {
	a := &testMigratableActor{
		state: testActorState{Name: "player1", Score: 100},
	}

	// 序列化
	data, err := a.MarshalState()
	if err != nil {
		t.Fatalf("MarshalState failed: %v", err)
	}

	// 反序列化到新实例
	b := &testMigratableActor{}
	if err := b.UnmarshalState(data); err != nil {
		t.Fatalf("UnmarshalState failed: %v", err)
	}

	if b.state.Name != "player1" || b.state.Score != 100 {
		t.Errorf("state mismatch: got %+v, want {Name:player1, Score:100}", b.state)
	}
}

func TestMigrationConfig(t *testing.T) {
	config := DefaultMigrationConfig()
	if config.Timeout != 30*time.Second {
		t.Errorf("default timeout: got %v, want 30s", config.Timeout)
	}
	if config.PauseTimeout != 5*time.Second {
		t.Errorf("default pause timeout: got %v, want 5s", config.PauseTimeout)
	}
}

func TestMigrationStatus(t *testing.T) {
	tests := []struct {
		status MigrationStatus
		str    string
	}{
		{MigrationPending, "Pending"},
		{MigrationPausing, "Pausing"},
		{MigrationSerializing, "Serializing"},
		{MigrationTransferring, "Transferring"},
		{MigrationRestoring, "Restoring"},
		{MigrationCompleted, "Completed"},
		{MigrationFailed, "Failed"},
		{MigrationRolledBack, "RolledBack"},
	}

	for _, tt := range tests {
		if got := tt.status.String(); got != tt.str {
			t.Errorf("MigrationStatus(%d).String() = %s, want %s", tt.status, got, tt.str)
		}
	}
}

func TestMigrationManagerCreation(t *testing.T) {
	system := actor.NewActorSystem()
	r := remote.NewRemote(system, "127.0.0.1:0")

	config := DefaultClusterConfig("test", "127.0.0.1:8000")
	config.Kinds = []string{"test"}
	c := NewCluster(system, r, config)

	mm := NewMigrationManager(c, nil)
	if mm == nil {
		t.Fatal("NewMigrationManager returned nil")
	}

	// 注册 Kind
	props := actor.PropsFromProducer(func() actor.Actor {
		return &testMigratableActor{}
	})
	mm.RegisterKind("test", props)

	if _, ok := mm.getKindProps("test"); !ok {
		t.Error("registered kind 'test' not found")
	}
	if _, ok := mm.getKindProps("unknown"); ok {
		t.Error("unregistered kind should not be found")
	}
}

func TestMigrationManagerValidation(t *testing.T) {
	system := actor.NewActorSystem()
	r := remote.NewRemote(system, "127.0.0.1:0")

	config := DefaultClusterConfig("test", "127.0.0.1:8000")
	c := NewCluster(system, r, config)

	mm := NewMigrationManager(c, DefaultMigrationConfig())
	mm.coordPID = actor.NewLocalPID("dummy") // fake

	// nil PID
	_, err := mm.Migrate(nil, "127.0.0.1:9000")
	if err == nil {
		t.Error("expected error for nil PID")
	}

	// empty target
	_, err = mm.Migrate(actor.NewLocalPID("test"), "")
	if err == nil {
		t.Error("expected error for empty target")
	}

	// migrate to self
	_, err = mm.Migrate(actor.NewLocalPID("test"), "127.0.0.1:8000")
	if err == nil {
		t.Error("expected error for migrate to self")
	}
}

func TestMigrationAwareCallbacks(t *testing.T) {
	a := &testMigratableActor{
		state: testActorState{Name: "hero", Score: 50},
	}

	// 验证 MigrationAware 接口
	var _ MigrationAware = a

	if a.migStarted {
		t.Error("migStarted should be false initially")
	}

	a.OnMigrationStart()
	if !a.migStarted {
		t.Error("migStarted should be true after OnMigrationStart")
	}

	a.OnMigrationComplete()
	if !a.migCompleted {
		t.Error("migCompleted should be true after OnMigrationComplete")
	}
}

func TestMigrateSingletonNoSingleton(t *testing.T) {
	system := actor.NewActorSystem()
	r := remote.NewRemote(system, "127.0.0.1:0")

	config := DefaultClusterConfig("test", "127.0.0.1:8000")
	c := NewCluster(system, r, config)

	mm := NewMigrationManager(c, nil)

	// 未设置 singleton 时应报错
	_, err := mm.MigrateSingleton("player", "127.0.0.1:9000")
	if err == nil {
		t.Error("expected error when no singleton is set")
	}
}

func TestMigrateSingletonSetSingleton(t *testing.T) {
	system := actor.NewActorSystem()
	r := remote.NewRemote(system, "127.0.0.1:0")

	config := DefaultClusterConfig("test", "127.0.0.1:8000")
	config.Kinds = []string{"player"}
	c := NewCluster(system, r, config)

	mm := NewMigrationManager(c, nil)
	cs := NewClusterSingleton(c)
	mm.SetSingleton(cs)

	if mm.singleton == nil {
		t.Error("singleton should be set after SetSingleton")
	}

	// 未注册的 singleton 应返回错误
	_, err := mm.MigrateSingleton("player", "127.0.0.1:9000")
	if err == nil {
		t.Error("expected error for unregistered singleton")
	}
}

func TestMigrationStartNotifyMessage(t *testing.T) {
	// 验证新消息类型可以正常创建
	start := &MigrationStartNotify{}
	complete := &MigrationCompleteNotify{}
	if start == nil || complete == nil {
		t.Error("migration notify messages should be creatable")
	}
}

func TestRedirectProcess(t *testing.T) {
	system := actor.NewActorSystem()
	r := remote.NewRemote(system, "127.0.0.1:0")

	config := DefaultClusterConfig("test", "127.0.0.1:8000")
	c := NewCluster(system, r, config)

	mm := NewMigrationManager(c, nil)

	// 添加转发规则
	newPID := actor.NewLocalPID("new-actor")
	mm.addRedirect("old-actor", newPID)

	pid, ok := mm.GetRedirect("old-actor")
	if !ok || !pid.Equal(newPID) {
		t.Error("redirect not found or mismatch")
	}

	// 移除转发规则
	mm.removeRedirect("old-actor")
	_, ok = mm.GetRedirect("old-actor")
	if ok {
		t.Error("redirect should be removed")
	}
}
