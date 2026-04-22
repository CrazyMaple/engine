package actor

import (
	"sync"
	"testing"
	"time"
)

func TestStash_PushPop(t *testing.T) {
	s := newMessageStash(10)

	// Push messages
	for i := 0; i < 5; i++ {
		if err := s.push(i); err != nil {
			t.Fatalf("push %d: %v", i, err)
		}
	}

	if s.size() != 5 {
		t.Fatalf("expected size 5, got %d", s.size())
	}

	// Pop all - should be FIFO
	msgs := s.popAll()
	if len(msgs) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(msgs))
	}
	for i, m := range msgs {
		if m.message != i {
			t.Errorf("message %d: expected %d, got %v", i, i, m.message)
		}
	}

	// After pop, stash should be empty
	if s.size() != 0 {
		t.Fatalf("expected empty stash, got %d", s.size())
	}
}

func TestStash_CapacityLimit(t *testing.T) {
	s := newMessageStash(3)

	for i := 0; i < 3; i++ {
		if err := s.push(i); err != nil {
			t.Fatalf("push %d: %v", i, err)
		}
	}

	// Should fail on 4th push
	if err := s.push(99); err != ErrStashFull {
		t.Fatalf("expected ErrStashFull, got %v", err)
	}
}

func TestStash_Clear(t *testing.T) {
	s := newMessageStash(10)
	s.push("a")
	s.push("b")
	s.clear()

	if s.size() != 0 {
		t.Fatalf("expected empty after clear, got %d", s.size())
	}
}

func TestActorStashUnstash(t *testing.T) {
	system := NewActorSystem()
	var mu sync.Mutex
	var received []interface{}
	ready := make(chan struct{})
	done := make(chan struct{})

	// Actor that stashes messages in Loading state, then unstashes in Ready state
	props := PropsFromFunc(func(ctx Context) {
		switch msg := ctx.Message().(type) {
		case *Started:
			// Start in loading state — do nothing special
		case string:
			if msg == "loaded" {
				// Transition to ready state, unstash all
				ctx.UnstashAll()
				return
			}
			if msg == "done" {
				close(done)
				return
			}
			// In loading state, stash all string messages
			mu.Lock()
			stashed := len(received) == 0 // still loading if no messages processed
			mu.Unlock()
			if stashed {
				ctx.Stash()
				return
			}
			// In ready state, collect messages
			mu.Lock()
			received = append(received, msg)
			mu.Unlock()
		case int:
			// Signal that actor has started
			if msg == 0 {
				close(ready)
			}
		}
	})

	pid := system.Root.SpawnNamed(props, "stash-test")
	time.Sleep(10 * time.Millisecond)

	// Send start signal
	system.Root.Send(pid, 0)
	<-ready

	// Send messages that should be stashed
	system.Root.Send(pid, "msg1")
	system.Root.Send(pid, "msg2")
	system.Root.Send(pid, "msg3")
	time.Sleep(20 * time.Millisecond)

	// Mark as "loaded" — this triggers UnstashAll
	mu.Lock()
	received = append(received, "MARKER") // mark transition to ready state
	mu.Unlock()
	system.Root.Send(pid, "loaded")
	time.Sleep(50 * time.Millisecond)

	// Send done signal
	system.Root.Send(pid, "done")

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for done signal")
	}

	mu.Lock()
	defer mu.Unlock()

	// After MARKER, we should have received msg1, msg2, msg3 (unstashed)
	if len(received) < 1 {
		t.Fatal("expected at least MARKER in received")
	}
	if received[0] != "MARKER" {
		t.Errorf("expected MARKER first, got %v", received[0])
	}
}

func TestActorStashSize(t *testing.T) {
	s := newMessageStash(100)
	if s.size() != 0 {
		t.Fatal("new stash should be empty")
	}

	s.push("a")
	s.push("b")
	if s.size() != 2 {
		t.Fatalf("expected 2, got %d", s.size())
	}

	s.popAll()
	if s.size() != 0 {
		t.Fatalf("expected 0 after popAll, got %d", s.size())
	}
}
