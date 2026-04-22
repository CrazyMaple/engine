package actor

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestActorSystem_Shutdown(t *testing.T) {
	// 使用 defaultSystem 因为 actorCell 内部硬编码使用 defaultSystem
	system := defaultSystem

	var started int32
	var stopped int32

	pids := make([]*PID, 5)
	for i := 0; i < 5; i++ {
		props := PropsFromFunc(ActorFunc(func(ctx Context) {
			switch ctx.Message().(type) {
			case *Started:
				atomic.AddInt32(&started, 1)
			case *Stopping:
				atomic.AddInt32(&stopped, 1)
			}
		}))
		pids[i] = system.Root.SpawnNamed(props, "shutdown-test-"+string(rune('a'+i)))
	}

	time.Sleep(50 * time.Millisecond)

	if atomic.LoadInt32(&started) != 5 {
		t.Fatalf("expected 5 started actors, got %d", started)
	}

	// 记录停机前进程数
	beforeCount := system.ProcessRegistry.Count()
	if beforeCount < 6 { // 5 actors + deadletter
		t.Fatalf("expected at least 6 processes before shutdown, got %d", beforeCount)
	}

	err := system.ShutdownWithConfig(ShutdownConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("shutdown error: %v", err)
	}

	// 验证只剩 deadletter
	afterCount := system.ProcessRegistry.Count()
	if afterCount > 1 {
		t.Errorf("expected <=1 processes after shutdown, got %d", afterCount)
	}
}

func TestActorSystem_ShutdownTimeout(t *testing.T) {
	system := NewActorSystem()

	// 注册一个假的进程来模拟超时场景
	fakePID := NewLocalPID("fake-blocking")
	fakeProc := &blockingProcess{block: make(chan struct{})}
	system.ProcessRegistry.Add(fakePID, fakeProc)

	err := system.ShutdownWithConfig(ShutdownConfig{Timeout: 200 * time.Millisecond})
	close(fakeProc.block)

	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestActorSystem_ShutdownEmpty(t *testing.T) {
	system := NewActorSystem()
	err := system.Shutdown()
	if err != nil {
		t.Errorf("shutdown of empty system should succeed: %v", err)
	}
}

func TestShutdownConfig_DefaultTimeout(t *testing.T) {
	system := NewActorSystem()
	err := system.ShutdownWithConfig(ShutdownConfig{Timeout: 0})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// blockingProcess 模拟一个停不下来的进程
type blockingProcess struct {
	block chan struct{}
}

func (p *blockingProcess) SendUserMessage(pid *PID, message interface{})  {}
func (p *blockingProcess) SendSystemMessage(pid *PID, message interface{}) {
	<-p.block // 阻塞直到关闭
}
func (p *blockingProcess) Stop(pid *PID) {}
