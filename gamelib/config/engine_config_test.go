package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultEngineConfigValidates(t *testing.T) {
	cfg := DefaultEngineConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should validate: %v", err)
	}
}

func TestLoadEngineConfigFromBytes(t *testing.T) {
	y := `
version: "2.0"
node_id: test-node
cluster:
  enabled: true
  name: test-cluster
  seeds:
    - 10.0.0.1:6000
    - 10.0.0.2:6000
  gossip_period: 2s
  provider: consul
remote:
  address: 0.0.0.0:7000
  codec: protobuf
  enable_tls: true
gate:
  tcp_addr: 0.0.0.0:9000
  max_msg_len: 2097152
log:
  level: debug
  format: json
`
	cfg, err := LoadEngineConfigFromBytes([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NodeID != "test-node" {
		t.Errorf("node_id: %q", cfg.NodeID)
	}
	if !cfg.Cluster.Enabled || cfg.Cluster.Name != "test-cluster" || cfg.Cluster.Provider != "consul" {
		t.Errorf("cluster mismatch: %+v", cfg.Cluster)
	}
	if len(cfg.Cluster.Seeds) != 2 || cfg.Cluster.Seeds[0] != "10.0.0.1:6000" {
		t.Errorf("seeds: %v", cfg.Cluster.Seeds)
	}
	if cfg.Cluster.GossipPeriod != 2*time.Second {
		t.Errorf("gossip period: %v", cfg.Cluster.GossipPeriod)
	}
	if cfg.Remote.Address != "0.0.0.0:7000" || cfg.Remote.Codec != "protobuf" || !cfg.Remote.EnableTLS {
		t.Errorf("remote mismatch: %+v", cfg.Remote)
	}
	if cfg.Gate.TCPAddr != "0.0.0.0:9000" || cfg.Gate.MaxMsgLen != 2097152 {
		t.Errorf("gate mismatch: %+v", cfg.Gate)
	}
	if cfg.Log.Level != "debug" || cfg.Log.Format != "json" {
		t.Errorf("log mismatch: %+v", cfg.Log)
	}
}

func TestApplyEnvOverrides(t *testing.T) {
	cfg := DefaultEngineConfig()
	env := []string{
		"ENGINE_NODE_ID=override-node",
		"ENGINE_REMOTE_ADDRESS=0.0.0.0:7777",
		"ENGINE_REMOTE_MAX_CONN_NUM=2000",
		"ENGINE_REMOTE_CODEC=protobuf",
		"ENGINE_REMOTE_ENABLE_TLS=true",
		"ENGINE_CLUSTER_ENABLED=true",
		"ENGINE_CLUSTER_NAME=envcluster",
		"ENGINE_CLUSTER_SEEDS=a:1,b:2,c:3",
		"ENGINE_CLUSTER_GOSSIP_PERIOD=500ms",
		"ENGINE_GATE_MAX_MSG_LEN=65536",
		"ENGINE_LOG_LEVEL=debug",
		"SOMEONE_ELSE=ignore",
	}
	if err := cfg.ApplyEnv(env); err != nil {
		t.Fatal(err)
	}
	if cfg.NodeID != "override-node" {
		t.Errorf("node_id not overridden: %q", cfg.NodeID)
	}
	if cfg.Remote.Address != "0.0.0.0:7777" || cfg.Remote.MaxConnNum != 2000 ||
		cfg.Remote.Codec != "protobuf" || !cfg.Remote.EnableTLS {
		t.Errorf("remote overrides failed: %+v", cfg.Remote)
	}
	if !cfg.Cluster.Enabled || cfg.Cluster.Name != "envcluster" ||
		len(cfg.Cluster.Seeds) != 3 || cfg.Cluster.Seeds[2] != "c:3" {
		t.Errorf("cluster overrides failed: %+v", cfg.Cluster)
	}
	if cfg.Cluster.GossipPeriod != 500*time.Millisecond {
		t.Errorf("gossip period override failed: %v", cfg.Cluster.GossipPeriod)
	}
	if cfg.Gate.MaxMsgLen != 65536 {
		t.Errorf("gate.max_msg_len override failed: %d", cfg.Gate.MaxMsgLen)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("log.level override failed: %q", cfg.Log.Level)
	}
}

func TestApplyEnvInvalidValues(t *testing.T) {
	cases := []struct {
		name string
		env  []string
		want string
	}{
		{"invalid bool", []string{"ENGINE_CLUSTER_ENABLED=yesh"}, "invalid bool"},
		{"invalid int", []string{"ENGINE_REMOTE_MAX_CONN_NUM=xyz"}, "invalid int"},
		{"invalid uint32", []string{"ENGINE_GATE_MAX_MSG_LEN=-1"}, "invalid uint32"},
		{"invalid duration", []string{"ENGINE_REMOTE_HEALTH_INTERVAL=notatime"}, "invalid duration"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := DefaultEngineConfig()
			err := cfg.ApplyEnv(c.env)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("expected error containing %q, got %v", c.want, err)
			}
		})
	}
}

