package stress

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"time"
)

// --- 可编程虚拟玩家（Bot）框架 ---
// 提供 Bot 生命周期管理、链式行为配置、批量管理

// BotLifecycle 虚拟玩家生命周期接口
// 使用者实现此接口定义 Bot 的登录、游戏、登出行为
type BotLifecycle interface {
	// Login 登录阶段（连接服务器、认证、进入大厅）
	Login(ctx context.Context, conn BotConnection) error
	// Play 游戏阶段（循环执行直到上下文取消）
	Play(ctx context.Context, conn BotConnection) error
	// Logout 登出阶段（清理资源、断开连接）
	Logout(ctx context.Context, conn BotConnection) error
}

// BotConnection 对 Bot 暴露的连接抽象
type BotConnection interface {
	// Send 发送消息，返回延迟（微秒）
	Send(msgType string, payload interface{}) (latencyUs int64, err error)
	// Recv 接收消息（阻塞直到收到或超时）
	Recv(timeout time.Duration) (msgType string, payload json.RawMessage, err error)
	// Close 关闭连接
	Close() error
	// RemoteAddr 服务器地址
	RemoteAddr() string
}

// BotState 虚拟玩家状态
type BotState string

const (
	BotStateIdle     BotState = "idle"
	BotStateLogin    BotState = "login"
	BotStatePlaying  BotState = "playing"
	BotStateLogout   BotState = "logout"
	BotStateStopped  BotState = "stopped"
	BotStateError    BotState = "error"
)

// ManagedBot 带生命周期管理的虚拟玩家
type ManagedBot struct {
	ID        int
	Name      string
	State     BotState
	Lifecycle BotLifecycle
	Metrics   *Metrics
	conn      BotConnection
	addr      string
	connFn    func(addr string) (BotConnection, error)
	mu        sync.RWMutex
	err       error
}

// Run 执行 Bot 完整生命周期: Login → Play → Logout
func (b *ManagedBot) Run(ctx context.Context) error {
	var err error

	// 建立连接
	conn, connErr := b.connFn(b.addr)
	if connErr != nil {
		b.setState(BotStateError)
		b.err = connErr
		return connErr
	}
	b.conn = conn
	defer conn.Close()

	// Login
	b.setState(BotStateLogin)
	if err = b.Lifecycle.Login(ctx, conn); err != nil {
		b.setState(BotStateError)
		b.err = err
		if b.Metrics != nil {
			b.Metrics.RecordRequest(0, true)
		}
		return fmt.Errorf("bot %d login: %w", b.ID, err)
	}

	// Play
	b.setState(BotStatePlaying)
	playErr := b.Lifecycle.Play(ctx, conn)
	// Play 正常因 ctx 取消而退出不算错误
	if playErr != nil && ctx.Err() == nil {
		b.err = playErr
	}

	// Logout
	b.setState(BotStateLogout)
	logoutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = b.Lifecycle.Logout(logoutCtx, conn); err != nil {
		b.setState(BotStateError)
		b.err = err
		return fmt.Errorf("bot %d logout: %w", b.ID, err)
	}

	b.setState(BotStateStopped)
	return nil
}

func (b *ManagedBot) setState(s BotState) {
	b.mu.Lock()
	b.State = s
	b.mu.Unlock()
}

// GetState 获取当前状态
func (b *ManagedBot) GetState() BotState {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.State
}

// GetError 获取错误
func (b *ManagedBot) GetError() error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.err
}

// --- BotBuilder 链式配置 ---

// BotBuilder 虚拟玩家构建器
type BotBuilder struct {
	addr      string
	lifecycle BotLifecycle
	connFn    func(addr string) (BotConnection, error)
	metrics   *Metrics
}

// NewBotBuilder 创建 Bot 构建器
func NewBotBuilder() *BotBuilder {
	return &BotBuilder{
		connFn: defaultTCPConnector,
	}
}

// Target 设置目标服务器地址
func (bb *BotBuilder) Target(addr string) *BotBuilder {
	bb.addr = addr
	return bb
}

// WithLifecycle 设置 Bot 生命周期实现
func (bb *BotBuilder) WithLifecycle(lc BotLifecycle) *BotBuilder {
	bb.lifecycle = lc
	return bb
}

// WithConnector 设置自定义连接器
func (bb *BotBuilder) WithConnector(fn func(addr string) (BotConnection, error)) *BotBuilder {
	bb.connFn = fn
	return bb
}

// WithMetrics 设置指标收集器
func (bb *BotBuilder) WithMetrics(m *Metrics) *BotBuilder {
	bb.metrics = m
	return bb
}

