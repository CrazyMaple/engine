package actor

import (
	"sync"
	"sync/atomic"
	"testing"
)

// BenchmarkZeroAllocSendPath 测试 Send 路径的分配数（核心 Zero-Alloc 基线）。
// 目标：识别消息传递链路上的所有分配点。
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

// BenchmarkPIDCache 测试 PID 缓存性能。
func BenchmarkPIDCache(b *testing.B) {
	system := NewActorSystem()
	props := PropsFromFunc(func(ctx Context) {})
	pid := system.Root.SpawnNamed(props, "cache-target")

	b.Run("ColdLookup-First", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = system.ProcessRegistry.Get(pid)
		}
	})

	b.Run("ByID-Cache", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			p := NewLocalPID("cache-target")
			_, _ = system.ProcessRegistry.Get(p)
		}
	})

	system.Root.Stop(pid)
}

// BenchmarkPIDCacheConcurrent 并发场景下的 PID 缓存查找。
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
