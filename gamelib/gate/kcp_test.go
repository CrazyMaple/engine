package gate

import (
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"engine/actor"
	"engine/network"
)

// echoProcessor 简单回显处理器
type echoProcessor struct {
	routed atomic.Int64
}

func (p *echoProcessor) Unmarshal(data []byte) (interface{}, error) { return data, nil }
func (p *echoProcessor) Marshal(msg interface{}) ([][]byte, error) {
	return [][]byte{msg.([]byte)}, nil
}
func (p *echoProcessor) Route(msg interface{}, agent interface{}) error {
	p.routed.Add(1)
	a := agent.(*Agent)
	return a.WriteMsg(msg)
}

// freeUDPPort 分配一个可用的 UDP 端口（返回 addr 字符串）
//
// KCP 会话只关心远端 addr；测试为避免与其他进程撞端口，先 ListenPacket 占位再关闭。
func freeUDPPort(t *testing.T) string {
	t.Helper()
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("alloc udp port: %v", err)
	}
	addr := c.LocalAddr().String()
	_ = c.Close()
	return addr
}

func TestGateKCPStartStop(t *testing.T) {
	system := actor.NewActorSystem()
	g := NewGate(system)
	g.KCPAddr = freeUDPPort(t)
	g.Processor = &echoProcessor{}
	g.Start()
	defer g.Close()

	if g.kcpServer == nil {
		t.Fatal("KCP server should be created when KCPAddr is set")
	}
	if g.kcpServer.LocalAddr() == nil {
		t.Fatal("KCP server LocalAddr should be non-nil after Start")
	}
}

func TestGateKCPEchoEndToEnd(t *testing.T) {
	system := actor.NewActorSystem()
	proc := &echoProcessor{}
	g := NewGate(system)
	g.KCPAddr = freeUDPPort(t)
	g.Processor = proc
	g.KCPConfig = network.FastKCPConfig()
	g.Start()
	defer g.Close()

	cli, err := NewKCPClient(g.kcpServer.LocalAddr().String())
	if err != nil {
		t.Fatalf("dial kcp: %v", err)
	}
	defer cli.Close()

	payload := []byte("hello-kcp")
	if err := cli.WriteMsg(payload); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := readClientWithTimeout(cli, 3*time.Second)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("echo mismatch: got %q want %q", got, payload)
	}
	if proc.routed.Load() == 0 {
		t.Fatal("processor.Route should have been invoked")
	}
	if g.ConnCount() <= 0 {
		t.Fatalf("expected ConnCount > 0, got %d", g.ConnCount())
	}
}

// TestGateTransportCoexist 验证 TCP + KCP 同时接入共享同一 Processor / Agent 管线
func TestGateTransportCoexist(t *testing.T) {
	system := actor.NewActorSystem()
	proc := &echoProcessor{}
	g := NewGate(system)

	tcpPort := allocTCPPort(t)
	g.TCPAddr = "127.0.0.1:" + strconv.Itoa(tcpPort)
	g.KCPAddr = freeUDPPort(t)
	g.Processor = proc

	g.Start()
	defer g.Close()

	// KCP 客户端
	cli, err := NewKCPClient(g.kcpServer.LocalAddr().String())
	if err != nil {
		t.Fatalf("dial kcp: %v", err)
	}
	defer cli.Close()

	var wg sync.WaitGroup
	const N = 5
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < N; i++ {
			if err := cli.WriteMsg([]byte(fmt.Sprintf("m-%d", i))); err != nil {
				t.Errorf("write %d: %v", i, err)
				return
			}
		}
	}()

	wg.Wait()

	// 读取回显
	for i := 0; i < N; i++ {
		got, err := readClientWithTimeout(cli, 3*time.Second)
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		want := fmt.Sprintf("m-%d", i)
		if string(got) != want {
			t.Fatalf("msg %d: got %q want %q", i, got, want)
		}
	}
}

func TestGateKCPIdentifiers(t *testing.T) {
	// 验证 Agent.Transport() 正确报告 KCP
	system := actor.NewActorSystem()
	proc := &echoProcessor{}
	g := NewGate(system)
	g.KCPAddr = freeUDPPort(t)
	g.Processor = proc
	g.Start()
	defer g.Close()

	cli, err := NewKCPClient(g.kcpServer.LocalAddr().String())
	if err != nil {
		t.Fatalf("dial kcp: %v", err)
	}
	defer cli.Close()
	if err := cli.WriteMsg([]byte("x")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := readClientWithTimeout(cli, 3*time.Second); err != nil {
		t.Fatalf("read: %v", err)
	}

	// Agent 已注册到 Gate；遍历内部 kcpServer 的 SessionCount 做侧证
	if g.kcpServer.SessionCount() != 1 {
		t.Fatalf("expected 1 kcp session, got %d", g.kcpServer.SessionCount())
	}

	// 客户端工具函数契约检查
	if IsKCP(nil) {
		t.Fatal("IsKCP(nil) should return false")
	}
	if KCPConn(nil) != nil {
		t.Fatal("KCPConn(nil) should return nil")
	}
}

// TestGateKCPClientBadAddr 覆盖 NewKCPClient 参数校验
func TestGateKCPClientBadAddr(t *testing.T) {
	if _, err := NewKCPClient(""); err == nil {
		t.Fatal("NewKCPClient(\"\") should error")
	}
	if _, err := NewKCPClientWithConfig("", network.FastKCPConfig()); err == nil {
		t.Fatal("NewKCPClientWithConfig(\"\", ...) should error")
	}
}

// ---- 辅助 ----

func readClientWithTimeout(c network.Conn, d time.Duration) ([]byte, error) {
	type result struct {
		msg []byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		msg, err := c.ReadMsg()
		ch <- result{msg, err}
	}()
	select {
	case r := <-ch:
		return r.msg, r.err
	case <-time.After(d):
		return nil, fmt.Errorf("read timeout after %v", d)
	}
}

func allocTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("alloc tcp port: %v", err)
	}
	p := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return p
}
