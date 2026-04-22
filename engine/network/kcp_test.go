package network

import (
	"bytes"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// echoAgent 简单回显 Agent
type echoAgent struct {
	conn   *KCPConn
	closed chan struct{}
}

func newEchoAgent(c *KCPConn) Agent {
	return &echoAgent{conn: c, closed: make(chan struct{})}
}

func (a *echoAgent) Run() {
	for {
		msg, err := a.conn.ReadMsg()
		if err != nil {
			return
		}
		if err := a.conn.WriteMsg(msg); err != nil {
			return
		}
	}
}

func (a *echoAgent) OnClose() {
	close(a.closed)
}

// startKCPServer 启动一个动态端口 KCP 服务器
func startKCPServer(t *testing.T, cfg KCPConfig, newAgent func(*KCPConn) Agent) *KCPServer {
	t.Helper()
	if cfg.Interval == 0 {
		cfg = FastKCPConfig()
	}
	srv := &KCPServer{
		Addr:       "127.0.0.1:0",
		MaxConnNum: 64,
		Config:     cfg,
		NewAgent:   newAgent,
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("KCPServer start: %v", err)
	}
	return srv
}

func TestKCPEncodeDecode(t *testing.T) {
	in := &kcpSegment{
		conv: 0xCAFEBABE,
		cmd:  kcpCmdPush,
		sn:   42,
		una:  10,
		data: []byte("hello kcp"),
	}
	buf := encodeKCPSegment(in)
	out, err := decodeKCPSegment(buf)
	if err != nil {
		t.Fatal(err)
	}
	if out.conv != in.conv || out.cmd != in.cmd || out.sn != in.sn || out.una != in.una {
		t.Fatalf("header mismatch: got %+v want %+v", out, in)
	}
	if !bytes.Equal(out.data, in.data) {
		t.Fatalf("payload mismatch: got %s", out.data)
	}
}

func TestKCPDecodeShort(t *testing.T) {
	if _, err := decodeKCPSegment([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected error on short buffer")
	}
}

func TestKCPEchoBasic(t *testing.T) {
	srv := startKCPServer(t, FastKCPConfig(), newEchoAgent)
	defer srv.Close()

	cli, err := DialKCP(srv.LocalAddr().String(), FastKCPConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	payload := []byte("ping")
	if err := cli.WriteMsg(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := readWithTimeout(cli, 2*time.Second)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("echo mismatch: got %q want %q", got, payload)
	}
}

func TestKCPMultipleMessages(t *testing.T) {
	srv := startKCPServer(t, FastKCPConfig(), newEchoAgent)
	defer srv.Close()

	cli, err := DialKCP(srv.LocalAddr().String(), FastKCPConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	const N = 20
	for i := 0; i < N; i++ {
		msg := []byte(fmt.Sprintf("msg-%03d", i))
		if err := cli.WriteMsg(msg); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	for i := 0; i < N; i++ {
		want := []byte(fmt.Sprintf("msg-%03d", i))
		got, err := readWithTimeout(cli, 3*time.Second)
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("msg %d: got %q want %q", i, got, want)
		}
	}
}

func TestKCPMultipleClients(t *testing.T) {
	srv := startKCPServer(t, FastKCPConfig(), newEchoAgent)
	defer srv.Close()

	const clients = 5
	const msgsPerClient = 10

	var wg sync.WaitGroup
	var failures int32
	for c := 0; c < clients; c++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cli, err := DialKCP(srv.LocalAddr().String(), FastKCPConfig())
			if err != nil {
				atomic.AddInt32(&failures, 1)
				return
			}
			defer cli.Close()
			for i := 0; i < msgsPerClient; i++ {
				msg := []byte(fmt.Sprintf("c%d-m%d", idx, i))
				if err := cli.WriteMsg(msg); err != nil {
					atomic.AddInt32(&failures, 1)
					return
				}
				got, err := readWithTimeout(cli, 3*time.Second)
				if err != nil || !bytes.Equal(got, msg) {
					atomic.AddInt32(&failures, 1)
					return
				}
			}
		}(c)
	}
	wg.Wait()
	if failures > 0 {
		t.Fatalf("failures = %d", failures)
	}
	// 等待会话清理
	deadline := time.Now().Add(2 * time.Second)
	for srv.SessionCount() > 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
}

func TestKCPMessageExceedsMTU(t *testing.T) {
	srv := startKCPServer(t, FastKCPConfig(), newEchoAgent)
	defer srv.Close()

	cli, err := DialKCP(srv.LocalAddr().String(), FastKCPConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	huge := make([]byte, FastKCPConfig().Mtu+1)
	if err := cli.WriteMsg(huge); err == nil {
		t.Fatal("expected MTU error")
	}
}

func TestKCPCloseUnblocksRead(t *testing.T) {
	srv := startKCPServer(t, FastKCPConfig(), newEchoAgent)
	defer srv.Close()

	cli, err := DialKCP(srv.LocalAddr().String(), FastKCPConfig())
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := cli.ReadMsg()
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cli.Close()
	select {
	case <-done:
		// 关闭后 ReadMsg 应返回
	case <-time.After(1 * time.Second):
		t.Fatal("ReadMsg did not unblock after Close")
	}
}

func TestKCPDefaultConfigs(t *testing.T) {
	d := DefaultKCPConfig()
	if d.Interval == 0 || d.Mtu == 0 || d.SendWindow == 0 {
		t.Fatal("DefaultKCPConfig has zero fields")
	}
	f := FastKCPConfig()
	if f.NoDelay != 1 || f.Resend == 0 {
		t.Fatal("FastKCPConfig should enable nodelay+resend")
	}
}

// readWithTimeout 在独立 goroutine 中调用 ReadMsg，避免测试卡死
func readWithTimeout(c *KCPConn, d time.Duration) ([]byte, error) {
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
