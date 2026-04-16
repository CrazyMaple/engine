package operator

import (
	"testing"
	"time"
)

// --- mock 实现 ---

type mockClusterSource struct {
	members []ClusterMemberInfo
}

func (m *mockClusterSource) Members() []ClusterMemberInfo { return m.members }
func (m *mockClusterSource) IsHealthy(addr string) bool {
	for _, member := range m.members {
		if member.Address == addr && member.Alive {
			return true
		}
	}
	return false
}

type mockMetricsProvider struct {
	conns  map[string]int64
	actors map[string]int
	cpu    map[string]float64
}

func newMockMetrics() *mockMetricsProvider {
	return &mockMetricsProvider{
		conns:  make(map[string]int64),
		actors: make(map[string]int),
		cpu:    make(map[string]float64),
	}
}

func (m *mockMetricsProvider) NodeConnections(addr string) int64  { return m.conns[addr] }
func (m *mockMetricsProvider) NodeActorCount(addr string) int     { return m.actors[addr] }
func (m *mockMetricsProvider) NodeCPUPercent(addr string) float64 { return m.cpu[addr] }

// --- CRD 类型测试 ---

func TestDefaultUpgradeStrategy(t *testing.T) {
	s := DefaultUpgradeStrategy()
	if s.Type != "RollingUpdate" {
		t.Errorf("expected RollingUpdate, got %s", s.Type)
	}
	if s.MaxUnavailable != 1 {
		t.Errorf("expected MaxUnavailable 1, got %d", s.MaxUnavailable)
	}
	if !s.MigrateActors {
		t.Error("expected MigrateActors true")
	}
}

func TestDefaultScalePolicy(t *testing.T) {
	p := DefaultScalePolicy()
	if p.MinReplicas != 2 {
		t.Errorf("expected MinReplicas 2, got %d", p.MinReplicas)
	}
	if p.MaxReplicas != 20 {
		t.Errorf("expected MaxReplicas 20, got %d", p.MaxReplicas)
	}
	if p.ConnectionThreshold != 1000 {
		t.Errorf("expected ConnectionThreshold 1000, got %d", p.ConnectionThreshold)
	}
}

func TestClusterPhase(t *testing.T) {
	phases := []ClusterPhase{PhasePending, PhaseRunning, PhaseUpgrading, PhaseScaling, PhaseFailed}
	expected := []string{"Pending", "Running", "Upgrading", "Scaling", "Failed"}
	for i, p := range phases {
		if string(p) != expected[i] {
			t.Errorf("expected %s, got %s", expected[i], p)
		}
	}
}

// --- Controller 测试 ---

func TestController_ApplyAndReconcile(t *testing.T) {
	source := &mockClusterSource{
		members: []ClusterMemberInfo{
			{Address: "node1:9100", Kinds: []string{"game"}, Alive: true},
			{Address: "node2:9100", Kinds: []string{"game"}, Alive: true},
			{Address: "node3:9100", Kinds: []string{"gate"}, Alive: true},
		},
	}

	ctrl := NewController(ControllerConfig{
		Source:            source,
		ReconcileInterval: time.Hour, // 不自动触发
	})

	ec := &EngineCluster{
		Name:      "test-cluster",
		Namespace: "default",
		Spec: EngineClusterSpec{
			Replicas:    3,
			Version:     "v1.9.0",
			ClusterName: "test",
			Image:       "engine/engine",
		},
		Status: EngineClusterStatus{
			Phase:          PhasePending,
			CurrentVersion: "v1.9.0",
		},
	}

	ctrl.Apply(ec)

	// 验证节点被同步
	nodes := ctrl.Nodes()
	if len(nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(nodes))
	}

	// 验证状态更新
	status := ctrl.Status()
	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.ReadyReplicas != 3 {
		t.Errorf("expected 3 ready replicas, got %d", status.ReadyReplicas)
	}
	if status.Phase != PhaseRunning {
		t.Errorf("expected Running phase, got %s", status.Phase)
	}
}

