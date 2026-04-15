package actor

import (
	"sync"
	"sync/atomic"
	"testing"
)

// BenchmarkZeroAllocSendPath 测试 Send 路径的分配数（核心 Zero-Alloc 基线）
// 目标：识别消息传递链路上的所有分配点
func BenchmarkZeroAllocSendPath(b *testing.B) {
	system := NewActorSystem()
	var counter int64
	var wg sync.WaitGroup
	wg.Add(b.N)

	props := PropsFromFunc(func(ctx Context) {
		if _, ok := ctx.Message().(int); ok {
			atomic.AddInt64(&counter, 1)
			wg.Done()
		}
	})
	pid := system.Root.SpawnNamed(props, "zeroalloc-target")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		system.Root.Send(pid, i)
	}
	wg.Wait()
	b.StopTimer()
}

// BenchmarkRingBufferMailboxThroughput 测试 Ring Buffer 邮箱吞吐
func BenchmarkRingBufferMailboxThroughput(b *testing.B) {
	processed := int64(0)
	mb := NewRingBufferMailbox(4096)
	mb.RegisterHandlers(
		func(msg interface{}) { atomic.AddInt64(&processed, 1) },
		func(msg interface{}) {},
	)
	if rmb, ok := mb.(*ringBufferMailbox); ok {
		rmb.SetScheduler(NewSynchronizedDispatcher(100))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mb.PostUserMessage("test")
	}
	b.ReportMetric(float64(processed), "processed")
}

// BenchmarkRingBufferVsMPSCQueue 对比 Ring Buffer 与 MPSC 链表队列
func BenchmarkRingBufferVsMPSCQueue(b *testing.B) {
	b.Run("RingBuffer-Push", func(b *testing.B) {
		rb := newMPSCRingBuffer(b.N)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			rb.Push(i)
		}
	})

	b.Run("RingBuffer-PushPop", func(b *testing.B) {
		rb := newMPSCRingBuffer(1024)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			rb.Push(i)
			rb.Pop()
		}
	})

	b.Run("RingBuffer-Concurrent", func(b *testing.B) {
		rb := newMPSCRingBuffer(1 << 16)
		b.ReportAllocs()
		b.SetParallelism(4)
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				rb.Push(1)
			}
		})
	})
}

// BenchmarkPIDCache 测试 PID 缓存性能
func BenchmarkPIDCache(b *testing.B) {
	system := NewActorSystem()
	props := PropsFromFunc(func(ctx Context) {})
	pid := system.Root.SpawnNamed(props, "cache-target")

	b.Run("ColdLookup-First", func(b *testing.B) {
		// 模拟已缓存的 pid.p 命中路径
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = system.ProcessRegistry.Get(pid)
		}
	})

	b.Run("ByID-Cache", func(b *testing.B) {
		// 通过 id 新建 PID（无 pid.p 缓存），测试 PIDCache 回填效果
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			p := NewLocalPID("cache-target")
			_, _ = system.ProcessRegistry.Get(p)
		}
	})

	system.Root.Stop(pid)
}

// BenchmarkRingBufferMailboxPostAllocs 专测 PostUserMessage 分配数
func BenchmarkRingBufferMailboxPostAllocs(b *testing.B) {
	mb := NewRingBufferMailbox(1 << 16)
	mb.RegisterHandlers(
		func(msg interface{}) {},
		func(msg interface{}) {},
	)
	// 使用非调度 Dispatcher 保持 status = idle 测试纯 Push 路径
	disp := NewSynchronizedDispatcher(1 << 20)
	if rmb, ok := mb.(*ringBufferMailbox); ok {
		rmb.SetScheduler(disp)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mb.PostUserMessage(1)
	}
}

// BenchmarkSendMessage_RingBuffer 对比使用 Ring Buffer 邮箱的完整 Send 路径
func BenchmarkSendMessage_RingBuffer(b *testing.B) {
	system := NewActorSystem()
	var wg sync.WaitGroup
	wg.Add(b.N)

	props := PropsFromFunc(func(ctx Context) {
		if _, ok := ctx.Message().(int); ok {
			wg.Done()
		}
	}).WithRingBufferMailbox(1 << 16)

	pid := system.Root.SpawnNamed(props, "rb-actor")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		system.Root.Send(pid, i)
	}
	wg.Wait()
}

// BenchmarkPIDCacheConcurrent 并发场景下的 PID 缓存查找
func BenchmarkPIDCacheConcurrent(b *testing.B) {
	system := NewActorSystem()
	props := PropsFromFunc(func(ctx Context) {})
	pid := system.Root.SpawnNamed(props, "concurrent-target")

	b.ReportAllocs()
	b.SetParallelism(8)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = system.ProcessRegistry.Get(pid)
		}
	})
}
