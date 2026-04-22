package actor

import (
	"sync"
	"testing"

	"engine/internal"
)

// BenchmarkActorSpawn 测试 Actor 创建性能
func BenchmarkActorSpawn(b *testing.B) {
	system := NewActorSystem()
	props := PropsFromFunc(func(ctx Context) {})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pid := system.Root.Spawn(props)
		system.Root.Stop(pid)
	}
}

// BenchmarkSendMessage 测试消息发送性能
func BenchmarkSendMessage(b *testing.B) {
	system := NewActorSystem()
	var wg sync.WaitGroup
	wg.Add(b.N)

	props := PropsFromFunc(func(ctx Context) {
		switch ctx.Message().(type) {
		case string:
			wg.Done()
		}
	})
	pid := system.Root.SpawnNamed(props, "bench-actor")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		system.Root.Send(pid, "hello")
	}
	wg.Wait()
}

// BenchmarkMailboxThroughput 测试邮箱吞吐量
func BenchmarkMailboxThroughput(b *testing.B) {
	processed := 0
	mb := NewDefaultMailbox()
	mb.RegisterHandlers(
		func(msg interface{}) { processed++ },
		func(msg interface{}) {},
	)
	if dmb, ok := mb.(*defaultMailbox); ok {
		dmb.SetScheduler(NewSynchronizedDispatcher(100))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mb.PostUserMessage("test")
	}
	b.ReportMetric(float64(processed), "processed")
}

// BenchmarkBoundedMailboxThroughput 测试有界邮箱吞吐量
func BenchmarkBoundedMailboxThroughput(b *testing.B) {
	processed := 0
	mb := NewBoundedMailbox(4096)
	mb.RegisterHandlers(
		func(msg interface{}) { processed++ },
		func(msg interface{}) {},
	)
	if bmb, ok := mb.(*boundedMailbox); ok {
		bmb.SetScheduler(NewSynchronizedDispatcher(100))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mb.PostUserMessage("test")
	}
	b.ReportMetric(float64(processed), "processed")
}

// BenchmarkEnvelopePool 测试消息信封池化性能
func BenchmarkEnvelopePool(b *testing.B) {
	sender := NewLocalPID("sender")

	b.Run("pooled", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			env := AcquireEnvelope("msg", sender)
			ReleaseEnvelope(env)
		}
	})

	b.Run("allocate", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = &MessageEnvelope{Message: "msg", Sender: sender}
		}
	})
}

// BenchmarkMPSCQueue 测试 MPSC 无锁队列性能
func BenchmarkMPSCQueue(b *testing.B) {
	q := internal.NewQueue()

	b.Run("push", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			q.Push(i)
		}
	})

	// 重新填充队列用于 pop 测试
	q2 := internal.NewQueue()
	for i := 0; i < b.N; i++ {
		q2.Push(i)
	}
	b.Run("pop", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			q2.Pop()
		}
	})

	b.Run("push_pop", func(b *testing.B) {
		q3 := internal.NewQueue()
		for i := 0; i < b.N; i++ {
			q3.Push(i)
			q3.Pop()
		}
	})
}

// BenchmarkBufferPool 测试字节缓冲区池化性能
func BenchmarkBufferPool(b *testing.B) {
	b.Run("pooled", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			buf := AcquireBuffer()
			*buf = append(*buf, "test data for benchmark"...)
			ReleaseBuffer(buf)
		}
	})

	b.Run("allocate", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			buf := make([]byte, 0, 4096)
			buf = append(buf, "test data for benchmark"...)
			_ = buf
		}
	})
}