func TestController_VersionUpgradeDetection(t *testing.T) {
	source := &mockClusterSource{
		members: []ClusterMemberInfo{
			{Address: "node1:9100", Alive: true},
			{Address: "node2:9100", Alive: true},
		},
	}

	var events []ControllerEvent
	ctrl := NewController(ControllerConfig{
		Source:            source,
		ReconcileInterval: time.Hour,
	})
	ctrl.SetEventHandler(func(e ControllerEvent) {
		events = append(events, e)
	})

	ec := &EngineCluster{
		Name:      "test",
		Namespace: "default",
		Spec: EngineClusterSpec{
			Replicas: 2,
			Version:  "v2.0.0",
		},
		Status: EngineClusterStatus{
			CurrentVersion: "v1.9.0",
		},
	}

	ctrl.Apply(ec)

	// 应检测到版本变更并发出事件
	found := false
	for _, e := range events {
		if e.Type == "UpgradeStart" || e.Type == "UpgradeComplete" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected upgrade event to be emitted")
	}
}

func TestController_ScaleDownCandidateSelection(t *testing.T) {
	ctrl := NewController(ControllerConfig{
		ReconcileInterval: time.Hour,
	})

	ctrl.mu.Lock()
	ctrl.nodes = map[string]*NodeInfo{
		"node1": {Address: "node1", Connections: 100, ActorCount: 500, Ready: true},
		"node2": {Address: "node2", Connections: 10, ActorCount: 50, Ready: true},
		"node3": {Address: "node3", Connections: 50, ActorCount: 200, Ready: true},
	}
	ctrl.mu.Unlock()

	candidates := ctrl.selectScaleDownCandidates(1)
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	// node2 应优先被选中（连接数+Actor数最少）
	if candidates[0] != "node2" {
		t.Errorf("expected node2, got %s", candidates[0])
	}

	candidates = ctrl.selectScaleDownCandidates(2)
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}
}

func TestController_NodeStateSync(t *testing.T) {
	source := &mockClusterSource{
		members: []ClusterMemberInfo{
			{Address: "node1:9100", Alive: true},
			{Address: "node2:9100", Alive: false},
		},
	}

	ctrl := NewController(ControllerConfig{
		Source:            source,
		ReconcileInterval: time.Hour,
	})

	ctrl.syncNodeStates()

	nodes := ctrl.Nodes()
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	if !nodes["node1:9100"].Ready {
		t.Error("expected node1 ready")
	}
	if nodes["node2:9100"].Ready {
		t.Error("expected node2 not ready")
	}

	// 模拟节点消失
	source.members = source.members[:1]
	ctrl.syncNodeStates()

	nodes = ctrl.Nodes()
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node after removal, got %d", len(nodes))
	}
}

func TestController_StatusPhasePending(t *testing.T) {
	source := &mockClusterSource{
		members: []ClusterMemberInfo{}, // 无节点
	}

	ctrl := NewController(ControllerConfig{
		Source:            source,
		ReconcileInterval: time.Hour,
	})

	ec := &EngineCluster{
		Spec:   EngineClusterSpec{Replicas: 3, Version: "v1.0"},
		Status: EngineClusterStatus{CurrentVersion: "v1.0"},
	}

	ctrl.Apply(ec)

	if ec.Status.Phase != PhasePending {
		t.Errorf("expected Pending with no ready nodes, got %s", ec.Status.Phase)
	}
}

// --- Scaler 测试 ---

func TestScaler_ScaleUpOnHighConnections(t *testing.T) {
	metrics := newMockMetrics()
	metrics.conns["node1"] = 1200
	metrics.conns["node2"] = 1100
	metrics.actors["node1"] = 100
	metrics.actors["node2"] = 100
	metrics.cpu["node1"] = 30
	metrics.cpu["node2"] = 40

	policy := &ScalePolicy{
		MinReplicas:         2,
		MaxReplicas:         10,
		ConnectionThreshold: 1000,
		ActorThreshold:      5000,
		CPUThreshold:        80,
		CooldownPeriod:      0, // 无冷却
	}

	scaler := NewScaler(policy, metrics)
	decision := scaler.Evaluate([]string{"node1", "node2"})

	if decision.Direction != ScaleUp {
		t.Errorf("expected ScaleUp, got %s", decision.Direction)
	}
	if decision.Target != 3 {
		t.Errorf("expected target 3, got %d", decision.Target)
	}
}

