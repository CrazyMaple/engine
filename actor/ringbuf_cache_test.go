package actor

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRingBuffer_BasicPushPop(t *testing.T) {
	rb := newMPSCRingBuffer(64)

	if !rb.Empty() {
		t.Fatal("new ring buffer should be empty")
	}

	// Push 10 条消息
	for i := 0; i < 10; i++ {
		if !rb.Push(i) {
			t.Fatalf("push %d failed", i)
		}
	}

	if rb.Len() != 10 {
		t.Fatalf("expected len 10, got %d", rb.Len())
	}

	// Pop 依次取出并校验顺序（FIFO）
	for i := 0; i < 10; i++ {
		v := rb.Pop()
		if v != i {
			t.Fatalf("expected %d, got %v", i, v)
		}
	}

	if !rb.Empty() {
		t.Fatal("ring buffer should be empty after popping all")
	}
}

func TestRingBuffer_FullBuffer(t *testing.T) {
	rb := newMPSCRingBuffer(64)
	cap := rb.Cap()

	// 填满
	for i := 0; i < cap; i++ {
		if !rb.Push(i) {
			t.Fatalf("push %d failed", i)
		}
	}

	// 再 Push 应失败
	if rb.Push(999) {
		t.Fatal("push on full buffer should fail")
	}

	// Pop 一个后再 Push 应成功
	_ = rb.Pop()
	if !rb.Push(100) {
		t.Fatal("push after pop should succeed")
	}
}

func TestRingBuffer_PopBatch(t *testing.T) {
	rb := newMPSCRingBuffer(64)
	for i := 0; i < 20; i++ {
		rb.Push(i)
	}

	dst := make([]interface{}, 0, 10)
	dst = rb.PopBatch(dst, 10)

	if len(dst) != 10 {
		t.Fatalf("expected 10 items, got %d", len(dst))
	}
	for i, v := range dst {
		if v != i {
			t.Fatalf("expected %d, got %v at index %d", i, v, i)
		}
	}

	if rb.Len() != 10 {
		t.Fatalf("expected 10 remaining, got %d", rb.Len())
	}
}

func TestRingBuffer_ConcurrentPush(t *testing.T) {
	rb := newMPSCRingBuffer(1 << 16)
	numProducers := 8
	perProducer := 1000
	var wg sync.WaitGroup

	for p := 0; p < numProducers; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perProducer; i++ {
				for !rb.Push(i) {
					// 自旋等待（测试不考虑吞吐，只要求正确性）
					time.Sleep(time.Microsecond)
				}
			}
		}()
	}
	wg.Wait()

	total := rb.Len()
	if total != numProducers*perProducer {
		t.Fatalf("expected %d items, got %d", numProducers*perProducer, total)
	}

	// Pop 出全部消息
	popped := 0
	for rb.Pop() != nil || !rb.Empty() {
		popped++
		if popped > total {
			break
		}
	}
}

func TestRingBufferMailbox_Basic(t *testing.T) {
	var processed int64
	mb := NewRingBufferMailbox(64)
	mb.RegisterHandlers(
		func(msg interface{}) { atomic.AddInt64(&processed, 1) },
		func(msg interface{}) {},
	)
	if rmb, ok := mb.(*ringBufferMailbox); ok {
		rmb.SetScheduler(NewSynchronizedDispatcher(100))
	}

	for i := 0; i < 100; i++ {
		mb.PostUserMessage("msg")
	}

	if atomic.LoadInt64(&processed) != 100 {
		t.Fatalf("expected 100 processed, got %d", atomic.LoadInt64(&processed))
	}
}

func TestRingBufferMailbox_OverflowQueue(t *testing.T) {
	// 使用很小的 ring buffer 强制走溢出队列
	var processed int64
	mb := NewRingBufferMailbox(64)
	mb.RegisterHandlers(
		func(msg interface{}) { atomic.AddInt64(&processed, 1) },
		func(msg interface{}) {},
	)
	if rmb, ok := mb.(*ringBufferMailbox); ok {
		rmb.SetScheduler(NewSynchronizedDispatcher(10))
	}

	// 发送超过 ring buffer 容量的消息
	total := 1000
	for i := 0; i < total; i++ {
		mb.PostUserMessage("msg")
	}

	// 给异步处理一点时间
	time.Sleep(100 * time.Millisecond)

	// 同步 Dispatcher 下应该立即处理完（有 overflow 迁移逻辑）
	if atomic.LoadInt64(&processed) != int64(total) {
		t.Fatalf("expected %d processed, got %d", total, atomic.LoadInt64(&processed))
	}
}

func TestPIDCache_BasicGetSet(t *testing.T) {
	cache := &PIDCache{}
	pid := NewLocalPID("test-pid")
	proc := &deadLetterProcess{}

	// 初始未命中
	if _, ok := cache.Get(pid); ok {
		t.Fatal("empty cache should miss")
	}

	// Set 后命中
	cache.Set(pid, proc)
	if p, ok := cache.Get(pid); !ok || p != proc {
		t.Fatal("cache hit expected")
	}

	// Invalidate 后未命中
	cache.Invalidate(pid)
	// 注意：pid.p 仍会被清空，但新建一个 PID 实例走 sync.Map 路径
	newPid := NewLocalPID("test-pid")
	if _, ok := cache.Get(newPid); ok {
		t.Fatal("after invalidate, cache should miss")
	}
}

func TestPIDCache_HitRate(t *testing.T) {
	cache := &PIDCache{}
	pid := NewLocalPID("hit-test")
	proc := &deadLetterProcess{}
	cache.Set(pid, proc)

	// 100 次命中
	for i := 0; i < 100; i++ {
		cache.Get(pid)
	}
	// 10 次未命中
	for i := 0; i < 10; i++ {
		cache.Get(NewLocalPID("missing"))
	}

	rate := cache.HitRate()
	if rate < 0.9 || rate > 1.0 {
		t.Fatalf("expected hit rate ~0.91, got %f", rate)
	}
}

func TestProcessRegistry_PIDCacheIntegration(t *testing.T) {
	// 清理全局缓存避免测试间干扰
	globalPIDCache.InvalidateAll()

	system := NewActorSystem()
	props := PropsFromFunc(func(ctx Context) {})
	pid := system.Root.SpawnNamed(props, "cache-integration-test")

	// 通过新建 PID（无 pid.p）触发缓存路径
	newPid := NewLocalPID("cache-integration-test")
	proc, ok := system.ProcessRegistry.Get(newPid)
	if !ok {
		t.Fatal("expected to find process")
	}
	if proc == nil {
		t.Fatal("process should not be nil")
	}

	// 检查缓存命中率提升
	hits, _ := globalPIDCache.Stats()
	if hits == 0 {
		t.Fatal("expected at least one cache hit after lookups")
	}

	system.Root.Stop(pid)
}
