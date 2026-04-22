package actor

import (
	"sync"
	"testing"
)

func TestEnvelopePool(t *testing.T) {
	sender := NewLocalPID("sender")
	msg := "hello"

	env := AcquireEnvelope(msg, sender)
	if env.Message != msg {
		t.Fatalf("expected message %v, got %v", msg, env.Message)
	}
	if env.Sender != sender {
		t.Fatalf("expected sender %v, got %v", sender, env.Sender)
	}

	ReleaseEnvelope(env)
	if env.Message != nil || env.Sender != nil {
		t.Fatal("envelope should be cleared after release")
	}
}

func TestEnvelopePoolConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			env := AcquireEnvelope(n, nil)
			if env.Message != n {
				t.Errorf("expected %d, got %v", n, env.Message)
			}
			ReleaseEnvelope(env)
		}(i)
	}
	wg.Wait()
}

func TestBufferPool(t *testing.T) {
	buf := AcquireBuffer()
	if buf == nil {
		t.Fatal("buffer should not be nil")
	}
	if len(*buf) != 0 {
		t.Fatalf("buffer should be empty, got len=%d", len(*buf))
	}
	if cap(*buf) < 4096 {
		t.Fatalf("buffer capacity should be >= 4096, got %d", cap(*buf))
	}

	*buf = append(*buf, []byte("test data")...)
	ReleaseBuffer(buf)
}

func TestPIDPool(t *testing.T) {
	pid := AcquirePID("localhost:8080", "actor1")
	if pid.Address != "localhost:8080" || pid.Id != "actor1" {
		t.Fatalf("unexpected PID: %v", pid)
	}
	ReleasePID(pid)
	if pid.Address != "" || pid.Id != "" {
		t.Fatal("PID should be cleared after release")
	}
}

func TestReleaseNil(t *testing.T) {
	// 不应 panic
	ReleaseEnvelope(nil)
	ReleaseBuffer(nil)
	ReleasePID(nil)
}