// Build 构建单个 ManagedBot
func (bb *BotBuilder) Build(id int) *ManagedBot {
	return &ManagedBot{
		ID:        id,
		Name:      fmt.Sprintf("bot-%d", id),
		State:     BotStateIdle,
		Lifecycle: bb.lifecycle,
		Metrics:   bb.metrics,
		addr:      bb.addr,
		connFn:    bb.connFn,
	}
}

// --- 内置 Bot 行为模板 ---

// IdleBot 空闲 Bot：登录后仅保持连接，不发送任何消息
type IdleBot struct{}

func (b *IdleBot) Login(ctx context.Context, conn BotConnection) error  { return nil }
func (b *IdleBot) Logout(ctx context.Context, conn BotConnection) error { return nil }
func (b *IdleBot) Play(ctx context.Context, conn BotConnection) error {
	<-ctx.Done()
	return nil
}

// ActiveBot 活跃 Bot：按固定间隔发送消息
type ActiveBot struct {
	MsgType  string                 // 消息类型
	Payload  map[string]interface{} // 消息内容
	Interval time.Duration          // 发送间隔
	Metrics  *Metrics
}

func (b *ActiveBot) Login(ctx context.Context, conn BotConnection) error  { return nil }
func (b *ActiveBot) Logout(ctx context.Context, conn BotConnection) error { return nil }
func (b *ActiveBot) Play(ctx context.Context, conn BotConnection) error {
	ticker := time.NewTicker(b.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			lat, err := conn.Send(b.MsgType, b.Payload)
			if b.Metrics != nil {
				b.Metrics.RecordRequest(lat, err != nil)
			}
		}
	}
}

// StressBot 压力 Bot：尽可能快地发送消息（最高压力模式）
type StressBot struct {
	MsgType string
	Payload map[string]interface{}
	Metrics *Metrics
}

func (b *StressBot) Login(ctx context.Context, conn BotConnection) error  { return nil }
func (b *StressBot) Logout(ctx context.Context, conn BotConnection) error { return nil }
func (b *StressBot) Play(ctx context.Context, conn BotConnection) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			lat, err := conn.Send(b.MsgType, b.Payload)
			if b.Metrics != nil {
				b.Metrics.RecordRequest(lat, err != nil)
			}
		}
	}
}

// SequenceBot 序列 Bot：按预定义行为序列循环执行
type SequenceBot struct {
	Actions []BotSequenceAction
	Metrics *Metrics
}

// BotSequenceAction 序列行为
type BotSequenceAction struct {
	Type    string                 // "send", "wait", "random_wait"
	MsgType string                 // send 时的消息类型
	Payload map[string]interface{} // send 时的消息内容
	Delay   time.Duration          // wait 时的延迟
	MinWait time.Duration          // random_wait 最小延迟
	MaxWait time.Duration          // random_wait 最大延迟
}

func (b *SequenceBot) Login(ctx context.Context, conn BotConnection) error  { return nil }
func (b *SequenceBot) Logout(ctx context.Context, conn BotConnection) error { return nil }
func (b *SequenceBot) Play(ctx context.Context, conn BotConnection) error {
	for {
		for _, action := range b.Actions {
			select {
			case <-ctx.Done():
				return nil
			default:
			}

			switch action.Type {
			case "send":
				lat, err := conn.Send(action.MsgType, action.Payload)
				if b.Metrics != nil {
					b.Metrics.RecordRequest(lat, err != nil)
				}
			case "wait":
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(action.Delay):
				}
			case "random_wait":
				d := action.MinWait
				if action.MaxWait > action.MinWait {
					d += time.Duration(rand.Int63n(int64(action.MaxWait - action.MinWait)))
				}
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(d):
				}
			}
		}
	}
}

// --- 默认 TCP 连接器 ---

func defaultTCPConnector(addr string) (BotConnection, error) {
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, err
	}
	return &tcpBotConnection{conn: conn}, nil
}

type tcpBotConnection struct {
	conn net.Conn
	mu   sync.Mutex
}

func (c *tcpBotConnection) Send(msgType string, payload interface{}) (int64, error) {
	start := time.Now()
	data, err := json.Marshal(map[string]interface{}{
		"type":    msgType,
		"payload": payload,
	})
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	_, err = c.conn.Write(data)
	c.mu.Unlock()
	return time.Since(start).Microseconds(), err
}

func (c *tcpBotConnection) Recv(timeout time.Duration) (string, json.RawMessage, error) {
	c.conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 4096)
	n, err := c.conn.Read(buf)
	if err != nil {
		return "", nil, err
	}
	var msg struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(buf[:n], &msg); err != nil {
		return "", nil, err
	}
	return msg.Type, msg.Payload, nil
}

func (c *tcpBotConnection) Close() error {
	return c.conn.Close()
}

func (c *tcpBotConnection) RemoteAddr() string {
	return c.conn.RemoteAddr().String()
}
