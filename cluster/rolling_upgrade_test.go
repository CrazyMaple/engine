package cluster

import (
	"testing"
	"time"

	"engine/actor"
	"engine/remote"
)

func TestVersionInfoCompatibility(t *testing.T) {
	v1 := &VersionInfo{Version: "1.0.0", ProtocolVersion: 1}
	v2 := &VersionInfo{Version: "1.1.0", ProtocolVersion: 2}
	v3 := &VersionInfo{Version: "2.0.0", ProtocolVersion: 3}

	// 相邻版本兼容
	if !v1.IsCompatibleWith(v2) {
		t.Error("v1 should be compatible with v2")
	}
	if !v2.IsCompatibleWith(v1) {
		t.Error("v2 should be compatible with v1")
	}

	// 跨两个版本不兼容
	if v1.IsCompatibleWith(v3) {
		t.Error("v1 should NOT be compatible with v3")
	}

	// nil 兼容
	if !v1.IsCompatibleWith(nil) {
		t.Error("v1 should be compatible with nil")
	}
	var nilVer *VersionInfo
	if !nilVer.IsCompatibleWith(v1) {
		t.Error("nil should be compatible with v1")
	}
}

func TestNodeStatus(t *testing.T) {
	tests := []struct {
		status NodeStatus
		str    string
	}{
		{NodeNormal, "Normal"},
		{NodeDraining, "Draining"},
		{NodeDrained, "Drained"},
		{NodeUpgrading, "Upgrading"},
		{NodeCanary, "Canary"},
	}

	for _, tt := range tests {
		if got := tt.status.String(); got != tt.str {
			t.Errorf("NodeStatus(%d).String() = %s, want %s", tt.status, got, tt.str)
		}
	}
}

func TestUpgradeState(t *testing.T) {
	tests := []struct {
		state UpgradeState
		str   string
	}{
		{UpgradeIdle, "Idle"},
		{UpgradeInProgress, "InProgress"},
		{UpgradeCanary, "Canary"},
		{UpgradeRollingBack, "RollingBack"},
		{UpgradeCompleted, "Completed"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.str {
			t.Errorf("UpgradeState(%d).String() = %s, want %s", tt.state, got, tt.str)
		}
	}
}

func TestDefaultUpgradeConfig(t *testing.T) {
	config := DefaultUpgradeConfig()
	if config.DrainTimeout != 30*time.Second {
		t.Errorf("DrainTimeout: got %v, want 30s", config.DrainTimeout)
	}
	if config.CanaryDuration != 60*time.Second {
		t.Errorf("CanaryDuration: got %v, want 60s", config.CanaryDuration)
	}
	if config.CanaryNodes != 1 {
		t.Errorf("CanaryNodes: got %d, want 1", config.CanaryNodes)
	}
	if config.HealthCheckInterval != 5*time.Second {
		t.Errorf("HealthCheckInterval: got %v, want 5s", config.HealthCheckInterval)
	}
	if config.HealthCheckThreshold != 3 {
		t.Errorf("HealthCheckThreshold: got %d, want 3", config.HealthCheckThreshold)
	}
	if config.MinHealthyNodes != 1 {
		t.Errorf("MinHealthyNodes: got %d, want 1", config.MinHealthyNodes)
	}
}

func TestRollingUpgradeCoordinatorCreation(t *testing.T) {
	system := actor.NewActorSystem()
	r := remote.NewRemote(system, "127.0.0.1:0")

	config := DefaultClusterConfig("test", "127.0.0.1:8000")
	config.Kinds = []string{"test"}
	c := NewCluster(system, r, config)

	ruc := NewRollingUpgradeCoordinator(c, nil)
	if ruc == nil {
		t.Fatal("NewRollingUpgradeCoordinator returned nil")
	}
	if ruc.State() != UpgradeIdle {
		t.Errorf("initial state: got %v, want Idle", ruc.State())
	}
}

func TestRollingUpgradeNotEnoughNodes(t *testing.T) {
	system := actor.NewActorSystem()
	r := remote.NewRemote(system, "127.0.0.1:0")

	config := DefaultClusterConfig("test", "127.0.0.1:8000")
	c := NewCluster(system, r, config)

	upgradeConfig := DefaultUpgradeConfig()
	upgradeConfig.MinHealthyNodes = 5 // 需要很多节点
	ruc := NewRollingUpgradeCoordinator(c, upgradeConfig)

	err := ruc.StartRollingUpgrade("2.0.0", []string{"127.0.0.1:9001"})
	if err == nil {
		t.Error("expected error for not enough healthy nodes")
	}
}

func TestActiveActorCount(t *testing.T) {
	system := actor.NewActorSystem()
	r := remote.NewRemote(system, "127.0.0.1:0")

	config := DefaultClusterConfig("test", "127.0.0.1:8000")
	c := NewCluster(system, r, config)

	ruc := NewRollingUpgradeCoordinator(c, nil)

	// 初始状态应有一些系统 Actor（$开头的不计入）
	count := ruc.ActiveActorCount()
	// count 可能为 0 或正数，取决于系统初始化
	if count < 0 {
		t.Errorf("ActiveActorCount should be >= 0, got %d", count)
	}
}

func TestWithMigrationManager(t *testing.T) {
	system := actor.NewActorSystem()
	r := remote.NewRemote(system, "127.0.0.1:0")

	config := DefaultClusterConfig("test", "127.0.0.1:8000")
	c := NewCluster(system, r, config)

	ruc := NewRollingUpgradeCoordinator(c, nil)
	mm := NewMigrationManager(c, nil)

	result := ruc.WithMigrationManager(mm)
	if result != ruc {
		t.Error("WithMigrationManager should return self for chaining")
	}
	if ruc.migrationManager != mm {
		t.Error("migrationManager should be set after WithMigrationManager")
	}
}

func TestSelectMigrationTarget(t *testing.T) {
	system := actor.NewActorSystem()
	r := remote.NewRemote(system, "127.0.0.1:0")

	config := DefaultClusterConfig("test", "127.0.0.1:8000")
	c := NewCluster(system, r, config)

	ruc := NewRollingUpgradeCoordinator(c, nil)

	// 没有其他成员时应返回空
	target := ruc.selectMigrationTarget("127.0.0.1:8000")
	if target != "" {
		t.Errorf("expected empty target, got %s", target)
	}
}

func TestIsDraining(t *testing.T) {
	system := actor.NewActorSystem()
	r := remote.NewRemote(system, "127.0.0.1:0")

	config := DefaultClusterConfig("test", "127.0.0.1:8000")
	c := NewCluster(system, r, config)

	ruc := NewRollingUpgradeCoordinator(c, nil)

	if ruc.IsDraining() {
		t.Error("should not be draining initially")
	}

	ruc.mu.Lock()
	ruc.drainStatus["127.0.0.1:8000"] = NodeDraining
	ruc.mu.Unlock()

	if !ruc.IsDraining() {
		t.Error("should be draining after setting status")
	}
}
