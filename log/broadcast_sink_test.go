package log

import (
	"sync"
	"sync/atomic"
	"testing"
)

type countingSub struct {
	received []LogEntry
	mu       sync.Mutex
	count    atomic.Int64
}

func (c *countingSub) Notify(entry LogEntry) {
	c.mu.Lock()
	c.received = append(c.received, entry)
	c.mu.Unlock()
	c.count.Add(1)
}

func TestBroadcastSink_SubscribeAndWrite(t *testing.T) {
	bs := NewBroadcastSink()
	if bs.SubscriberCount() != 0 {
		t.Fatalf("initial subscribers: want 0, got %d", bs.SubscriberCount())
	}

	s1 := &countingSub{}
	s2 := &countingSub{}
	cancel1 := bs.Subscribe(s1)
	bs.Subscribe(s2)
	if bs.SubscriberCount() != 2 {
		t.Fatalf("after subscribe: want 2, got %d", bs.SubscriberCount())
	}

	_ = bs.Write(LogEntry{Msg: "hello", Level: LevelInfo})
	if s1.count.Load() != 1 || s2.count.Load() != 1 {
		t.Fatalf("fan-out failed: s1=%d, s2=%d", s1.count.Load(), s2.count.Load())
	}

	// 取消后只剩 s2
	cancel1()
	if bs.SubscriberCount() != 1 {
		t.Fatalf("after cancel: want 1, got %d", bs.SubscriberCount())
	}
	_ = bs.Write(LogEntry{Msg: "world"})
	if s1.count.Load() != 1 {
		t.Fatalf("cancelled subscriber still receives: %d", s1.count.Load())
	}
	if s2.count.Load() != 2 {
		t.Fatalf("s2 should receive 2, got %d", s2.count.Load())
	}
}

func TestBroadcastSink_Close(t *testing.T) {
	bs := NewBroadcastSink()
	s := &countingSub{}
	bs.Subscribe(s)
	if err := bs.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if bs.SubscriberCount() != 0 {
		t.Fatalf("after Close: want 0 subs, got %d", bs.SubscriberCount())
	}
	// Close 后写入不再投递
	_ = bs.Write(LogEntry{Msg: "ghost"})
	if s.count.Load() != 0 {
		t.Fatalf("post-Close write should not deliver: %d", s.count.Load())
	}
}

func TestBroadcastSink_ConcurrentSubscribeWrite(t *testing.T) {
	bs := NewBroadcastSink()
	var wg sync.WaitGroup
	// 并发订阅 + 写入，验证无竞态
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := &countingSub{}
			cancel := bs.Subscribe(s)
			for j := 0; j < 20; j++ {
				_ = bs.Write(LogEntry{Msg: "x"})
			}
			cancel()
		}()
	}
	wg.Wait()
}
