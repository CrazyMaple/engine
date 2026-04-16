package testkit

import (
	"fmt"
	"net"
	"testing"
	"time"

	"engine/actor"
)

// --- ClusterTestKit Tests ---

func TestClusterTestKit_StartNodes(t *testing.T) {
	tk := NewClusterTestKit(t)
	nodes := tk.StartNodes(3)

	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}
	if tk.NodeCount() != 3 {
		t.Fatalf("expected NodeCount=3, got %d", tk.NodeCount())
	}

	for i, node := range nodes {
		if node.System == nil {
			t.Fatalf("node %d: system is nil", i)
		}
		if node.Address == "" {
			t.Fatalf("node %d: address is empty", i)
		}
		// 验证端口可访问
		conn, err := net.DialTimeout("tcp", node.Address, time.Second)
		if err != nil {
			t.Fatalf("node %d: cannot connect to %s: %v", i, node.Address, err)
		}
		conn.Close()
	}
}

func TestClusterTestKit_NodeAccess(t *testing.T) {
	tk := NewClusterTestKit(t)
	tk.StartNodes(2)

	n0 := tk.Node(0)
	n1 := tk.Node(1)
	if n0 == n1 {
		t.Fatal("node 0 and 1 should be different instances")
	}
	if n0.Address == n1.Address {
		t.Fatal("nodes should have different addresses")
	}
}

func TestClusterTestKit_SpawnOnNode(t *testing.T) {
	tk := NewClusterTestKit(t)
	nodes := tk.StartNodes(1)

	props := actor.PropsFromFunc(actor.ActorFunc(func(ctx actor.Context) {}))
	pid := tk.SpawnOnNode(nodes[0], props)
	if pid == nil {
		t.Fatal("expected non-nil PID")
	}
}

// --- FaultProxy Tests ---

func TestFaultProxy_BasicRelay(t *testing.T) {
	// 启动一个简单的 echo TCP 服务器
	echoServer, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echoServer.Close()

	go func() {
		for {
			conn, err := echoServer.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					c.Write(buf[:n])
				}
			}(conn)
		}
	}()

	proxy := NewFaultProxyAuto(echoServer.Addr().String())
	if err := proxy.Start(); err != nil {
		t.Fatal(err)
	}
	defer proxy.Stop()

	// 通过代理连接
	conn, err := net.Dial("tcp", proxy.ListenAddr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// 发送数据并验证回显
	msg := []byte("hello proxy")
	conn.Write(msg)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "hello proxy" {
		t.Fatalf("expected 'hello proxy', got %q", string(buf[:n]))
	}
}

func TestFaultProxy_Partition(t *testing.T) {
	echoServer, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echoServer.Close()

	go func() {
		for {
			conn, err := echoServer.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					c.Write(buf[:n])
				}
			}(conn)
		}
	}()

	proxy := NewFaultProxyAuto(echoServer.Addr().String())
	if err := proxy.Start(); err != nil {
		t.Fatal(err)
	}
	defer proxy.Stop()

	// 开启网络分区
	proxy.SetPartitioned(true)

	// 分区后连接应该失败或无法读写
	conn, err := net.DialTimeout("tcp", proxy.ListenAddr(), 500*time.Millisecond)
	if err != nil {
		// 连接被拒绝（代理关闭了连接），验证分区有效
		return
	}
	defer conn.Close()

	// 即使连接成功，写入后也不应收到回复
	conn.Write([]byte("test"))
	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 1024)
	_, readErr := conn.Read(buf)
	if readErr == nil {
		t.Fatal("expected read error during partition, but succeeded")
	}
}

func TestFaultProxy_Latency(t *testing.T) {
	echoServer, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echoServer.Close()

	go func() {
		for {
			conn, err := echoServer.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					c.Write(buf[:n])
				}
			}(conn)
		}
	}()

	proxy := NewFaultProxyAuto(echoServer.Addr().String())
	if err := proxy.Start(); err != nil {
		t.Fatal(err)
	}
	defer proxy.Stop()

	// 注入 100ms 延迟
	proxy.SetLatency(100 * time.Millisecond)

	conn, err := net.Dial("tcp", proxy.ListenAddr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	start := time.Now()
	conn.Write([]byte("latency test"))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 1024)
	_, err = conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)

	// 延迟应至少 100ms（单向延迟注入）
	if elapsed < 80*time.Millisecond {
		t.Fatalf("expected at least 80ms delay, got %v", elapsed)
	}
}

