package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// EngineConfig 引擎统一配置
// 支持：YAML 文件加载、环境变量覆盖、字段校验、默认值
//
// 结构对齐 deploy/helm/engine/values.yaml，
// 使 Helm Chart 的 values 能直接映射到 engine.yaml
type EngineConfig struct {
	Version   string           `yaml:"version"`
	NodeID    string           `yaml:"node_id"`
	Cluster   ClusterSection   `yaml:"cluster"`
	Remote    RemoteSection    `yaml:"remote"`
	Gate      GateSection      `yaml:"gate"`
	Dashboard DashboardSection `yaml:"dashboard"`
	Log       LogSection       `yaml:"log"`
	Metrics   MetricsSection   `yaml:"metrics"`
	Custom    map[string]any   `yaml:"custom,omitempty"`
}

// ClusterSection 集群配置
type ClusterSection struct {
	Enabled      bool          `yaml:"enabled"`
	Name         string        `yaml:"name"`
	Seeds        []string      `yaml:"seeds"`
	GossipPeriod time.Duration `yaml:"gossip_period"`
	Provider     string        `yaml:"provider"` // static|consul|etcd|k8s
}

// RemoteSection 远程通信配置
type RemoteSection struct {
	Address          string        `yaml:"address"`
	MaxConnNum       int           `yaml:"max_conn_num"`
	PendingWriteNum  int           `yaml:"pending_write_num"`
	Codec            string        `yaml:"codec"` // json|protobuf
	EnableTLS        bool          `yaml:"enable_tls"`
	EnableEncryption bool          `yaml:"enable_encryption"`
	SignerKey        string        `yaml:"signer_key"`
	HealthInterval   time.Duration `yaml:"health_interval"`
}

// GateSection 客户端网关配置
type GateSection struct {
	TCPAddr   string `yaml:"tcp_addr"`
	WSAddr    string `yaml:"ws_addr"`
	KCPAddr   string `yaml:"kcp_addr"`
	MaxMsgLen uint32 `yaml:"max_msg_len"`
	RateLimit int    `yaml:"rate_limit"` // msg/sec per conn
}

// DashboardSection 运维面板配置
type DashboardSection struct {
	Enabled bool   `yaml:"enabled"`
	Listen  string `yaml:"listen"`
	Token   string `yaml:"token"`
}

// LogSection 日志配置
type LogSection struct {
	Level  string `yaml:"level"` // debug|info|warn|error
	Format string `yaml:"format"` // text|json
	Path   string `yaml:"path"`   // 空表示 stdout
}

// MetricsSection 指标采集配置
type MetricsSection struct {
	Enabled bool   `yaml:"enabled"`
	Listen  string `yaml:"listen"`
	Path    string `yaml:"path"`
}

// DefaultEngineConfig 返回带默认值的配置实例
// 默认值用于 CLI 模板生成和字段回退
func DefaultEngineConfig() *EngineConfig {
	return &EngineConfig{
		Version: "1.0",
		NodeID:  "engine-node-1",
		Cluster: ClusterSection{
			Enabled:      false,
			Name:         "default",
			Seeds:        []string{},
			GossipPeriod: time.Second,
			Provider:     "static",
		},
		Remote: RemoteSection{
			Address:         "0.0.0.0:6000",
			MaxConnNum:      1000,
			PendingWriteNum: 100,
			Codec:           "json",
			HealthInterval:  10 * time.Second,
		},
		Gate: GateSection{
			TCPAddr:   "0.0.0.0:8000",
			WSAddr:    "0.0.0.0:8080",
			MaxMsgLen: 1024 * 1024,
			RateLimit: 0,
		},
		Dashboard: DashboardSection{
			Enabled: true,
			Listen:  "0.0.0.0:9090",
		},
		Log: LogSection{
			Level:  "info",
			Format: "text",
		},
		Metrics: MetricsSection{
			Enabled: true,
			Listen:  "0.0.0.0:9100",
			Path:    "/metrics",
		},
	}
}

