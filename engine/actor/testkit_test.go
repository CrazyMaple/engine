package actor

import (
	"testing"
	"time"
)

func TestTestKitBasic(t *testing.T) {
	tk := NewTestKit(t)

	probe := tk.NewProbe()
	if probe.PID() == nil {
		t.Fatal("probe should have PID")
	}

	// 发送消息给探针
	tk.Send(probe.PID(), "hello")
	msg := probe.ExpectMsg(time.Second)
	if msg != "hello" {
		t.Fatalf("expected 'hello', got %v", msg)
	}
}

func TestTestProbeExpectNoMsg(t *testing.T) {
	tk := NewTestKit(t)
	probe := tk.NewProbe()

	// 不发送消息，验证无消息断言
	probe.ExpectNoMsg(50 * time.Millisecond)
}

func TestTestProbeMessageCount(t *testing.T) {
	tk := NewTestKit(t)
	probe := tk.NewProbe()

	for i := 0; i < 5; i++ {
		tk.Send(probe.PID(), i)
	}
	time.Sleep(100 * time.Millisecond)

	if probe.MessageCount() != 5 {
		t.Fatalf("expected 5 messages, got %d", probe.MessageCount())
	}

	msgs := probe.Messages()
	if len(msgs) != 5 {
		t.Fatalf("expected 5 messages in snapshot, got %d", len(msgs))
	}
}

func TestTestProbeIgnoreMsg(t *testing.T) {
	tk := NewTestKit(t)
	probe := tk.NewProbe()

	// 忽略字符串消息
	probe.IgnoreMsg(func(msg interface{}) bool {
		_, ok := msg.(string)
		return ok
	})

	tk.Send(probe.PID(), "ignored")
	tk.Send(probe.PID(), 42)
	time.Sleep(100 * time.Millisecond)

	msg := probe.ExpectMsg(time.Second)
	if msg != 42 {
		t.Fatalf("expected 42, got %v", msg)
	}

	if probe.MessageCount() != 1 {
		t.Fatalf("expected 1 message (string ignored), got %d", probe.MessageCount())
	}
}

func TestTestProbeClear(t *testing.T) {
	tk := NewTestKit(t)
	probe := tk.NewProbe()

	tk.Send(probe.PID(), "a")
	tk.Send(probe.PID(), "b")
	time.Sleep(100 * time.Millisecond)

	probe.Clear()

	if probe.MessageCount() != 0 {
		t.Fatalf("after clear, expected 0 messages, got %d", probe.MessageCount())
	}
}

func TestEchoActor(t *testing.T) {
	tk := NewTestKit(t)
	probe := tk.NewProbe()
	echo := tk.Spawn(NewEchoProps())

	// 通过探针发送并期望回声
	msg := probe.RequestAndExpect(echo, "ping", time.Second)
	if msg != "ping" {
		t.Fatalf("expected 'ping' echo, got %v", msg)
	}
}

func TestForwardActor(t *testing.T) {
	tk := NewTestKit(t)
	probe := tk.NewProbe()
	fwd := tk.Spawn(NewForwardProps(probe.PID()))

	tk.Send(fwd, "forwarded")
	msg := probe.ExpectMsg(time.Second)
	if msg != "forwarded" {
		t.Fatalf("expected 'forwarded', got %v", msg)
	}
}

func TestBlackholeActor(t *testing.T) {
	tk := NewTestKit(t)

	// 黑洞 Actor 不应崩溃
	bh := tk.Spawn(NewBlackholeProps())
	tk.Send(bh, "anything")
	tk.Send(bh, 123)
	tk.Send(bh, struct{}{})
	time.Sleep(50 * time.Millisecond)
	// 没有崩溃就算通过
}

func TestTestKitSpawnNamed(t *testing.T) {
	tk := NewTestKit(t)

	pid := tk.SpawnNamed(NewBlackholeProps(), "test-actor")
	if pid == nil {
		t.Fatal("should spawn named actor")
	}
	if pid.Id == "" {
		t.Fatal("named actor should have ID")
	}
}

type counterActor struct {
	count int
}

func (a *counterActor) Receive(ctx Context) {
	switch ctx.Message().(type) {
	case *Started:
		return
	case string:
		a.count++
		if ctx.Sender() != nil {
			ctx.Respond(a.count)
		}
	}
}

func TestTestKitMultiProbe(t *testing.T) {
	tk := NewTestKit(t)
	p1 := tk.NewProbe()
	p2 := tk.NewProbe()

	counter := tk.Spawn(PropsFromProducer(func() Actor { return &counterActor{} }))

	// p1 发请求
	p1.RequestAndExpect(counter, "inc", time.Second)
	// p2 发请求
	p2.RequestAndExpect(counter, "inc", time.Second)

	if p1.MessageCount() != 1 {
		t.Fatalf("p1 should have 1 message, got %d", p1.MessageCount())
	}
	if p2.MessageCount() != 1 {
		t.Fatalf("p2 should have 1 message, got %d", p2.MessageCount())
	}
}
