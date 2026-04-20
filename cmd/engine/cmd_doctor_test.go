package main

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"engine/config"
)

func TestVersionLess(t *testing.T) {
	cases := []struct {
		have, min string
		want      bool
	}{
		{"go1.24.0", "go1.24", false},
		{"go1.23.5", "go1.24", true},
		{"go1.25rc1", "go1.24", false},
		{"go1.24.3", "go1.24.4", true},
		{"go1.30", "go1.24", false},
	}
	for _, c := range cases {
		if got := versionLess(c.have, c.min); got != c.want {
			t.Errorf("versionLess(%q, %q) = %v, want %v", c.have, c.min, got, c.want)
		}
	}
}

func TestProbeListenTCP(t *testing.T) {
	// 先占用一个端口，再用 probeListen 探测该端口应该返回错误
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("prepare listener: %v", err)
	}
	defer ln.Close()

	if err := probeListen("tcp", ln.Addr().String()); err == nil {
		t.Fatalf("期望端口被占用时 probeListen 返回 error，但得到 nil")
	}

	// 关闭后应可监听
	addr := ln.Addr().String()
	ln.Close()
	if err := probeListen("tcp", addr); err != nil {
		t.Fatalf("端口释放后应能监听: %v", err)
	}
}

func TestStripSchemeAndMongoHost(t *testing.T) {
	if got := stripScheme("http://consul:8500/v1/foo"); got != "consul:8500" {
		t.Errorf("stripScheme http = %q", got)
	}
	if got := stripScheme("mongodb://a:b@host:27017/db"); got != "a:b@host:27017" {
		t.Errorf("stripScheme mongodb = %q", got)
	}
	if got := extractMongoHost("mongodb://user:pw@10.0.0.1:27018,10.0.0.2:27018/appdb?ssl=true"); got != "10.0.0.1:27018" {
		t.Errorf("extractMongoHost = %q", got)
	}
	if got := extractMongoHost("mongodb://host"); got != "host:27017" {
		t.Errorf("extractMongoHost default port = %q", got)
	}
}

func TestCheckRuntime(t *testing.T) {
	g := checkRuntime()
	if g.Name != "Runtime" {
		t.Fatalf("group name = %q", g.Name)
	}
	if len(g.Items) < 3 {
		t.Fatalf("expected >=3 items, got %d", len(g.Items))
	}
}

func TestCheckConfigMissing(t *testing.T) {
	tmp := t.TempDir()
	cfg, g := checkConfig(filepath.Join(tmp, "no-such.yaml"))
	if cfg == nil {
		t.Fatal("cfg must be defaulted even when missing")
	}
	if g.Items[0].Status != stWarn {
		t.Errorf("missing file should be warn, got %s", g.Items[0].Status)
	}
}

func TestCheckConfigValid(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "engine.yaml")
	if err := os.WriteFile(path, config.GenerateTemplate(), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, g := checkConfig(path)
	if cfg == nil {
		t.Fatal("cfg nil")
	}
	for _, it := range g.Items {
		if it.Status == stFail {
			t.Errorf("unexpected fail: %s %s", it.Name, it.Detail)
		}
	}
}

func TestCheckConfigInvalid(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "bad.yaml")
	if err := os.WriteFile(path, []byte("remote:\n  address: \"\"\nnode_id: x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, g := checkConfig(path)
	var hasFail bool
	for _, it := range g.Items {
		if it.Status == stFail {
			hasFail = true
		}
	}
	if !hasFail {
		t.Error("invalid config should produce fail item")
	}
}

func TestCheckServicesStatic(t *testing.T) {
	cfg := config.DefaultEngineConfig()
	cfg.Cluster.Enabled = true
	cfg.Cluster.Provider = "static"
	cfg.Cluster.Seeds = []string{"127.0.0.1:1"} // 基本不可达
	g := checkServices(cfg, 200*time.Millisecond)
	if len(g.Items) < 2 {
		t.Fatalf("expected provider + seed item; got %d", len(g.Items))
	}
	seedItem := g.Items[len(g.Items)-1]
	if seedItem.Status != stFail {
		t.Errorf("unreachable seed should be fail, got %s", seedItem.Status)
	}
}

func TestCheckDiskCwd(t *testing.T) {
	cfg := config.DefaultEngineConfig()
	g := checkDisk(cfg)
	if len(g.Items) == 0 {
		t.Fatal("disk should at least report cwd")
	}
}

func TestHumanBytes(t *testing.T) {
	if humanBytes(512) != "512 B" {
		t.Errorf("bytes = %s", humanBytes(512))
	}
	if humanBytes(1536) != "1.5 KiB" {
		t.Errorf("kib = %s", humanBytes(1536))
	}
	if humanBytes(2*1024*1024) != "2.0 MiB" {
		t.Errorf("mib = %s", humanBytes(2*1024*1024))
	}
}
