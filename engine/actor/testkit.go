package actor

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestKit Actor 测试工具包
// 提供 TestActorSystem、TestProbe 等工具，简化 Actor 单元测试
type TestKit struct {
	t      testing.TB
	system *ActorSystem
	probes []*TestProbe
}

// NewTestKit 创建测试工具包
// 自动创建隔离的 ActorSystem，测试结束后自动清理
func NewTestKit(t testing.TB) *TestKit {
	system := NewActorSystem()
	tk := &TestKit{
		t:      t,
		system: system,
	}
	t.Cleanup(func() {
		tk.Shutdown()
	})
	return tk
}

// System 获取测试用 ActorSystem
func (tk *TestKit) System() *ActorSystem {
	return tk.system
}

// Spawn 创建 Actor
func (tk *TestKit) Spawn(props *Props) *PID {
	return tk.system.Root.Spawn(props)
}

// SpawnNamed 创建命名 Actor
func (tk *TestKit) SpawnNamed(props *Props, name string) *PID {
	return tk.system.Root.SpawnNamed(props, name)
}

// Send 发送消息
func (tk *TestKit) Send(pid *PID, message interface{}) {
	tk.system.Root.Send(pid, message)
}

// Stop 停止 Actor
func (tk *TestKit) Stop(pid *PID) {
	tk.system.Root.Stop(pid)
}

// NewProbe 创建测试探针 Actor，可记录并断言接收到的消息
func (tk *TestKit) NewProbe() *TestProbe {
	probe := newTestProbe(tk)
	tk.probes = append(tk.probes, probe)
	return probe
}

// Shutdown 关闭测试系统
func (tk *TestKit) Shutdown() {
	for _, p := range tk.probes {
		if p.pid != nil {
			tk.system.Root.Stop(p.pid)
		}
	}
}

// TestProbe 测试探针 Actor
// 记录收到的所有消息，提供 ExpectMsg / ExpectNoMsg 等断言方法
type TestProbe struct {
	tk       *TestKit
	pid      *PID
	messages []interface{}
	msgCh    chan interface{}
	mu       sync.Mutex
	filters  []func(interface{}) bool
}

func newTestProbe(tk *TestKit) *TestProbe {
	p := &TestProbe{
		tk:    tk,
		msgCh: make(chan interface{}, 256),
	}

	props := PropsFromFunc(ActorFunc(func(ctx Context) {
		msg := ctx.Message()
		switch msg.(type) {
		case *Started, *Stopping, *Stopped:
			return // 忽略生命周期消息
		}

		// 解包信封
		actual, _ := UnwrapEnvelope(msg)

		// 检查过滤器
		p.mu.Lock()
		shouldIgnore := false
		for _, f := range p.filters {
			if f(actual) {
				shouldIgnore = true
				break
			}
		}
		p.mu.Unlock()
		if shouldIgnore {
			return
		}

		p.mu.Lock()
		p.messages = append(p.messages, actual)
		p.mu.Unlock()

		select {
		case p.msgCh <- actual:
		default:
			// channel 满了，消息仍然记录在 messages 切片中
		}
	}))

	p.pid = tk.system.Root.Spawn(props)
	return p
}

// PID 返回探针的 PID
func (p *TestProbe) PID() *PID {
	return p.pid
}

// ExpectMsg 等待并返回下一条消息
// 超时则使测试失败
func (p *TestProbe) ExpectMsg(timeout time.Duration) interface{} {
	p.tk.t.Helper()
	select {
	case msg := <-p.msgCh:
		return msg
	case <-time.After(timeout):
		p.tk.t.Fatalf("TestProbe: timeout waiting for message (waited %v)", timeout)
		return nil
	}
}

// ExpectMsgType 等待指定类型的消息
func (p *TestProbe) ExpectMsgType(timeout time.Duration, expected interface{}) interface{} {
	p.tk.t.Helper()
	msg := p.ExpectMsg(timeout)
	expectedType := fmt.Sprintf("%T", expected)
	actualType := fmt.Sprintf("%T", msg)
	if expectedType != actualType {
		p.tk.t.Fatalf("TestProbe: expected message type %s, got %s (%v)", expectedType, actualType, msg)
	}
	return msg
}

// ExpectNoMsg 断言在指定时间内没有收到消息
func (p *TestProbe) ExpectNoMsg(duration time.Duration) {
	p.tk.t.Helper()
	select {
	case msg := <-p.msgCh:
		p.tk.t.Fatalf("TestProbe: expected no message but received %T: %v", msg, msg)
	case <-time.After(duration):
		// pass
	}
}

// IgnoreMsg 注册消息过滤器，匹配的消息将被忽略不记录
func (p *TestProbe) IgnoreMsg(filter func(interface{}) bool) {
	p.mu.Lock()
	p.filters = append(p.filters, filter)
	p.mu.Unlock()
}

// Messages 返回所有已接收到的消息（快照副本）
func (p *TestProbe) Messages() []interface{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]interface{}, len(p.messages))
	copy(result, p.messages)
	return result
}

// MessageCount 返回已接收的消息数量
func (p *TestProbe) MessageCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.messages)
}

// Clear 清空已接收的消息
func (p *TestProbe) Clear() {
	p.mu.Lock()
	p.messages = nil
	p.mu.Unlock()
	// drain channel
	for {
		select {
		case <-p.msgCh:
		default:
			return
		}
	}
}

// RequestAndExpect 向目标发送请求并等待探针收到响应
func (p *TestProbe) RequestAndExpect(target *PID, message interface{}, timeout time.Duration) interface{} {
	p.tk.t.Helper()
	p.tk.system.Root.Send(target, WrapEnvelope(message, p.pid))
	return p.ExpectMsg(timeout)
}

// --- 测试辅助 Actor ---

// BlackholeActor 黑洞 Actor，接收所有消息但不做任何事
type BlackholeActor struct{}

func (a *BlackholeActor) Receive(ctx Context) {}

// NewBlackholeProps 创建黑洞 Actor 的 Props
func NewBlackholeProps() *Props {
	return PropsFromProducer(func() Actor { return &BlackholeActor{} })
}

// EchoActor 回声 Actor，将收到的消息原样返回给发送方
type EchoActor struct{}

func (a *EchoActor) Receive(ctx Context) {
	switch ctx.Message().(type) {
	case *Started, *Stopping, *Stopped:
		return
	}
	if ctx.Sender() != nil {
		ctx.Respond(ctx.Message())
	}
}

// NewEchoProps 创建回声 Actor 的 Props
func NewEchoProps() *Props {
	return PropsFromProducer(func() Actor { return &EchoActor{} })
}

// ForwardActor 转发 Actor，将消息转发到指定目标
type ForwardActor struct {
	Target *PID
}

func (a *ForwardActor) Receive(ctx Context) {
	switch ctx.Message().(type) {
	case *Started, *Stopping, *Stopped:
		return
	}
	if a.Target != nil {
		ctx.Send(a.Target, ctx.Message())
	}
}

// NewForwardProps 创建转发 Actor 的 Props
func NewForwardProps(target *PID) *Props {
	return PropsFromProducer(func() Actor { return &ForwardActor{Target: target} })
}
