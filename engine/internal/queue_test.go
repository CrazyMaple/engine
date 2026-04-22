package internal

import (
	"sync"
	"testing"
)

func TestQueuePushPop(t *testing.T) {
	q := NewQueue()

	q.Push("a")
	q.Push("b")
	q.Push("c")

	if v := q.Pop(); v != "a" {
		t.Fatalf("expected a, got %v", v)
	}
	if v := q.Pop(); v != "b" {
		t.Fatalf("expected b, got %v", v)
	}
	if v := q.Pop(); v != "c" {
		t.Fatalf("expected c, got %v", v)
	}
}

func TestQueuePopEmpty(t *testing.T) {
	q := NewQueue()

	if v := q.Pop(); v != nil {
		t.Fatalf("expected nil, got %v", v)
	}
}

func TestQueueEmpty(t *testing.T) {
	q := NewQueue()

	if !q.Empty() {
		t.Fatal("new queue should be empty")
	}

	q.Push("x")
	if q.Empty() {
		t.Fatal("queue should not be empty after push")
	}

	q.Pop()
	if !q.Empty() {
		t.Fatal("queue should be empty after pop")
	}
}

func TestQueueConcurrentPush(t *testing.T) {
	q := NewQueue()
	const numProducers = 10
	const numPerProducer = 1000

	var wg sync.WaitGroup
	for p := 0; p < numProducers; p++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < numPerProducer; i++ {
				q.Push(id*numPerProducer + i)
			}
		}(p)
	}
	wg.Wait()

	// 单消费者验证所有消息都能取出
	seen := make(map[int]bool)
	for {
		v := q.Pop()
		if v == nil {
			break
		}
		n := v.(int)
		if seen[n] {
			t.Fatalf("duplicate value: %d", n)
		}
		seen[n] = true
	}

	if len(seen) != numProducers*numPerProducer {
		t.Fatalf("expected %d values, got %d", numProducers*numPerProducer, len(seen))
	}
}

func TestQueueOrder(t *testing.T) {
	q := NewQueue()
	for i := 0; i < 100; i++ {
		q.Push(i)
	}

	// 单生产者时应保持 FIFO 顺序
	for i := 0; i < 100; i++ {
		v := q.Pop()
		if v != i {
			t.Fatalf("expected %d, got %v", i, v)
		}
	}
}
