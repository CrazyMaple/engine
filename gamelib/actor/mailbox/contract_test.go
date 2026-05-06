package mailbox_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"engine/actor"
	"gamelib/actor/mailbox"
)

// 这一组契约测试覆盖外迁邮箱通过 Props.WithMailbox 注入后，
// engine spawn 路径会通过 §2.5.1 的四个扩展接口（SchedulerAware / OwnerAware /
// EventStreamAware / BatchAwareMailbox）正确连接调度、Owner、事件流、批处理回调。

// --- adaptive 邮箱：调度链路 ---

func TestContract_AdaptiveMailbox_SchedulesAndProcesses(t *testing.T) {
	system := actor.NewActorSystem()
	var counter int64
	var wg sync.WaitGroup
	const n = 200
	wg.Add(n)

	props := actor.PropsFromFunc(func(ctx actor.Context) {
		if _, ok := ctx.Message().(int); ok {
			atomic.AddInt64(&counter, 1)
			wg.Done()
		}
	}).WithMailbox(func() actor.Mailbox {
		return mailbox.NewAdaptive(mailbox.DefaultAdaptiveConfig())
	})

	pid := system.Root.SpawnNamed(props, "adaptive-actor")
	defer system.Root.Stop(pid)

	for i := 0; i < n; i++ {
		system.Root.Send(pid, i)
	}

	if !waitForWG(&wg, 2*time.Second) {
		t.Fatalf("adaptive mailbox did not flush in time, processed=%d", atomic.LoadInt64(&counter))
	}
	if got := atomic.LoadInt64(&counter); got != n {
		t.Fatalf("expected %d processed, got %d", n, got)
	}
}

// --- batch 邮箱：BatchReceive 仍能拿到 ctx 与消息切片 ---

type batchCollector struct {
	t        *testing.T
	mu       sync.Mutex
	batches  [][]interface{}
	gotCtx   bool
	doneOnce sync.Once
	done     chan struct{}
}

func newBatchCollector(t *testing.T) *batchCollector {
	return &batchCollector{t: t, done: make(chan struct{})}
}

func (b *batchCollector) Receive(ctx actor.Context) {
	// 单条路径；批处理时不会走这里（batchInvoker 优先）。
}

func (b *batchCollector) BatchReceive(ctx actor.Context, msgs []interface{}) {
	b.mu.Lock()
	if ctx != nil {
		b.gotCtx = true
	}
	cp := make([]interface{}, len(msgs))
	copy(cp, msgs)
	b.batches = append(b.batches, cp)
	b.mu.Unlock()
	b.doneOnce.Do(func() { close(b.done) })
}

func TestContract_BatchMailbox_PreservesContextAndMessages(t *testing.T) {
	system := actor.NewActorSystem()
	collector := newBatchCollector(t)

	props := actor.PropsFromProducer(func() actor.Actor { return collector }).
		WithMailbox(func() actor.Mailbox {
			return mailbox.NewBatch(mailbox.BatchConfig{BatchSize: 3, BatchTimeout: time.Second})
		})

	pid := system.Root.SpawnNamed(props, "batch-actor")
	defer system.Root.Stop(pid)

	system.Root.Send(pid, "a")
	system.Root.Send(pid, "b")
	system.Root.Send(pid, "c")

	select {
	case <-collector.done:
	case <-time.After(2 * time.Second):
		t.Fatal("batch mailbox did not invoke BatchReceive within timeout")
	}

	collector.mu.Lock()
	defer collector.mu.Unlock()
	if !collector.gotCtx {
		t.Fatal("BatchReceive must receive non-nil Context (ctx must not be lost)")
	}
	if len(collector.batches) == 0 {
		t.Fatal("expected at least one batch")
	}
	if len(collector.batches[0]) != 3 {
		t.Fatalf("expected first batch size 3, got %d", len(collector.batches[0]))
	}
}

// --- priority 邮箱：背压事件经 EventStream 发出，OwnerPID 命中 ---

type prioritizer struct{}

func (prioritizer) Priority(msg interface{}) mailbox.MessagePriority {
	if s, ok := msg.(string); ok && s == "high" {
		return mailbox.PriorityHigh
	}
	if s, ok := msg.(string); ok && s == "low" {
		return mailbox.PriorityLow
	}
	return mailbox.PriorityNormal
}

func TestContract_PriorityMailbox_BackpressureEvent(t *testing.T) {
	system := actor.NewActorSystem()
	// system.EventStream 会被 spawn 路径自动注入到实现 EventStreamAware 的 mailbox。
	es := system.EventStream

	overflowCh := make(chan actor.MailboxOverflowEvent, 8)
	sub := es.Subscribe(func(ev interface{}) {
		if e, ok := ev.(actor.MailboxOverflowEvent); ok {
			select {
			case overflowCh <- e:
			default:
			}
		}
	})
	defer es.Unsubscribe(sub)

	props := actor.PropsFromFunc(func(ctx actor.Context) {
		// 故意阻塞处理，让 low 队列堆积
		time.Sleep(10 * time.Millisecond)
	}).WithMailbox(func() actor.Mailbox {
		return mailbox.NewPriority(mailbox.PriorityConfig{
			Prioritizer:        prioritizer{},
			BackpressureConfig: &actor.BackpressureConfig{HighWatermark: 2, Strategy: actor.StrategyDropNewest},
		})
	})

	pid := system.Root.SpawnNamed(props, "priority-actor")
	defer system.Root.Stop(pid)

	for i := 0; i < 10; i++ {
		system.Root.Send(pid, "low")
	}

	select {
	case ev := <-overflowCh:
		if ev.PID == nil || ev.PID.Id != "priority-actor" {
			t.Fatalf("OwnerPID not propagated correctly: %+v", ev.PID)
		}
		if ev.Watermark != 2 {
			t.Fatalf("Watermark mismatch: %d", ev.Watermark)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected MailboxOverflowEvent was not published — OwnerAware / EventStreamAware not wired")
	}
}

// --- ringbuf 邮箱：spawn 后能正常处理消息（验证 SchedulerAware 接入）---

func TestContract_RingBufferMailbox_Processes(t *testing.T) {
	system := actor.NewActorSystem()
	var counter int64
	var wg sync.WaitGroup
	const n = 500
	wg.Add(n)

	props := actor.PropsFromFunc(func(ctx actor.Context) {
		if _, ok := ctx.Message().(int); ok {
			atomic.AddInt64(&counter, 1)
			wg.Done()
		}
	}).WithMailbox(func() actor.Mailbox {
		return mailbox.NewRingBuffer(1024)
	})

	pid := system.Root.SpawnNamed(props, "ringbuf-actor")
	defer system.Root.Stop(pid)

	for i := 0; i < n; i++ {
		system.Root.Send(pid, i)
	}

	if !waitForWG(&wg, 2*time.Second) {
		t.Fatalf("ringbuf mailbox did not flush in time, processed=%d", atomic.LoadInt64(&counter))
	}
	if got := atomic.LoadInt64(&counter); got != n {
		t.Fatalf("expected %d processed, got %d", n, got)
	}
}

func waitForWG(wg *sync.WaitGroup, timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}