func TestScaler_ScaleDownOnLowUtilization(t *testing.T) {
	metrics := newMockMetrics()
	metrics.conns["node1"] = 100
	metrics.conns["node2"] = 100
	metrics.conns["node3"] = 100
	metrics.actors["node1"] = 50
	metrics.actors["node2"] = 50
	metrics.actors["node3"] = 50
	metrics.cpu["node1"] = 10
	metrics.cpu["node2"] = 15
	metrics.cpu["node3"] = 12

	policy := &ScalePolicy{
		MinReplicas:         2,
		MaxReplicas:         10,
		ConnectionThreshold: 1000,
		ActorThreshold:      5000,
		CPUThreshold:        80,
		CooldownPeriod:      0,
	}

	scaler := NewScaler(policy, metrics)
	decision := scaler.Evaluate([]string{"node1", "node2", "node3"})

	if decision.Direction != ScaleDown {
		t.Errorf("expected ScaleDown, got %s", decision.Direction)
	}
	if decision.Target != 2 {
		t.Errorf("expected target 2 (minReplicas), got %d", decision.Target)
	}
}

func TestScaler_NoScaleInCooldown(t *testing.T) {
	metrics := newMockMetrics()
	metrics.conns["node1"] = 2000

	policy := &ScalePolicy{
		MinReplicas:         1,
		MaxReplicas:         10,
		ConnectionThreshold: 1000,
		CooldownPeriod:      5 * time.Minute,
	}

	scaler := NewScaler(policy, metrics)
	scaler.MarkScaled() // 刚扩过容

	decision := scaler.Evaluate([]string{"node1"})
	if decision.Direction != ScaleNone {
		t.Errorf("expected ScaleNone during cooldown, got %s", decision.Direction)
	}
}

func TestScaler_NoScaleAtMax(t *testing.T) {
	metrics := newMockMetrics()
	for i := 0; i < 10; i++ {
		addr := "node" + intToStr(int64(i))
		metrics.conns[addr] = 2000
	}

	policy := &ScalePolicy{
		MinReplicas:         2,
		MaxReplicas:         10,
		ConnectionThreshold: 1000,
		CooldownPeriod:      0,
	}

	nodes := make([]string, 10)
	for i := range nodes {
		nodes[i] = "node" + intToStr(int64(i))
	}

	scaler := NewScaler(policy, metrics)
	decision := scaler.Evaluate(nodes)

	// 已经在 max，不应再扩容
	if decision.Direction != ScaleNone {
		t.Errorf("expected ScaleNone at max, got %s", decision.Direction)
	}
}

func TestScaler_History(t *testing.T) {
	metrics := newMockMetrics()
	metrics.conns["node1"] = 2000

	policy := &ScalePolicy{
		MinReplicas:         1,
		MaxReplicas:         10,
		ConnectionThreshold: 1000,
		CooldownPeriod:      0,
	}

	scaler := NewScaler(policy, metrics)
	scaler.Evaluate([]string{"node1"})

	history := scaler.History()
	if len(history) != 1 {
		t.Errorf("expected 1 history entry, got %d", len(history))
	}
	if history[0].Direction != ScaleUp {
		t.Errorf("expected ScaleUp in history, got %s", history[0].Direction)
	}
}

func TestScaler_ScaleUpOnHighCPU(t *testing.T) {
	metrics := newMockMetrics()
	metrics.conns["node1"] = 100
	metrics.actors["node1"] = 100
	metrics.cpu["node1"] = 95

	policy := &ScalePolicy{
		MinReplicas:         1,
		MaxReplicas:         5,
		ConnectionThreshold: 1000,
		ActorThreshold:      5000,
		CPUThreshold:        80,
		CooldownPeriod:      0,
	}

	scaler := NewScaler(policy, metrics)
	decision := scaler.Evaluate([]string{"node1"})

	if decision.Direction != ScaleUp {
		t.Errorf("expected ScaleUp on high CPU, got %s", decision.Direction)
	}
}

func TestScaleDirection_String(t *testing.T) {
	if ScaleNone.String() != "None" {
		t.Errorf("expected None, got %s", ScaleNone.String())
	}
	if ScaleUp.String() != "ScaleUp" {
		t.Errorf("expected ScaleUp, got %s", ScaleUp.String())
	}
	if ScaleDown.String() != "ScaleDown" {
		t.Errorf("expected ScaleDown, got %s", ScaleDown.String())
	}
}

func TestFormatFloat(t *testing.T) {
	cases := []struct {
		input    float64
		expected string
	}{
		{0, "0"},
		{100, "100"},
		{3.5, "3.5"},
	}
	for _, c := range cases {
		got := formatFloat(c.input)
		if got != c.expected {
			t.Errorf("formatFloat(%v) = %s, want %s", c.input, got, c.expected)
		}
	}
}
