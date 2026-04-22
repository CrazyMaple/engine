package stress

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"engine/actor"
	"gamelib/gate"
)

// TestStress10KConnections 模拟万人同服压力测试
// 启动一个 Gate 网关，然后发起 10000 个并发 TCP 连接，
// 每个连接发送一条消息并保持 3 秒
func TestStress10KConnections(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	const (
		totalConns = 10000
		holdTime   = 3 * time.Second
		timeout    = 30 * time.Second
	)

	// 创建 ActorSystem 和 Gate
	system := actor.NewActorSystem()
	g := gate.NewGate(system)
	g.TCPAddr = "127.0.0.1:0" // 让系统分配端口
	g.MaxConnNum = totalConns + 1000
	g.PendingWriteNum = 100
	g.LenMsgLen = 2
	g.Processor = &echoProcessor{}

	g.Start()
	defer g.Close()

	// 获取实际监听地址
	// 由于 Gate 不暴露 listener 地址，我们用固定端口重新启动
	g.Close()
	g.TCPAddr = "127.0.0.1:19876"
	g.Start()
	defer g.Close()

	// 等待服务器就绪
	time.Sleep(100 * time.Millisecond)

	addr := "127.0.0.1:19876"

	var (
		connected atomic.Int64
		failed    atomic.Int64
		msgSent   atomic.Int64
		wg        sync.WaitGroup
	)

	start := time.Now()

	// 分批发起连接，避免一次性文件描述符耗尽
	batchSize := 500
	for batch := 0; batch < totalConns; batch += batchSize {
		remaining := totalConns - batch
		if remaining > batchSize {
			remaining = batchSize
		}

		for i := 0; i < remaining; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
				if err != nil {
					failed.Add(1)
					return
				}
				defer conn.Close()
				connected.Add(1)

				// 发送一条消息（2 字节长度 + 数据）
				msg := []byte("ping")
				header := make([]byte, 2)
				binary.BigEndian.PutUint16(header, uint16(len(msg)))
				conn.Write(header)
				conn.Write(msg)
				msgSent.Add(1)

				// 保持连接
				time.Sleep(holdTime)
			}()
		}

		// 小间隔避免 SYN 风暴
		time.Sleep(50 * time.Millisecond)
	}

	// 等待所有连接完成
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatal("stress test timed out")
	}

	elapsed := time.Since(start)
	connRate := float64(connected.Load()) / elapsed.Seconds()

	t.Logf("=== 压力测试结果 ===")
	t.Logf("目标连接数: %d", totalConns)
	t.Logf("成功连接: %d", connected.Load())
	t.Logf("失败连接: %d", failed.Load())
	t.Logf("发送消息: %d", msgSent.Load())
	t.Logf("总耗时: %v", elapsed)
	t.Logf("连接速率: %.0f conn/s", connRate)

	// 至少 90% 的连接应成功
	minExpected := int64(float64(totalConns) * 0.9)
	if connected.Load() < minExpected {
		t.Errorf("连接成功率过低: %d/%d (期望至少 %d)", connected.Load(), totalConns, minExpected)
	}
}

// echoProcessor 简单的回显处理器
type echoProcessor struct{}

func (p *echoProcessor) Unmarshal(data []byte) (interface{}, error) {
	return string(data), nil
}

func (p *echoProcessor) Marshal(msg interface{}) ([][]byte, error) {
	return [][]byte{[]byte(fmt.Sprint(msg))}, nil
}

func (p *echoProcessor) Route(msg interface{}, agent interface{}) error {
	return nil
}

// TestStressMessageThroughput 测试消息吞吐量
func TestStressMessageThroughput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	const (
		actorCount = 100
		msgPerActor = 10000
	)

	system := actor.NewActorSystem()
	var totalProcessed atomic.Int64
	var wg sync.WaitGroup

	// 创建多个 Actor
	pids := make([]*actor.PID, actorCount)
	for i := 0; i < actorCount; i++ {
		wg.Add(msgPerActor)
		props := actor.PropsFromFunc(func(ctx actor.Context) {
			switch ctx.Message().(type) {
			case int:
				totalProcessed.Add(1)
				wg.Done()
			}
		})
		pids[i] = system.Root.SpawnNamed(props, fmt.Sprintf("stress-actor-%d", i))
	}

	start := time.Now()

	// 发送消息
	for i := 0; i < actorCount; i++ {
		go func(pid *actor.PID) {
			for j := 0; j < msgPerActor; j++ {
				system.Root.Send(pid, j)
			}
		}(pids[i])
	}

	// 等待所有消息处理完成
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatalf("timeout, processed: %d/%d", totalProcessed.Load(), int64(actorCount*msgPerActor))
	}

	elapsed := time.Since(start)
	total := totalProcessed.Load()
	throughput := float64(total) / elapsed.Seconds()

	t.Logf("=== 消息吞吐量测试 ===")
	t.Logf("Actor 数量: %d", actorCount)
	t.Logf("每 Actor 消息数: %d", msgPerActor)
	t.Logf("总消息数: %d", total)
	t.Logf("总耗时: %v", elapsed)
	t.Logf("吞吐量: %.0f msg/s", throughput)
}
