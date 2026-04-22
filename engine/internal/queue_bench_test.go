package internal

import (
	"sync"
	"testing"
)

func BenchmarkQueuePush(b *testing.B) {
	q := NewQueue()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Push(i)
	}
}

func BenchmarkQueuePop(b *testing.B) {
	q := NewQueue()
	for i := 0; i < b.N; i++ {
		q.Push(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Pop()
	}
}

func BenchmarkQueuePushPop(b *testing.B) {
	q := NewQueue()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Push(i)
		q.Pop()
	}
}

func BenchmarkQueueConcurrentPush(b *testing.B) {
	q := NewQueue()
	numProducers := 4

	b.ResetTimer()
	var wg sync.WaitGroup
	perProducer := b.N / numProducers
	for p := 0; p < numProducers; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perProducer; i++ {
				q.Push(i)
			}
		}()
	}
	wg.Wait()
}