// LoadEngineConfig 从文件加载引擎配置
// 顺序：默认值 → YAML 文件覆盖 → 环境变量覆盖 → 校验
func LoadEngineConfig(path string) (*EngineConfig, error) {
	cfg := DefaultEngineConfig()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read engine config %s: %w", path, err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse engine config %s: %w", path, err)
		}
	}
	if err := cfg.ApplyEnv(os.Environ()); err != nil {
		return nil, fmt.Errorf("apply env overrides: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// LoadEngineConfigFromBytes 从字节数组加载引擎配置（测试用）
func LoadEngineConfigFromBytes(data []byte) (*EngineConfig, error) {
	cfg := DefaultEngineConfig()
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse engine config: %w", err)
		}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// ApplyEnv 应用环境变量覆盖
// 映射规则：ENGINE_<SECTION>_<FIELD>，如 ENGINE_REMOTE_ADDRESS 覆盖 remote.address
// 支持的字段类型：string / int / uint / bool / time.Duration / []string (逗号分隔)
func (c *EngineConfig) ApplyEnv(env []string) error {
	kv := envToMap(env)

	set := func(prefix string, target map[string]any) error {
		for name, ptr := range target {
			key := "ENGINE_" + prefix + "_" + strings.ToUpper(name)
			v, ok := kv[key]
			if !ok || v == "" {
				continue
			}
			if err := applyEnvValue(ptr, v); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
		return nil
	}

	if v, ok := kv["ENGINE_NODE_ID"]; ok && v != "" {
		c.NodeID = v
	}
	if v, ok := kv["ENGINE_VERSION"]; ok && v != "" {
		c.Version = v
	}

	if err := set("CLUSTER", map[string]any{
		"ENABLED":       &c.Cluster.Enabled,
		"NAME":          &c.Cluster.Name,
		"SEEDS":         &c.Cluster.Seeds,
		"GOSSIP_PERIOD": &c.Cluster.GossipPeriod,
		"PROVIDER":      &c.Cluster.Provider,
	}); err != nil {
		return err
	}
	if err := set("REMOTE", map[string]any{
		"ADDRESS":            &c.Remote.Address,
		"MAX_CONN_NUM":       &c.Remote.MaxConnNum,
		"PENDING_WRITE_NUM":  &c.Remote.PendingWriteNum,
		"CODEC":              &c.Remote.Codec,
		"ENABLE_TLS":         &c.Remote.EnableTLS,
		"ENABLE_ENCRYPTION":  &c.Remote.EnableEncryption,
		"SIGNER_KEY":         &c.Remote.SignerKey,
		"HEALTH_INTERVAL":    &c.Remote.HealthInterval,
	}); err != nil {
		return err
	}
	if err := set("GATE", map[string]any{
		"TCP_ADDR":    &c.Gate.TCPAddr,
		"WS_ADDR":     &c.Gate.WSAddr,
		"KCP_ADDR":    &c.Gate.KCPAddr,
		"MAX_MSG_LEN": &c.Gate.MaxMsgLen,
		"RATE_LIMIT":  &c.Gate.RateLimit,
	}); err != nil {
		return err
	}
	if err := set("DASHBOARD", map[string]any{
		"ENABLED": &c.Dashboard.Enabled,
		"LISTEN":  &c.Dashboard.Listen,
		"TOKEN":   &c.Dashboard.Token,
	}); err != nil {
		return err
	}
	if err := set("LOG", map[string]any{
		"LEVEL":  &c.Log.Level,
		"FORMAT": &c.Log.Format,
		"PATH":   &c.Log.Path,
	}); err != nil {
		return err
	}
	if err := set("METRICS", map[string]any{
		"ENABLED": &c.Metrics.Enabled,
		"LISTEN":  &c.Metrics.Listen,
		"PATH":    &c.Metrics.Path,
	}); err != nil {
		return err
	}
	return nil
}

// Validate 校验必填字段与合法范围
func (c *EngineConfig) Validate() error {
	if c.NodeID == "" {
		return fmt.Errorf("node_id is required")
	}
	if c.Remote.Address == "" {
		return fmt.Errorf("remote.address is required")
	}
	if !strings.Contains(c.Remote.Address, ":") {
		return fmt.Errorf("remote.address must be in host:port form, got %q", c.Remote.Address)
	}
	if c.Remote.MaxConnNum < 0 {
		return fmt.Errorf("remote.max_conn_num must be >= 0, got %d", c.Remote.MaxConnNum)
	}
	if c.Remote.Codec != "" && c.Remote.Codec != "json" && c.Remote.Codec != "protobuf" {
		return fmt.Errorf("remote.codec must be 'json' or 'protobuf', got %q", c.Remote.Codec)
	}
	if c.Cluster.Enabled && c.Cluster.Name == "" {
		return fmt.Errorf("cluster.name is required when cluster.enabled=true")
	}
	if c.Cluster.Provider != "" {
		switch c.Cluster.Provider {
		case "static", "consul", "etcd", "k8s":
		default:
			return fmt.Errorf("cluster.provider must be one of static|consul|etcd|k8s, got %q", c.Cluster.Provider)
		}
	}
	switch c.Log.Level {
	case "", "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log.level must be debug|info|warn|error, got %q", c.Log.Level)
	}
	switch c.Log.Format {
	case "", "text", "json":
	default:
		return fmt.Errorf("log.format must be text|json, got %q", c.Log.Format)
	}
	if c.Gate.MaxMsgLen > 0 && c.Gate.MaxMsgLen < 64 {
		return fmt.Errorf("gate.max_msg_len too small, must be >= 64, got %d", c.Gate.MaxMsgLen)
	}
	return nil
}

// Marshal 序列化为 YAML 字节数组
func (c *EngineConfig) Marshal() ([]byte, error) {
	return yaml.Marshal(c)
}

// Save 写入到 YAML 文件
func (c *EngineConfig) Save(path string) error {
	data, err := c.Marshal()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// GenerateTemplate 生成带注释的默认 engine.yaml 模板
// 用于 `engine init` 命令
func GenerateTemplate() []byte {
	return []byte(`# Engine 统一配置文件 (engine.yaml)
# 对应 deploy/helm/engine/values.yaml，字段可被环境变量覆盖：
#   ENGINE_REMOTE_ADDRESS=0.0.0.0:7000  覆盖 remote.address
#   ENGINE_CLUSTER_ENABLED=true         覆盖 cluster.enabled
#   ENGINE_CLUSTER_SEEDS=a:1,b:2        覆盖 cluster.seeds (逗号分隔)

version: "1.0"
node_id: engine-node-1

cluster:
  enabled: false
  name: default
  seeds: []
  gossip_period: 1s
  provider: static  # static|consul|etcd|k8s

remote:
  address: 0.0.0.0:6000
  max_conn_num: 1000
  pending_write_num: 100
  codec: json         # json|protobuf
  enable_tls: false
  enable_encryption: false
  signer_key: ""
  health_interval: 10s

gate:
  tcp_addr: 0.0.0.0:8000
  ws_addr: 0.0.0.0:8080
  kcp_addr: ""
  max_msg_len: 1048576
  rate_limit: 0

dashboard:
  enabled: true
  listen: 0.0.0.0:9090
  token: ""

log:
  level: info         # debug|info|warn|error
  format: text        # text|json
  path: ""            # 空表示 stdout

metrics:
  enabled: true
  listen: 0.0.0.0:9100
  path: /metrics

custom: {}
`)
}

// envToMap 将 ["K=V", ...] 形式的环境变量转为映射
func envToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, e := range env {
		if idx := strings.Index(e, "="); idx > 0 {
			m[e[:idx]] = e[idx+1:]
		}
	}
	return m
}

// applyEnvValue 按 ptr 的静态类型解析 v 并赋值
func applyEnvValue(ptr any, v string) error {
	switch p := ptr.(type) {
	case *string:
		*p = v
	case *bool:
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("invalid bool %q", v)
		}
		*p = b
	case *int:
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid int %q", v)
		}
		*p = n
	case *uint32:
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid uint32 %q", v)
		}
		*p = uint32(n)
	case *time.Duration:
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("invalid duration %q", v)
		}
		*p = d
	case *[]string:
		if v == "" {
			*p = nil
			return nil
		}
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, s := range parts {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		*p = out
	default:
		return fmt.Errorf("unsupported type %T", ptr)
	}
	return nil
}
