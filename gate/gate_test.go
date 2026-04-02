package gate

import (
	"errors"
	"net"
	"testing"
	"time"

	"engine/actor"
)

// mockConn 模拟网络连接
type mockConn struct {
	readData  [][]byte
	readIdx   int
	written   [][]byte
	closed    bool
	destroyed bool
}

func (c *mockConn) ReadMsg() ([]byte, error) {
	if c.readIdx >= len(c.readData) {
		return nil, errors.New("EOF")
	}
	data := c.readData[c.readIdx]
	c.readIdx++
	return data, nil
}

func (c *mockConn) WriteMsg(args ...[]byte) error {
	for _, b := range args {
		c.written = append(c.written, b)
	}
	return nil
}

func (c *mockConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
}

func (c *mockConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 12345}
}

func (c *mockConn) Close() {
	c.closed = true
}

func (c *mockConn) Destroy() {
	c.destroyed = true
}

// mockProcessor 模拟消息处理器
type mockProcessor struct {
	unmarshalFn func([]byte) (interface{}, error)
	marshalFn   func(interface{}) ([][]byte, error)
	routeFn     func(interface{}, interface{}) error
}

func (p *mockProcessor) Unmarshal(data []byte) (interface{}, error) {
	if p.unmarshalFn != nil {
		return p.unmarshalFn(data)
	}
	return string(data), nil
}

func (p *mockProcessor) Marshal(msg interface{}) ([][]byte, error) {
	if p.marshalFn != nil {
		return p.marshalFn(msg)
	}
	return [][]byte{[]byte(msg.(string))}, nil
}

func (p *mockProcessor) Route(msg interface{}, agent interface{}) error {
	if p.routeFn != nil {
		return p.routeFn(msg, agent)
	}
	return nil
}

func TestNewGateDefaults(t *testing.T) {
	system := actor.NewActorSystem()
	g := NewGate(system)

	if g.MaxConnNum != 1000 {
		t.Fatalf("expected MaxConnNum=1000, got %d", g.MaxConnNum)
	}
	if g.PendingWriteNum != 100 {
		t.Fatalf("expected PendingWriteNum=100, got %d", g.PendingWriteNum)
	}
	if g.MaxMsgLen != 4096 {
		t.Fatalf("expected MaxMsgLen=4096, got %d", g.MaxMsgLen)
	}
	if g.LenMsgLen != 2 {
		t.Fatalf("expected LenMsgLen=2, got %d", g.LenMsgLen)
	}
}

func TestAgentUserData(t *testing.T) {
	system := actor.NewActorSystem()
	g := NewGate(system)

	agent := &Agent{
		conn:      &mockConn{},
		gate:      g,
		system:    system,
		closeChan: make(chan struct{}),
	}

	if agent.UserData() != nil {
		t.Fatal("initial UserData should be nil")
	}

	agent.SetUserData("player1")
	if agent.UserData() != "player1" {
		t.Fatalf("expected UserData=player1, got %v", agent.UserData())
	}
}

func TestAgentBindActor(t *testing.T) {
	system := actor.NewActorSystem()
	g := NewGate(system)

	agent := &Agent{
		conn:      &mockConn{},
		gate:      g,
		system:    system,
		closeChan: make(chan struct{}),
	}

	if agent.GetActor() != nil {
		t.Fatal("initial actor should be nil")
	}

	pid := actor.NewLocalPID("test-actor")
	agent.BindActor(pid)

	if agent.GetActor() != pid {
		t.Fatal("bound actor PID should match")
	}
}

func TestAgentAddr(t *testing.T) {
	conn := &mockConn{}
	agent := &Agent{
		conn:      conn,
		closeChan: make(chan struct{}),
	}

	local := agent.LocalAddr()
	if local.String() != "127.0.0.1:8080" {
		t.Fatalf("unexpected local addr: %s", local.String())
	}

	remote := agent.RemoteAddr()
	if remote.String() != "192.168.1.1:12345" {
		t.Fatalf("unexpected remote addr: %s", remote.String())
	}
}

func TestAgentWriteMsg(t *testing.T) {
	conn := &mockConn{}
	proc := &mockProcessor{
		marshalFn: func(msg interface{}) ([][]byte, error) {
			return [][]byte{[]byte("encoded")}, nil
		},
	}

	system := actor.NewActorSystem()
	g := NewGate(system)
	g.Processor = proc

	agent := &Agent{
		conn:      conn,
		gate:      g,
		system:    system,
		closeChan: make(chan struct{}),
	}

	err := agent.WriteMsg("hello")
	if err != nil {
		t.Fatalf("WriteMsg error: %v", err)
	}

	if len(conn.written) != 1 || string(conn.written[0]) != "encoded" {
		t.Fatalf("expected written=[encoded], got %v", conn.written)
	}
}

func TestAgentWriteMsgNoProcessor(t *testing.T) {
	conn := &mockConn{}
	system := actor.NewActorSystem()
	g := NewGate(system)
	// 不设置 Processor

	agent := &Agent{
		conn:      conn,
		gate:      g,
		system:    system,
		closeChan: make(chan struct{}),
	}

	err := agent.WriteMsg("hello")
	if err != nil {
		t.Fatalf("WriteMsg without processor should return nil, got: %v", err)
	}
	if len(conn.written) != 0 {
		t.Fatal("nothing should be written without processor")
	}
}

func TestAgentRunProcessesMessages(t *testing.T) {
	conn := &mockConn{
		readData: [][]byte{[]byte("msg1"), []byte("msg2")},
	}

	var routed []string
	proc := &mockProcessor{
		routeFn: func(msg interface{}, agent interface{}) error {
			routed = append(routed, msg.(string))
			return nil
		},
	}

	system := actor.NewActorSystem()
	g := NewGate(system)
	g.Processor = proc

	agent := &Agent{
		conn:      conn,
		gate:      g,
		system:    system,
		closeChan: make(chan struct{}),
	}

	// Run 将在 ReadMsg 返回 EOF 后退出
	done := make(chan struct{})
	go func() {
		agent.Run()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run should have returned after EOF")
	}

	if len(routed) != 2 {
		t.Fatalf("expected 2 routed messages, got %d", len(routed))
	}
	if routed[0] != "msg1" || routed[1] != "msg2" {
		t.Fatalf("unexpected routed messages: %v", routed)
	}
}

func TestAgentOnClose(t *testing.T) {
	conn := &mockConn{}
	system := actor.NewActorSystem()
	g := NewGate(system)

	agent := &Agent{
		conn:      conn,
		gate:      g,
		system:    system,
		closeChan: make(chan struct{}),
	}

	// OnClose 不应 panic（无绑定 Actor 时）
	agent.OnClose()

	select {
	case <-agent.closeChan:
		// ok, channel closed
	default:
		t.Fatal("closeChan should be closed after OnClose")
	}
}

func TestAgentCloseAndDestroy(t *testing.T) {
	conn := &mockConn{}
	agent := &Agent{
		conn:      conn,
		closeChan: make(chan struct{}),
	}

	agent.Close()
	if !conn.closed {
		t.Fatal("conn should be closed")
	}

	conn2 := &mockConn{}
	agent2 := &Agent{
		conn:      conn2,
		closeChan: make(chan struct{}),
	}
	agent2.Destroy()
	if !conn2.destroyed {
		t.Fatal("conn should be destroyed")
	}
}
