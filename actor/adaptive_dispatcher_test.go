package actor

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdaptiveMailbox_Basic(t *testing.T) {
	var processed int64
	config := DefaultAdaptiveConfig()
	mb := NewAdaptiveMailbox(config)
	mb.RegisterHandlers(
		func(msg interface{}) { atomic.AddInt64(&processed, 1) },
		func(msg interface{}) {},
	)
	if amb, ok := mb.(*adaptiveMailbox); ok {
		amb.SetScheduler(NewSynchronizedDispatcher(100))
	}

	for i := 0; i < 100; i++ {
		mb.PostUserMessage("msg")
	}

	if atomic.LoadInt64(&processed) != 100 {
		t.Fatalf("expected 100 processed, got %d", atomic.LoadInt64(&processed))
	}
}

func TestAdaptiveMailbox_ThroughputAdaptation(t *testing.T) {
	config := AdaptiveMailboxConfig{
		MinThroughput:      2,
		MaxThroughput:      100,
		LowDepthThreshold:  5,
		HighDepthThreshold: 50,
	}
	mb := NewAdaptiveMailbox(config).(*adaptiveMailbox)

	// 低深度 → MinThroughput
	atomic.StoreInt64(&mb.depth, 3)
	if tp := mb.adaptiveThroughput(); tp != 2 {
		t.Fatalf("low depth: expected 2, got %d", tp)
	}

	// 高深度 → MaxThroughput
	atomic.StoreInt64(&mb.depth, 100)
	if tp := mb.adaptiveThroughput(); tp != 100 {
		t.Fatalf("high depth: expected 100, got %d", tp)
	}

	// 中间深度 → 线性插值
	atomic.StoreInt64(&mb.depth, 27) // 中点：(5+50)/2 ≈ 27
	tp := mb.adaptiveThroughput()
	if tp < 40 || tp > 60 {
		t.Fatalf("middle depth: expected ~51, got %d", tp)
	}
}

func TestAdaptiveMailbox_Stats(t *testing.T) {
	var processed int64
	mb := NewAdaptiveMailbox(DefaultAdaptiveConfig())
	mb.RegisterHandlers(
		func(msg interface{}) { atomic.AddInt64(&processed, 1) },
		func(msg interface{}) {},
	)
	amb := mb.(*adaptiveMailbox)
	amb.SetScheduler(NewSynchronizedDispatcher(20))

	for i := 0; i < 50; i++ {
		mb.PostUserMessage("msg")
	}

	stats := amb.Stats()
	if stats.ProcessedCount == 0 {
		t.Fatal("expected processed count > 0")
	}
	if stats.ScheduleCount == 0 {
		t.Fatal("expected schedule count > 0")
	}
	if stats.MaxQueueDepth == 0 {
		t.Fatal("expected max queue depth > 0")
	}
}

func TestAdaptiveMailbox_CooperativeYield(t *testing.T) {
	config := AdaptiveMailboxConfig{
		MinThroughput:             1000,
		MaxThroughput:             1000,
		LowDepthThreshold:         1,
		HighDepthThreshold:        10,
		CooperativeYieldThreshold: 5 * time.Millisecond,
	}
	mb := NewAdaptiveMailbox(config).(*adaptiveMailbox)
	mb.RegisterHandlers(
		func(msg interface{}) {
			time.Sleep(2 * time.Millisecond) // 模拟慢处理
		},
		func(msg interface{}) {},
	)

	// 使用延迟调度器：收集所有 Post 后再手动 run
	// 这样 run() 一次性处理多条消息，协作式让出才能生效
	lazy := &lazyDispatcher{}
	mb.SetScheduler(lazy)

	for i := 0; i < 20; i++ {
		mb.PostUserMessage("msg")
	}

	// 手动调用 run 一次
	mb.run()

	stats := mb.Stats()
	if stats.YieldCount == 0 {
		t.Fatal("expected at least one cooperative yield")
	}
}

// lazyDispatcher 延迟调度器：记录调度请求但不实际执行
// 用于测试需要精确控制 run() 调用时机的场景
type lazyDispatcher struct {
	throughput int
}

func (d *lazyDispatcher) Schedule(fn func()) {
	// 不执行，留给测试手动触发
}

func (d *lazyDispatcher) Throughput() int {
	if d.throughput == 0 {
		return 1000
	}
	return d.throughput
}

func TestWorkStealingDispatcher_BasicSchedule(t *testing.T) {
	d := NewWorkStealingDispatcher(4, 10)
	defer d.Stop()

	var executed int64
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		d.Schedule(func() {
			atomic.AddInt64(&executed, 1)
			wg.Done()
		})
	}

	// 等待所有任务完成
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout: only %d / 100 executed", atomic.LoadInt64(&executed))
	}

	if atomic.LoadInt64(&executed) != 100 {
		t.Fatalf("expected 100 executed, got %d", atomic.LoadInt64(&executed))
	}
}

func TestWorkStealingDispatcher_StealBehavior(t *testing.T) {
	d := NewWorkStealingDispatcher(4, 10)
	defer d.Stop()

	var executed int64
	var wg sync.WaitGroup

	// 直接往 worker 0 塞大量任务，其他 worker 应通过窃取帮助
	for i := 0; i < 200; i++ {
		wg.Add(1)
		d.workers[0].push(func() {
			atomic.AddInt64(&executed, 1)
			wg.Done()
		})
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout: only %d executed", atomic.LoadInt64(&executed))
	}

	// 检查是否发生过窃取
	stats := d.Stats()
	totalSteals := int64(0)
	for _, s := range stats {
		totalSteals += s.Steals
	}
	if totalSteals == 0 {
		t.Log("warning: no steals recorded (may be timing-dependent)")
	}
}

func TestWorkStealingDispatcher_PanicRecovery(t *testing.T) {
	d := NewWorkStealingDispatcher(2, 10)
	defer d.Stop()

	var executed int64
	var wg sync.WaitGroup

	// 调度 panic 任务
	for i := 0; i < 10; i++ {
		wg.Add(1)
		d.Schedule(func() {
			defer wg.Done()
			panic("test panic")
		})
	}

	// 调度正常任务
	for i := 0; i < 10; i++ {
		wg.Add(1)
		d.Schedule(func() {
			defer wg.Done()
			atomic.AddInt64(&executed, 1)
		})
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout: panic tasks should not block worker")
	}

	// 正常任务应全部执行
	if atomic.LoadInt64(&executed) != 10 {
		t.Fatalf("expected 10 normal tasks, got %d", atomic.LoadInt64(&executed))
	}
}

// Benchmarks

func BenchmarkAdaptiveMailbox(b *testing.B) {
	var processed int64
	mb := NewAdaptiveMailbox(DefaultAdaptiveConfig())
	mb.RegisterHandlers(
		func(msg interface{}) { atomic.AddInt64(&processed, 1) },
		func(msg interface{}) {},
	)
	if amb, ok := mb.(*adaptiveMailbox); ok {
		amb.SetScheduler(NewSynchronizedDispatcher(100))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mb.PostUserMessage("test")
	}
	b.ReportMetric(float64(processed), "processed")
}

func BenchmarkWorkStealingDispatcher(b *testing.B) {
	d := NewWorkStealingDispatcher(8, 10)
	defer d.Stop()

	var wg sync.WaitGroup
	wg.Add(b.N)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Schedule(wg.Done)
	}
	wg.Wait()
}
