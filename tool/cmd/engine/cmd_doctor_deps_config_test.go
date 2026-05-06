package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const configFileWithAllDeprecated = `package cluster

import "time"

type ClusterConfig struct {
	ClusterName string
	Address     string

	// SeedNodes 种子节点列表。
	//
	// Deprecated: v1.13 起改用 Provider 注入。
	SeedNodes []string

	// GossipInterval Gossip 间隔。
	//
	// Deprecated: engine 不再内嵌 gossip。
	GossipInterval time.Duration

	// GossipFanOut 每轮 fanout。
	//
	// Deprecated: engine 不再内嵌 gossip。
	GossipFanOut int

	Provider Provider
}

// WithSeedNodes 设置种子节点。
//
// Deprecated: v1.13 起改用 WithProvider 注入。
func (c *ClusterConfig) WithSeedNodes(seeds ...string) *ClusterConfig {
	c.SeedNodes = seeds
	return c
}

// WithGossipInterval 设置 Gossip 间隔。
//
// Deprecated: engine 不再内嵌 gossip。
func (c *ClusterConfig) WithGossipInterval(d time.Duration) *ClusterConfig {
	c.GossipInterval = d
	return c
}

// WithProvider 不应被该规则要求标注。
func (c *ClusterConfig) WithProvider(p Provider) *ClusterConfig {
	c.Provider = p
	return c
}

type Provider interface{}
`

const configFileMissingFieldDeprecation = `package cluster

import "time"

type ClusterConfig struct {
	// SeedNodes 没标注 Deprecated。
	SeedNodes []string

	// GossipInterval 没标注 Deprecated。
	GossipInterval time.Duration
}

// WithSeedNodes 标注了 Deprecated。
//
// Deprecated: 改用 WithProvider。
func (c *ClusterConfig) WithSeedNodes(s ...string) *ClusterConfig { return c }

// WithGossipInterval 没标注 Deprecated。
func (c *ClusterConfig) WithGossipInterval(d time.Duration) *ClusterConfig { return c }
`

func writeConfigFile(t *testing.T, root, src string) {
	t.Helper()
	p := filepath.Join(root, "engine", "cluster", "config.go")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestConfigDeprecationAllAnnotated 全部标注时不应有违规
func TestConfigDeprecationAllAnnotated(t *testing.T) {
	root := t.TempDir()
	writeConfigFile(t, root, configFileWithAllDeprecated)
	vs, err := checkClusterConfigDeprecations(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Errorf("expected 0 violations, got %+v", vs)
	}
}

// TestConfigDeprecationMissingFields 字段未标注时报警
func TestConfigDeprecationMissingFields(t *testing.T) {
	root := t.TempDir()
	writeConfigFile(t, root, configFileMissingFieldDeprecation)
	vs, err := checkClusterConfigDeprecations(root)
	if err != nil {
		t.Fatal(err)
	}
	wantSubs := []string{"SeedNodes", "GossipInterval", "WithGossipInterval"}
	for _, w := range wantSubs {
		hit := false
		for _, v := range vs {
			if strings.Contains(v.Subject, w) {
				hit = true
				break
			}
		}
		if !hit {
			t.Errorf("expected violation for %q, got %+v", w, vs)
		}
	}
	// WithSeedNodes 已标注，不应出现
	for _, v := range vs {
		if strings.Contains(v.Subject, "WithSeedNodes") {
			t.Errorf("WithSeedNodes is annotated; should not violate, got %+v", v)
		}
	}
}

// TestConfigDeprecationMissingFile 文件不存在视为零违规（允许 Phase 4 删除路径）
func TestConfigDeprecationMissingFile(t *testing.T) {
	root := t.TempDir()
	vs, err := checkClusterConfigDeprecations(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Errorf("expected 0 violations when file absent, got %+v", vs)
	}
}

// TestConfigDeprecationRealConfig 在真实仓库根上跑当前 engine/cluster/config.go
// 应零违规（保证 Phase 4 反向门禁通过）。仅在能定位到仓库根时执行。
func TestConfigDeprecationRealConfig(t *testing.T) {
	// 当前测试运行目录在 tool/cmd/engine 下；仓库根再回退三层
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(cwd, "..", "..", "..")
	if _, err := os.Stat(filepath.Join(root, "engine", "cluster", "config.go")); err != nil {
		t.Skip("无法定位 engine/cluster/config.go，跳过真实仓库验证")
	}
	vs, err := checkClusterConfigDeprecations(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Errorf("real engine/cluster/config.go must keep all Gossip*/SeedNodes/With* deprecated annotations, got %+v", vs)
	}
}
