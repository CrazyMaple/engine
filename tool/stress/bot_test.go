package stress

import (
	"context"
	"encoding/json"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// --- Mock Connection for Testing ---

type mockBotConnection struct {
	sendCount int64
	closed    int32
}

func (c *mockBotConnection) Send(msgType string, payload interface{}) (int64, error) {
	atomic.AddInt64(&c.sendCount, 1)
	return 100, nil // 100us latency
}

func (c *mockBotConnection) Recv(timeout time.Duration) (string, json.RawMessage, error) {
	time.Sleep(timeout)
	return "", nil, context.DeadlineExceeded
}

func (c *mockBotConnection) Close() error {
	atomic.StoreInt32(&c.closed, 1)
	return nil
}

func (c *mockBotConnection) RemoteAddr() string { return "mock:1234" }

func mockConnector(addr string) (BotConnection, error) {
	return &mockBotConnection{}, nil
}

// --- IdleBot Tests ---

func TestIdleBot(t *testing.T) {
	bot := &ManagedBot{
		ID:        0,
		Lifecycle: &IdleBot{},
		Metrics:   NewMetrics(),
		addr:      "localhost:0",
		connFn:    mockConnector,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := bot.Run(ctx)
	if err != nil {
		t.Fatalf("IdleBot.Run: %v", err)
	}
	if bot.GetState() != BotStateStopped {
		t.Fatalf("expected state=stopped, got %v", bot.GetState())
	}
}

// --- ActiveBot Tests ---

func TestActiveBot(t *testing.T) {
	metrics := NewMetrics()
	bot := &ManagedBot{
		ID: 0,
		Lifecycle: &ActiveBot{
			MsgType:  "ping",
			Payload:  map[string]interface{}{"seq": 1},
			Interval: 50 * time.Millisecond,
			Metrics:  metrics,
		},
		Metrics: metrics,
		addr:    "localhost:0",
		connFn:  mockConnector,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	err := bot.Run(ctx)
	if err != nil {
		t.Fatalf("ActiveBot.Run: %v", err)
	}

	total := metrics.TotalRequests.Load()
	if total < 2 {
		t.Fatalf("expected at least 2 requests from ActiveBot, got %d", total)
	}
}

// --- StressBot Tests ---

func TestStressBot(t *testing.T) {
	metrics := NewMetrics()
	bot := &ManagedBot{
		ID: 0,
		Lifecycle: &StressBot{
			MsgType: "flood",
			Metrics: metrics,
		},
		Metrics: metrics,
		addr:    "localhost:0",
		connFn:  mockConnector,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := bot.Run(ctx)
	if err != nil {
		t.Fatalf("StressBot.Run: %v", err)
	}

	total := metrics.TotalRequests.Load()
	if total < 10 {
		t.Fatalf("expected many requests from StressBot, got %d", total)
	}
}

// --- SequenceBot Tests ---

func TestSequenceBot(t *testing.T) {
	metrics := NewMetrics()
	bot := &ManagedBot{
		ID: 0,
		Lifecycle: &SequenceBot{
			Actions: []BotSequenceAction{
				{Type: "send", MsgType: "login", Payload: map[string]interface{}{"user": "test"}},
				{Type: "wait", Delay: 30 * time.Millisecond},
				{Type: "send", MsgType: "action", Payload: map[string]interface{}{"cmd": "move"}},
				{Type: "random_wait", MinWait: 10 * time.Millisecond, MaxWait: 50 * time.Millisecond},
			},
			Metrics: metrics,
		},
		Metrics: metrics,
		addr:    "localhost:0",
		connFn:  mockConnector,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err := bot.Run(ctx)
	if err != nil {
		t.Fatalf("SequenceBot.Run: %v", err)
	}

	total := metrics.TotalRequests.Load()
	if total < 2 {
		t.Fatalf("expected at least 2 requests, got %d", total)
	}
}

// --- BotBuilder Tests ---

func TestBotBuilder(t *testing.T) {
	builder := NewBotBuilder().
		Target("localhost:12345").
		WithLifecycle(&IdleBot{}).
		WithConnector(mockConnector)

	bot := builder.Build(42)
	if bot.ID != 42 {
		t.Fatalf("expected ID=42, got %d", bot.ID)
	}
	if bot.Name != "bot-42" {
		t.Fatalf("expected Name='bot-42', got %s", bot.Name)
	}
	if bot.State != BotStateIdle {
		t.Fatalf("expected initial state=idle, got %v", bot.State)
	}
}

// --- BotPool Tests ---

func TestBotPool_SpawnAndRun(t *testing.T) {
	builder := NewBotBuilder().
		Target("localhost:0").
		WithLifecycle(&IdleBot{}).
		WithConnector(mockConnector)

	pool := NewBotPool(builder)
	pool.Spawn(5)

	if pool.BotCount() != 5 {
		t.Fatalf("expected 5 bots, got %d", pool.BotCount())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	pool.StartAll(ctx, 0)
	pool.Wait()

	stats := pool.Stats()
	if stats.TotalCreated != 5 {
		t.Fatalf("expected TotalCreated=5, got %d", stats.TotalCreated)
	}
	if stats.TotalFinished != 5 {
		t.Fatalf("expected TotalFinished=5, got %d", stats.TotalFinished)
	}
}

func TestBotPool_StopAll(t *testing.T) {
	builder := NewBotBuilder().
		Target("localhost:0").
		WithLifecycle(&IdleBot{}).
		WithConnector(mockConnector)

	pool := NewBotPool(builder)
	pool.Spawn(3)

	ctx := context.Background()
	pool.StartAll(ctx, 0)

	// 等待 Bot 进入 playing 状态
	time.Sleep(100 * time.Millisecond)

	stats := pool.Stats()
	if stats.Playing < 1 {
		t.Logf("stats: %s", stats)
	}

	pool.StopAll()

	stats = pool.Stats()
	if stats.TotalFinished != 3 {
		t.Fatalf("expected all finished, got %d", stats.TotalFinished)
	}
}

func TestBotPool_Report(t *testing.T) {
	sharedMetrics := NewMetrics()
	builder := NewBotBuilder().
		Target("localhost:0").
		WithLifecycle(&ActiveBot{
			MsgType:  "test",
			Interval: 20 * time.Millisecond,
			Metrics:  sharedMetrics,
		}).
		WithConnector(mockConnector).
		WithMetrics(sharedMetrics)

	pool := NewBotPool(builder)
	pool.metrics = sharedMetrics // ensure pool uses the same metrics as lifecycle
	pool.Spawn(2)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	pool.StartAll(ctx, 0)
	pool.Wait()

	report := pool.Report("test scenario")
	if report.Scenario != "test scenario" {
		t.Fatalf("expected scenario name, got %s", report.Scenario)
	}
	if report.TotalRequests == 0 {
		t.Fatal("expected some requests in report")
	}
}

func TestBotPool_Stats_String(t *testing.T) {
	stats := BotPoolStats{
		TotalCreated: 10,
		Running:      true,
		Playing:      5,
		Idle:         3,
		Errored:      2,
	}
	s := stats.String()
	if s == "" {
		t.Fatal("expected non-empty stats string")
	}
}

// --- TCP Connection Test (with echo server) ---

func TestTCPBotConnection(t *testing.T) {
	// 启动 echo 服务器
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					// Echo back as a response
					c.Write(buf[:n])
				}
			}(conn)
		}
	}()

	conn, err := defaultTCPConnector(l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	lat, err := conn.Send("test_msg", map[string]interface{}{"key": "value"})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if lat <= 0 {
		t.Fatal("expected positive latency")
	}

	if conn.RemoteAddr() == "" {
		t.Fatal("expected non-empty remote addr")
	}
}
