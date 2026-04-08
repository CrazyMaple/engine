package actor

import (
	"sync"
	"testing"
	"time"
)

func TestBatchMailboxBatchFull(t *testing.T) {
	var received [][]interface{}
	var mu sync.Mutex
	done := make(chan struct{})

	mb := NewBatchMailbox(BatchMailboxConfig{
		BatchSize:    3,
		BatchTimeout: time.Second, // 不应触发
	})
	mb.RegisterHandlers(nil, nil)
	mb.(*batchMailbox).RegisterBatchHandler(func(msgs []interface{}) {
		mu.Lock()
		received = append(received, msgs)
		mu.Unlock()
		if len(msgs) == 3 {
			close(done)
		}
	})
	mb.(*batchMailbox).SetScheduler(defaultDispatcher)
	mb.Start()

	// 发送 3 条消息，应触发批处理
	mb.PostUserMessage("a")
	mb.PostUserMessage("b")
	mb.PostUserMessage("c")

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for batch")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) == 0 {
		t.Fatal("no batch received")
	}
	if len(received[0]) != 3 {
		t.Errorf("expected batch of 3, got %d", len(received[0]))
	}
}

func TestBatchMailboxTimeout(t *testing.T) {
	var received [][]interface{}
	var mu sync.Mutex
	done := make(chan struct{})

	mb := NewBatchMailbox(BatchMailboxConfig{
		BatchSize:    100, // 不会因数量触发
		BatchTimeout: 20 * time.Millisecond,
	})
	mb.RegisterHandlers(nil, nil)
	mb.(*batchMailbox).RegisterBatchHandler(func(msgs []interface{}) {
		mu.Lock()
		received = append(received, msgs)
		mu.Unlock()
		close(done)
	})
	mb.(*batchMailbox).SetScheduler(defaultDispatcher)
	mb.Start()

	// 发送 1 条，应在超时后触发
	mb.PostUserMessage("x")

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for batch timeout flush")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 || len(received[0]) != 1 {
		t.Errorf("expected 1 batch with 1 msg, got %v", received)
	}
}

func TestBatchMailboxSystemBypassesBatch(t *testing.T) {
	systemReceived := make(chan interface{}, 10)

	mb := NewBatchMailbox(BatchMailboxConfig{
		BatchSize:    100,
		BatchTimeout: time.Second,
	})
	mb.RegisterHandlers(nil, func(msg interface{}) {
		systemReceived <- msg
	})
	mb.(*batchMailbox).SetScheduler(defaultDispatcher)
	mb.Start()

	// 系统消息应立即投递
	mb.PostSystemMessage("sys1")

	select {
	case msg := <-systemReceived:
		if msg != "sys1" {
			t.Errorf("unexpected system message: %v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for system message")
	}
}

func TestBatchMailboxFallbackToSingle(t *testing.T) {
	// 当没有 batchInvoker 时，应降级为逐条投递
	var received []interface{}
	var mu sync.Mutex
	done := make(chan struct{})

	mb := NewBatchMailbox(BatchMailboxConfig{
		BatchSize:    2,
		BatchTimeout: time.Second,
	})
	mb.RegisterHandlers(func(msg interface{}) {
		mu.Lock()
		received = append(received, msg)
		if len(received) == 2 {
			close(done)
		}
		mu.Unlock()
	}, nil)
	// 不注册 batchInvoker
	mb.(*batchMailbox).SetScheduler(defaultDispatcher)
	mb.Start()

	mb.PostUserMessage("a")
	mb.PostUserMessage("b")

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Errorf("expected 2, got %d", len(received))
	}
}
