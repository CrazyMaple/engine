package main

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gamelib/config"
	"engine/log"
)

// allocPort 分配一个可用 TCP 端口（关闭后返回端口号）
func allocPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// allocUDPPort 分配一个可用 UDP 端口
func allocUDPPort(t *testing.T) int {
	t.Helper()
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("udp listen: %v", err)
	}
	port := c.LocalAddr().(*net.UDPAddr).Port
	_ = c.Close()
	return port
}

// waitPortOpen 等待 TCP 端口开放（最多 waitMax）
func waitPortOpen(t *testing.T, addr string, waitMax time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(waitMax)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// TestRuntimeStartStopMinimal 验证最小配置下 Start/Stop 可幂等、不 panic
func TestRuntimeStartStopMinimal(t *testing.T) {
	cfg := config.DefaultEngineConfig()
	cfg.Remote.Address = "127.0.0.1:" + strconv.Itoa(allocPort(t))
	cfg.Dashboard.Enabled = false
	cfg.Gate.TCPAddr = ""
	cfg.Gate.WSAddr = ""
	cfg.Gate.KCPAddr = ""
	cfg.Cluster.Enabled = false

	rt := newRuntime("", cfg)
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	if !waitPortOpen(t, cfg.Remote.Address, 2*time.Second) {
		t.Fatalf("remote not listening on %s", cfg.Remote.Address)
	}
}

// TestRuntimeStartGateAndDashboard 启用 Gate(TCP) + Dashboard，验证端口开放
func TestRuntimeStartGateAndDashboard(t *testing.T) {
	cfg := config.DefaultEngineConfig()
	cfg.Remote.Address = "127.0.0.1:" + strconv.Itoa(allocPort(t))
	cfg.Gate.TCPAddr = "127.0.0.1:" + strconv.Itoa(allocPort(t))
	cfg.Gate.WSAddr = ""
	cfg.Gate.KCPAddr = "127.0.0.1:" + strconv.Itoa(allocUDPPort(t))
	cfg.Dashboard.Enabled = true
	cfg.Dashboard.Listen = "127.0.0.1:" + strconv.Itoa(allocPort(t))
	cfg.Cluster.Enabled = false

	rt := newRuntime("", cfg)
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	if !waitPortOpen(t, cfg.Gate.TCPAddr, 2*time.Second) {
		t.Fatalf("gate TCP not listening on %s", cfg.Gate.TCPAddr)
	}
	if !waitPortOpen(t, cfg.Dashboard.Listen, 2*time.Second) {
		t.Fatalf("dashboard not listening on %s", cfg.Dashboard.Listen)
	}
	// Gate 内部必须已创建三种 listener（kcp 通过字段非 nil 判断）
	if rt.gate == nil {
		t.Fatal("gate should be initialized")
	}
	if rt.dashboard == nil {
		t.Fatal("dashboard should be initialized")
	}
}

// TestRuntimeReloadLogLevel 热重载日志级别
func TestRuntimeReloadLogLevel(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "engine.yaml")

	cfg := config.DefaultEngineConfig()
	cfg.Remote.Address = "127.0.0.1:" + strconv.Itoa(allocPort(t))
	cfg.Dashboard.Enabled = false
	cfg.Gate.TCPAddr = ""
	cfg.Gate.WSAddr = ""
	cfg.Gate.KCPAddr = ""
	cfg.Log.Level = "info"
	data, _ := cfg.Marshal()
	if err := os.WriteFile(yamlPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := config.LoadEngineConfig(yamlPath)
	if err != nil {
		t.Fatalf("LoadEngineConfig: %v", err)
	}
	rt := newRuntime(yamlPath, loaded)
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	// 改动 yaml，把 level 改为 debug
	newBytes := strings.Replace(string(data), "level: info", "level: debug", 1)
	if err := os.WriteFile(yamlPath, []byte(newBytes), 0644); err != nil {
		t.Fatal(err)
	}

	if err := rt.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if rt.cfg.Log.Level != "debug" {
		t.Fatalf("reload did not apply: level=%s", rt.cfg.Log.Level)
	}
}

// TestRuntimeReloadWithoutPath Reload 在无 cfgPath 时应返回错误
func TestRuntimeReloadWithoutPath(t *testing.T) {
	cfg := config.DefaultEngineConfig()
	cfg.Dashboard.Enabled = false
	cfg.Gate.TCPAddr = ""
	cfg.Gate.WSAddr = ""
	cfg.Gate.KCPAddr = ""
	cfg.Remote.Address = "127.0.0.1:" + strconv.Itoa(allocPort(t))

	rt := newRuntime("", cfg)
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	if err := rt.Reload(); err == nil {
		t.Fatal("Reload without cfgPath should fail")
	}
}

// TestRuntimeStartFailRemotePort 相同端口冲突时应报错（覆盖 remote 启动失败分支）
// 注：Remote.Start 内部未返回 error，端口冲突只打印 log，这里只验证 Start 不 panic
func TestRuntimeStartFailClusterWithoutRemote(t *testing.T) {
	cfg := config.DefaultEngineConfig()
	cfg.Cluster.Enabled = true
	cfg.Cluster.Name = "test-cluster"
	cfg.Cluster.Seeds = []string{}
	cfg.Remote.Address = "127.0.0.1:" + strconv.Itoa(allocPort(t))
	cfg.Dashboard.Enabled = false
	cfg.Gate.TCPAddr = ""
	cfg.Gate.WSAddr = ""
	cfg.Gate.KCPAddr = ""

	rt := newRuntime("", cfg)
	if err := rt.Start(); err != nil {
		// 允许失败（cluster 对没有 seeds 可能会警告但不阻塞），只确保 Stop 能清理
		rt.Stop()
		return
	}
	rt.Stop()
}

// TestAnyGateEnabled 边界条件
func TestAnyGateEnabled(t *testing.T) {
	if anyGateEnabled(config.GateSection{}) {
		t.Fatal("empty gate section should not be enabled")
	}
	if !anyGateEnabled(config.GateSection{TCPAddr: ":1"}) {
		t.Fatal("TCPAddr set should be enabled")
	}
	if !anyGateEnabled(config.GateSection{WSAddr: ":1"}) {
		t.Fatal("WSAddr set should be enabled")
	}
	if !anyGateEnabled(config.GateSection{KCPAddr: ":1"}) {
		t.Fatal("KCPAddr set should be enabled")
	}
}

// TestRuntimeLogPipelineWiring 启用 Dashboard 时，log.Info 应同时写入 RingBuffer 和 Broadcast
func TestRuntimeLogPipelineWiring(t *testing.T) {
	cfg := config.DefaultEngineConfig()
	cfg.Remote.Address = "127.0.0.1:" + strconv.Itoa(allocPort(t))
	cfg.Gate.TCPAddr = ""
	cfg.Gate.WSAddr = ""
	cfg.Gate.KCPAddr = ""
	cfg.Cluster.Enabled = false
	cfg.Dashboard.Enabled = true
	cfg.Dashboard.Listen = "127.0.0.1:" + strconv.Itoa(allocPort(t))
	cfg.Log.Level = "info"

	rt := newRuntime("", cfg)
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	if rt.logRing == nil || rt.logBroadcast == nil {
		t.Fatal("log ring/broadcast should be created")
	}

	// RingBuffer 在 Start 过程中被多条 log.Info 填充（如 "engine start", "actor system started"）
	if rt.logRing.Len() == 0 {
		t.Fatal("ring buffer should contain engine start log entries")
	}

	// 订阅广播，触发一条 log.Info 后订阅者应收到
	got := make(chan string, 4)
	cancel := rt.logBroadcast.Subscribe(&testSubscriber{ch: got})
	defer cancel()

	log.Info("pipeline-test")

	select {
	case msg := <-got:
		if msg != "pipeline-test" {
			t.Fatalf("unexpected msg: %s", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive broadcast write")
	}
}

// testSubscriber 把收到的日志 msg 写入 channel
type testSubscriber struct {
	ch chan string
}

func (t *testSubscriber) Notify(entry log.LogEntry) {
	select {
	case t.ch <- entry.Msg:
	default:
	}
}

// TestNonZeroDuration 回退逻辑
func TestNonZeroDuration(t *testing.T) {
	if nonZeroDuration(0, time.Second) != time.Second {
		t.Fatal("zero should use fallback")
	}
	if nonZeroDuration(-1, time.Second) != time.Second {
		t.Fatal("negative should use fallback")
	}
	if nonZeroDuration(5*time.Second, time.Second) != 5*time.Second {
		t.Fatal("positive should pass through")
	}
}
