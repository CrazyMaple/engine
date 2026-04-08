package actor

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestBackpressureMailboxDropOldest(t *testing.T) {
	config := BackpressureConfig{
		HighWatermark: 3,
		Strategy:      StrategyDropOldest,
	}
	mb := NewBackpressureMailbox(5, config).(*backpressureMailbox)

	mb.RegisterHandlers(func(msg interface{}) {}, func(msg interface{}) {})
	// 不设置 scheduler，消息只入队不处理，用于测试背压触发

	// 投递 5 条消息，高水位 3，应丢弃最老的
	mb.PostUserMessage("msg1")
	mb.PostUserMessage("msg2")
	mb.PostUserMessage("msg3")
	mb.PostUserMessage("msg4") // 触发背压，丢弃 msg1
	mb.PostUserMessage("msg5") // 触发背压，丢弃 msg2

	if mb.DroppedCount() != 2 {
		t.Fatalf("expected 2 dropped, got %d", mb.DroppedCount())
	}
	if mb.QueueSize() != 3 {
		t.Fatalf("expected queue size 3, got %d", mb.QueueSize())
	}
}

func TestBackpressureMailboxDropNewest(t *testing.T) {
	config := BackpressureConfig{
		HighWatermark: 3,
		Strategy:      StrategyDropNewest,
	}
	mb := NewBackpressureMailbox(5, config).(*backpressureMailbox)

	mb.RegisterHandlers(func(msg interface{}) {}, func(msg interface{}) {})
	// 不设置 scheduler

	// 投递超过高水位的消息
	for i := 0; i < 5; i++ {
		mb.PostUserMessage(i)
	}

	if mb.DroppedCount() != 2 {
		t.Fatalf("expected 2 dropped (newest), got %d", mb.DroppedCount())
	}
	if mb.QueueSize() != 3 {
		t.Fatalf("expected queue size 3, got %d", mb.QueueSize())
	}
}

func TestBackpressureMailboxBlock(t *testing.T) {
	config := BackpressureConfig{
		HighWatermark: 2,
		LowWatermark:  1,
		Strategy:      StrategyBlock,
	}
	mb := NewBackpressureMailbox(5, config).(*backpressureMailbox)

	mb.RegisterHandlers(func(msg interface{}) {
		atomic.AddInt32(new(int32), 1)
	}, func(msg interface{}) {})
	// 不设置 scheduler，手动控制 run

	// 先填满到高水位
	mb.PostUserMessage("msg1")
	mb.PostUserMessage("msg2")

	// 第 3 条消息应被阻塞（在另一个 goroutine 发送）
	blocked := make(chan bool, 1)
	go func() {
		mb.PostUserMessage("msg3") // 应阻塞
		blocked <- true
	}()

	// 等待一小段时间确认阻塞
	time.Sleep(50 * time.Millisecond)
	select {
	case <-blocked:
		t.Fatal("expected msg3 to be blocked")
	default:
		// OK，确认被阻塞
	}

	// 手动触发 run 来处理消息降低水位 → 解除阻塞
	mb.run()

	// 等待阻塞解除
	select {
	case <-blocked:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("msg3 should have been unblocked after processing")
	}
}

func TestBackpressureMailboxNotify(t *testing.T) {
	config := BackpressureConfig{
		HighWatermark: 2,
		Strategy:      StrategyNotify,
	}
	mb := NewBackpressureMailbox(5, config).(*backpressureMailbox)

	es := NewEventStream()
	mb.SetEventStream(es)
	mb.SetOwnerPID(NewLocalPID("test-actor"))

	var overflowCount int32
	es.Subscribe(func(event interface{}) {
		if _, ok := event.(*MailboxOverflowEvent); ok {
			atomic.AddInt32(&overflowCount, 1)
		}
	})

	mb.RegisterHandlers(func(msg interface{}) {}, func(msg interface{}) {})
	// 不设置 scheduler

	// 投递超过高水位的消息
	mb.PostUserMessage("msg1")
	mb.PostUserMessage("msg2")
	mb.PostUserMessage("msg3") // 触发 Notify

	// 等待异步事件发布
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt32(&overflowCount) == 0 {
		t.Fatal("expected overflow event to be published")
	}
	// Notify 不丢弃消息
	if mb.DroppedCount() != 0 {
		t.Fatalf("Notify should not drop messages, got %d", mb.DroppedCount())
	}
	if mb.QueueSize() != 3 {
		t.Fatalf("Notify should accept all messages, got queue size %d", mb.QueueSize())
	}
}

func TestBackpressureMailboxSystemMsgNotAffected(t *testing.T) {
	config := BackpressureConfig{
		HighWatermark: 1,
		Strategy:      StrategyDropNewest,
	}
	mb := NewBackpressureMailbox(5, config).(*backpressureMailbox)

	mb.RegisterHandlers(func(msg interface{}) {}, func(msg interface{}) {})
	// 不设置 scheduler

	// 系统消息不受背压限制
	mb.PostSystemMessage("sys1")
	mb.PostSystemMessage("sys2")
	mb.PostSystemMessage("sys3")

	// 不应有任何丢弃
	if mb.DroppedCount() != 0 {
		t.Fatalf("system messages should not be affected by backpressure, dropped=%d", mb.DroppedCount())
	}
}

func TestBackpressureMailboxDefaultConfig(t *testing.T) {
	config := BackpressureConfig{} // 全部默认
	mb := NewBackpressureMailbox(0, config).(*backpressureMailbox)

	if mb.capacity != 1024 {
		t.Fatalf("expected default capacity 1024, got %d", mb.capacity)
	}
	if mb.config.HighWatermark != 1024 {
		t.Fatalf("expected default HighWatermark 1024, got %d", mb.config.HighWatermark)
	}
	if mb.config.LowWatermark != 512 {
		t.Fatalf("expected default LowWatermark 512, got %d", mb.config.LowWatermark)
	}
}

func TestBackpressureMailboxWithProps(t *testing.T) {
	config := BackpressureConfig{
		HighWatermark: 100,
		Strategy:      StrategyNotify,
	}

	props := PropsFromFunc(ActorFunc(func(ctx Context) {})).
		WithBackpressureMailbox(200, config)

	mb := props.mailbox()
	bpm, ok := mb.(*backpressureMailbox)
	if !ok {
		t.Fatalf("expected *backpressureMailbox, got %T", mb)
	}
	if bpm.capacity != 200 {
		t.Fatalf("expected capacity 200, got %d", bpm.capacity)
	}
	if bpm.config.HighWatermark != 100 {
		t.Fatalf("expected HighWatermark 100, got %d", bpm.config.HighWatermark)
	}
}
