package federation

import (
	"testing"
	"time"
)

func TestParseFederatedPID(t *testing.T) {
	cases := []struct {
		addr      string
		clusterID string
		actorPath string
		wantErr   bool
	}{
		{"cluster://clusterB/actor/player1", "clusterB", "actor/player1", false},
		{"cluster://clusterA/singleton/leaderboard", "clusterA", "singleton/leaderboard", false},
		{"cluster://clusterC/", "clusterC", "", false},
		{"cluster://clusterD", "clusterD", "", false},
		{"127.0.0.1:8080", "", "", true},
		{"", "", "", true},
	}

	for _, c := range cases {
		clusterID, actorPath, err := ParseFederatedPID(c.addr)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseFederatedPID(%q): expected error", c.addr)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseFederatedPID(%q): unexpected error: %v", c.addr, err)
			continue
		}
		if clusterID != c.clusterID {
			t.Errorf("ParseFederatedPID(%q): clusterID = %q, want %q", c.addr, clusterID, c.clusterID)
		}
		if actorPath != c.actorPath {
			t.Errorf("ParseFederatedPID(%q): actorPath = %q, want %q", c.addr, actorPath, c.actorPath)
		}
	}
}

func TestIsFederated(t *testing.T) {
	if !IsFederated("cluster://clusterB/actor/xxx") {
		t.Error("should be federated")
	}
	if IsFederated("127.0.0.1:8080") {
		t.Error("should not be federated")
	}
	if IsFederated("") {
		t.Error("empty should not be federated")
	}
}

func TestFederatedPIDString(t *testing.T) {
	s := FederatedPIDString("clusterA", "actor/player1")
	if s != "cluster://clusterA/actor/player1" {
		t.Errorf("got %q", s)
	}
}

func TestNewFederatedPID(t *testing.T) {
	pid := NewFederatedPID("clusterB", "singleton/leaderboard")
	if pid.Address != "cluster://clusterB/singleton/leaderboard" {
		t.Errorf("Address = %q", pid.Address)
	}
	if pid.Id != "singleton/leaderboard" {
		t.Errorf("Id = %q", pid.Id)
	}
}

func TestClusterRegistry(t *testing.T) {
	reg := NewClusterRegistry()

	// Register
	reg.Register("cluster1", "10.0.0.1:9000", []string{"player", "npc"})
	reg.Register("cluster2", "10.0.0.2:9000", []string{"world"})

	if reg.Count() != 2 {
		t.Errorf("Count = %d, want 2", reg.Count())
	}

	// Lookup
	entry, ok := reg.Lookup("cluster1")
	if !ok {
		t.Fatal("cluster1 not found")
	}
	if entry.GatewayAddress != "10.0.0.1:9000" {
		t.Errorf("address = %s", entry.GatewayAddress)
	}
	if entry.Status != "alive" {
		t.Errorf("status = %s, want alive", entry.Status)
	}

	// UpdateStatus
	reg.UpdateStatus("cluster1", "suspect")
	entry, _ = reg.Lookup("cluster1")
	if entry.Status != "suspect" {
		t.Errorf("status = %s, want suspect", entry.Status)
	}

	// All
	all := reg.All()
	if len(all) != 2 {
		t.Errorf("All: len = %d, want 2", len(all))
	}

	// Unregister
	reg.Unregister("cluster2")
	if reg.Count() != 1 {
		t.Errorf("after unregister: Count = %d, want 1", reg.Count())
	}
}

func TestFederationConfig(t *testing.T) {
	cfg := DefaultFederationConfig("myCluster", "10.0.0.1:9100")
	if cfg.LocalClusterID != "myCluster" {
		t.Errorf("LocalClusterID = %s", cfg.LocalClusterID)
	}
	if cfg.HeartbeatInterval != 5*time.Second {
		t.Errorf("HeartbeatInterval = %v", cfg.HeartbeatInterval)
	}

	cfg.WithPeer("cluster2", "10.0.0.2:9100")
	if len(cfg.PeerClusters) != 1 {
		t.Errorf("PeerClusters len = %d", len(cfg.PeerClusters))
	}
}

func TestFederatedMessages(t *testing.T) {
	msg := &FederatedMessage{
		SourceCluster: "cluster1",
		TargetCluster: "cluster2",
		TargetActor:   "actor/player1",
		TypeName:      "MoveRequest",
	}
	if msg.SourceCluster != "cluster1" {
		t.Error("SourceCluster mismatch")
	}

	ping := &FederatedPing{ClusterID: "cluster1", Timestamp: time.Now().UnixMilli()}
	if ping.Timestamp <= 0 {
		t.Error("ping timestamp should be positive")
	}

	pong := &FederatedPong{ClusterID: "cluster1", Timestamp: ping.Timestamp}
	if pong.ClusterID != "cluster1" {
		t.Error("pong ClusterID mismatch")
	}

	reg := &FederatedRegister{
		ClusterID:      "cluster3",
		GatewayAddress: "10.0.0.3:9100",
		Kinds:          []string{"player"},
	}
	if len(reg.Kinds) != 1 {
		t.Error("reg Kinds mismatch")
	}
}