func TestFaultInjector(t *testing.T) {
	fi := NewFaultInjector()

	// 创建两个代理（不启动，仅测试管理逻辑）
	p1 := &FaultProxy{listenAddr: "127.0.0.1:0", targetAddr: "127.0.0.1:0"}
	p2 := &FaultProxy{listenAddr: "127.0.0.1:0", targetAddr: "127.0.0.1:0"}

	fi.AddProxy("a->b", p1)
	fi.AddProxy("b->a", p2)

	fi.InjectLatency(50 * time.Millisecond)
	if p1.latency != 50*time.Millisecond {
		t.Fatal("expected latency injected")
	}
	if p2.latency != 50*time.Millisecond {
		t.Fatal("expected latency injected")
	}

	fi.PartitionAll()
	if !p1.partitioned || !p2.partitioned {
		t.Fatal("expected all partitioned")
	}

	fi.HealAll()
	if p1.partitioned || p2.partitioned {
		t.Fatal("expected all healed")
	}
	if p1.latency != 0 || p2.latency != 0 {
		t.Fatal("expected latency reset")
	}
}

// --- Scenario Tests ---

func TestScenario_BasicSteps(t *testing.T) {
	counter := 0

	NewScenario(t, "basic steps").
		Step("step 1", func(ctx *ScenarioContext) error {
			counter++
			ctx.Set("count", counter)
			return nil
		}).
		Step("step 2", func(ctx *ScenarioContext) error {
			v, ok := ctx.Get("count")
			if !ok {
				return fmt.Errorf("key 'count' not found")
			}
			if v.(int) != 1 {
				return fmt.Errorf("expected count=1, got %v", v)
			}
			counter++
			return nil
		}).
		Run()

	if counter != 2 {
		t.Fatalf("expected counter=2, got %d", counter)
	}
}

func TestScenario_WithNodes(t *testing.T) {
	ctk := NewClusterTestKit(t)
	nodes := ctk.StartNodes(2)

	NewScenario(t, "multi-node spawn").
		WithNodes(nodes).
		Step("spawn actors", func(ctx *ScenarioContext) error {
			props := actor.PropsFromFunc(actor.ActorFunc(func(c actor.Context) {}))
			pid := ctx.Node(0).System.Root.Spawn(props)
			ctx.StorePID("actor1", pid)
			return nil
		}).
		Verify("actor exists", func(ctx *ScenarioContext) error {
			pid := ctx.GetPID("actor1")
			if pid == nil {
				return fmt.Errorf("actor1 PID not found")
			}
			return nil
		}).
		Run()
}

func TestScenario_RepeatStep(t *testing.T) {
	count := 0
	NewScenario(t, "repeat").
		Step("repeat 5 times", RepeatStep(5, func(ctx *ScenarioContext, i int) error {
			count++
			return nil
		})).
		Run()

	if count != 5 {
		t.Fatalf("expected count=5, got %d", count)
	}
}

func TestScenario_ParallelStep(t *testing.T) {
	var mu = &testing.T{}
	_ = mu // just to show it's testing context

	results := make([]bool, 3)
	NewScenario(t, "parallel").
		Step("parallel work", ParallelStep(
			func(ctx *ScenarioContext) error { results[0] = true; return nil },
			func(ctx *ScenarioContext) error { results[1] = true; return nil },
			func(ctx *ScenarioContext) error { results[2] = true; return nil },
		)).
		Verify("all completed", func(ctx *ScenarioContext) error {
			for i, r := range results {
				if !r {
					return fmt.Errorf("parallel action %d did not complete", i)
				}
			}
			return nil
		}).
		Run()
}

func TestScenario_Timeout(t *testing.T) {
	// 此测试验证超时机制工作正常
	// 不实际运行超时场景（会导致 t.Fatal）
	// 仅验证正常完成的步骤不会超时
	NewScenario(t, "no timeout").
		StepWithTimeout("fast step", 5*time.Second, func(ctx *ScenarioContext) error {
			time.Sleep(10 * time.Millisecond)
			return nil
		}).
		Run()
}