func TestValidateFailures(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*EngineConfig)
		want string
	}{
		{"no node_id", func(c *EngineConfig) { c.NodeID = "" }, "node_id"},
		{"no remote addr", func(c *EngineConfig) { c.Remote.Address = "" }, "remote.address"},
		{"remote addr no port", func(c *EngineConfig) { c.Remote.Address = "localhost" }, "host:port"},
		{"bad codec", func(c *EngineConfig) { c.Remote.Codec = "avro" }, "remote.codec"},
		{"cluster enabled no name", func(c *EngineConfig) {
			c.Cluster.Enabled = true
			c.Cluster.Name = ""
		}, "cluster.name"},
		{"bad cluster provider", func(c *EngineConfig) { c.Cluster.Provider = "zoo" }, "cluster.provider"},
		{"bad log level", func(c *EngineConfig) { c.Log.Level = "verbose" }, "log.level"},
		{"bad log format", func(c *EngineConfig) { c.Log.Format = "xml" }, "log.format"},
		{"max msg len too small", func(c *EngineConfig) { c.Gate.MaxMsgLen = 10 }, "gate.max_msg_len"},
		{"negative max_conn_num", func(c *EngineConfig) { c.Remote.MaxConnNum = -5 }, "max_conn_num"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := DefaultEngineConfig()
			c.mut(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected validation failure")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("expected %q, got %v", c.want, err)
			}
		})
	}
}

func TestLoadEngineConfigFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "engine.yaml")
	if err := os.WriteFile(path, GenerateTemplate(), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadEngineConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NodeID == "" {
		t.Error("node_id empty after loading template")
	}
	if cfg.Remote.Address != "0.0.0.0:6000" {
		t.Errorf("unexpected address: %s", cfg.Remote.Address)
	}
}

func TestLoadEngineConfigEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "engine.yaml")
	y := []byte(`node_id: file-node
remote:
  address: 0.0.0.0:6000
`)
	if err := os.WriteFile(path, y, 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENGINE_NODE_ID", "env-node")
	t.Setenv("ENGINE_REMOTE_ADDRESS", "0.0.0.0:9999")
	cfg, err := LoadEngineConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NodeID != "env-node" {
		t.Errorf("env override failed: %q", cfg.NodeID)
	}
	if cfg.Remote.Address != "0.0.0.0:9999" {
		t.Errorf("env override failed: %q", cfg.Remote.Address)
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.NodeID = "rt-node"
	cfg.Cluster.Enabled = true
	cfg.Cluster.Name = "rt-cluster"
	cfg.Cluster.Seeds = []string{"x:1", "y:2"}
	data, err := cfg.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadEngineConfigFromBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.NodeID != cfg.NodeID || !loaded.Cluster.Enabled ||
		loaded.Cluster.Name != cfg.Cluster.Name || len(loaded.Cluster.Seeds) != 2 {
		t.Errorf("round trip mismatch: %+v", loaded)
	}
}

func TestGenerateTemplateProducesValidConfig(t *testing.T) {
	tmpl := GenerateTemplate()
	cfg, err := LoadEngineConfigFromBytes(tmpl)
	if err != nil {
		t.Fatalf("template should parse: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("template should validate: %v", err)
	}
	// 关键字段应与 DefaultEngineConfig 对齐
	if cfg.Remote.Address != "0.0.0.0:6000" {
		t.Errorf("template remote.address mismatch: %s", cfg.Remote.Address)
	}
	if !cfg.Dashboard.Enabled {
		t.Error("template dashboard should be enabled by default")
	}
}

func TestSaveAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "saved.yaml")
	cfg := DefaultEngineConfig()
	cfg.NodeID = "saved-node"
	cfg.Custom = map[string]any{"greeting": "hello"}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadEngineConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.NodeID != "saved-node" {
		t.Errorf("node_id mismatch: %s", loaded.NodeID)
	}
	if loaded.Custom["greeting"] != "hello" {
		t.Errorf("custom data lost: %v", loaded.Custom)
	}
}
