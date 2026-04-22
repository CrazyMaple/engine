package k8s

import (
	"bytes"
	"strings"
	"testing"

	"gamelib/middleware"
)

// mockGate 模拟网关
type mockGate struct {
	count int64
}

func (m *mockGate) ConnCount() int64 { return m.count }

// mockSystem 模拟 ActorSystem
type mockSystem struct {
	count int
}

func (m *mockSystem) ActorCount() int { return m.count }

func TestRegisterHPAMetrics_AllMetrics(t *testing.T) {
	registry := middleware.NewMetricsRegistry()
	gate := &mockGate{count: 42}
	system := &mockSystem{count: 100}

	RegisterHPAMetrics(HPAMetricsConfig{
		Gate:        gate,
		System:      system,
		Registry:    registry,
		NodeRole:    "gate",
		NodeVersion: "v1.8.0",
	})

	var buf bytes.Buffer
	registry.WritePrometheus(&buf)
	output := buf.String()

	// 验证连接数指标
	if !strings.Contains(output, "engine_gate_connection_count") {
		t.Error("missing engine_gate_connection_count metric")
	}
	if !strings.Contains(output, "42") {
		t.Error("expected connection count 42")
	}

	// 验证 Actor 数指标
	if !strings.Contains(output, "engine_actor_count") {
		t.Error("missing engine_actor_count metric")
	}
	if !strings.Contains(output, "100") {
		t.Error("expected actor count 100")
	}

	// 验证节点信息指标
	if !strings.Contains(output, "engine_node_info") {
		t.Error("missing engine_node_info metric")
	}
	if !strings.Contains(output, `role="gate"`) {
		t.Error("missing role label")
	}
	if !strings.Contains(output, `version="v1.8.0"`) {
		t.Error("missing version label")
	}
}

func TestRegisterHPAMetrics_NilRegistry(t *testing.T) {
	// 不应 panic
	RegisterHPAMetrics(HPAMetricsConfig{
		Registry: nil,
	})
}

func TestRegisterHPAMetrics_PartialConfig(t *testing.T) {
	registry := middleware.NewMetricsRegistry()

	// 只注册 Gate，不注册 System
	RegisterHPAMetrics(HPAMetricsConfig{
		Gate:     &mockGate{count: 10},
		Registry: registry,
	})

	var buf bytes.Buffer
	registry.WritePrometheus(&buf)
	output := buf.String()

	if !strings.Contains(output, "engine_gate_connection_count") {
		t.Error("missing engine_gate_connection_count")
	}
	if strings.Contains(output, "engine_actor_count") {
		t.Error("should not have engine_actor_count when System is nil")
	}
}

func TestActorSystemAdapter(t *testing.T) {
	adapter := &ActorSystemAdapter{
		CountFn: func() int { return 256 },
	}
	if adapter.ActorCount() != 256 {
		t.Errorf("expected 256, got %d", adapter.ActorCount())
	}
}

func TestStandardLabels(t *testing.T) {
	labels := StandardLabels("myapp", "v1.0", "gate", "node-1")
	if labels["app.kubernetes.io/name"] != "myapp" {
		t.Error("wrong app name")
	}
	if labels["app.kubernetes.io/version"] != "v1.0" {
		t.Error("wrong version")
	}
	if labels["app.kubernetes.io/component"] != "gate" {
		t.Error("wrong component")
	}
}

func TestStandardAnnotations(t *testing.T) {
	annotations := StandardAnnotations(8080, "")
	if annotations["prometheus.io/path"] != "/api/metrics/prometheus" {
		t.Error("wrong default metrics path")
	}
	if annotations["prometheus.io/port"] != "8080" {
		t.Error("wrong port")
	}
}

func TestPodLabels(t *testing.T) {
	labels := PodLabels("engine", "v1.8", "game", "node-2", "prod-cluster")
	if labels["engine.io/cluster"] != "prod-cluster" {
		t.Error("wrong cluster label")
	}
}
