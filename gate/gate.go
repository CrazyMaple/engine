package gate

import (
	"encoding/json"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"engine/actor"
	"engine/log"
	"engine/network"
)

// Gate 网关模块
type Gate struct {
	MaxConnNum      int
	PendingWriteNum int
	MaxMsgLen       uint32
	Processor       interface {
		Unmarshal(data []byte) (interface{}, error)
		Marshal(msg interface{}) ([][]byte, error)
		Route(msg interface{}, agent interface{}) error
	}

	// TCP配置
	TCPAddr      string
	LenMsgLen    int
	LittleEndian bool

	// WebSocket配置
	WSAddr      string
	HTTPTimeout time.Duration
	CertFile    string
	KeyFile     string

	// 版本协商（nil 表示不启用握手）
	VersionNegotiator *VersionNegotiator

	// Security 安全过滤器链（nil 表示不启用安全检查）
	Security *SecurityChain

	tcpServer *network.TCPServer
	wsServer  *network.WSServer
	system    *actor.ActorSystem
	connCount int64 // 当前连接数（原子操作）
}

// ConnCount 返回当前客户端连接数
func (g *Gate) ConnCount() int64 {
	return atomic.LoadInt64(&g.connCount)
}

// Processor 消息处理器接口（已废弃，使用匿名接口）
type Processor interface {
	Unmarshal(data []byte) (interface{}, error)
	Marshal(msg interface{}) ([][]byte, error)
	Route(msg interface{}, agent interface{}) error
}

// NewGate 创建网关
func NewGate(system *actor.ActorSystem) *Gate {
	return &Gate{
		system:          system,
		MaxConnNum:      1000,
		PendingWriteNum: 100,
		MaxMsgLen:       4096,
		LenMsgLen:       2,
		LittleEndian:    false,
	}
}

// Start 启动网关
func (g *Gate) Start() {
	if g.TCPAddr != "" {
		g.tcpServer = &network.TCPServer{
			Addr:            g.TCPAddr,
			MaxConnNum:      g.MaxConnNum,
			PendingWriteNum: g.PendingWriteNum,
			LenMsgLen:       g.LenMsgLen,
			MaxMsgLen:       g.MaxMsgLen,
			LittleEndian:    g.LittleEndian,
			NewAgent:        g.newAgent,
		}
		g.tcpServer.Start()
	}

	if g.WSAddr != "" {
		g.wsServer = &network.WSServer{
			Addr:            g.WSAddr,
			MaxConnNum:      g.MaxConnNum,
			PendingWriteNum: g.PendingWriteNum,
			MaxMsgLen:       g.MaxMsgLen,
			HTTPTimeout:     g.HTTPTimeout,
			CertFile:        g.CertFile,
			KeyFile:         g.KeyFile,
			NewAgent:        g.newWSAgent,
		}
		g.wsServer.Start()
	}
}

// Close 关闭网关
func (g *Gate) Close() {
	if g.tcpServer != nil {
		g.tcpServer.Close()
	}
	if g.wsServer != nil {
		g.wsServer.Close()
	}
}

// GracefulClose 优雅关闭网关：通知客户端即将断开，等待进行中请求完成
func (g *Gate) GracefulClose(timeout time.Duration) {
	log.Info("Gate graceful shutdown started, timeout=%v", timeout)

	done := make(chan struct{})
	go func() {
		g.Close()
		close(done)
	}()

	select {
	case <-done:
		log.Info("Gate graceful shutdown completed")
	case <-time.After(timeout):
		log.Warn("Gate graceful shutdown timed out after %v", timeout)
	}
}

func (g *Gate) newAgent(conn *network.TCPConn) network.Agent {
	atomic.AddInt64(&g.connCount, 1)
	agent := &Agent{
		conn:      conn,
		gate:      g,
		system:    g.system,
		closeChan: make(chan struct{}),
	}
	agent.initSecurity()
	return agent
}

func (g *Gate) newWSAgent(conn *network.WSConn) network.Agent {
	atomic.AddInt64(&g.connCount, 1)
	agent := &Agent{
		conn:      conn,
		gate:      g,
		system:    g.system,
		closeChan: make(chan struct{}),
	}
	agent.initSecurity()
	return agent
}

// initSecurity 初始化安全上下文并执行连接检查
func (a *Agent) initSecurity() {
	if a.gate.Security == nil {
		return
	}
	remoteAddr := ""
	if addr := a.conn.RemoteAddr(); addr != nil {
		remoteAddr = addr.String()
	}
	a.secCtx = &SecurityContext{
		RemoteAddr:  remoteAddr,
		ConnID:      fmt.Sprintf("%p", a.conn),
		ConnectedAt: time.Now(),
	}
	if err := a.gate.Security.ProcessConnect(a.secCtx); err != nil {
		log.Warn("Security rejected connection from %s: %v", remoteAddr, err)
		a.conn.Close()
	}
}

// Agent 玩家代理
type Agent struct {
	conn            network.Conn
	gate            *Gate
	system          *actor.ActorSystem
	actorPID        *actor.PID
	userData        interface{}
	closeChan       chan struct{}
	protocolVersion int    // 协商后的协议版本（默认 1）
	clientSDK       string // 客户端 SDK 标识
	secCtx          *SecurityContext // 安全上下文
}

// Run 运行代理
func (a *Agent) Run() {
	a.protocolVersion = 1 // 默认版本
	firstMessage := true

	for {
		data, err := a.conn.ReadMsg()
		if err != nil {
			break
		}

		// 首条消息尝试检测握手请求
		if firstMessage && a.gate.VersionNegotiator != nil && isHandshakeRequest(data) {
			firstMessage = false
			a.handleHandshake(data)
			continue
		}
		firstMessage = false

		// 安全过滤器链检查
		if a.secCtx != nil && a.gate.Security != nil {
			result := a.gate.Security.ProcessMessage(a.secCtx, data)
			if result == FilterKick {
				log.Warn("Security kicked connection %s", a.secCtx.ConnID)
				break
			}
			if result == FilterReject {
				continue // 丢弃消息，继续处理下一条
			}
		}

		if a.gate.Processor != nil {
			msg, err := a.gate.Processor.Unmarshal(data)
			if err != nil {
				break
			}
			err = a.gate.Processor.Route(msg, a)
			if err != nil {
				break
			}
		}
	}
}

// handleHandshake 处理握手请求
func (a *Agent) handleHandshake(data []byte) {
	req, err := parseHandshakeRequest(data)
	if err != nil {
		log.Warn("handshake parse error: %v", err)
		return
	}

	resp := a.gate.VersionNegotiator.Negotiate(req)
	a.protocolVersion = resp.ProtocolVersion
	a.clientSDK = req.ClientSDK

	respData, err := json.Marshal(resp)
	if err != nil {
		log.Warn("handshake marshal error: %v", err)
		return
	}
	_ = a.conn.WriteMsg(respData)

	if resp.Status == "ok" {
		log.Info("handshake ok: client=%s version=%d", req.ClientSDK, resp.ProtocolVersion)
	} else {
		log.Warn("handshake failed: %s", resp.Message)
	}
}

// ProtocolVersion 返回协商后的协议版本
func (a *Agent) ProtocolVersion() int {
	return a.protocolVersion
}

// ClientSDK 返回客户端 SDK 标识
func (a *Agent) ClientSDK() string {
	return a.clientSDK
}

// OnClose 连接关闭回调
func (a *Agent) OnClose() {
	atomic.AddInt64(&a.gate.connCount, -1)
	// 通知安全过滤器连接断开
	if a.secCtx != nil && a.gate.Security != nil {
		a.gate.Security.ProcessDisconnect(a.secCtx)
	}
	close(a.closeChan)
	if a.actorPID != nil {
		a.system.Root.Stop(a.actorPID)
	}
}

// WriteMsg 写入消息
func (a *Agent) WriteMsg(msg interface{}) error {
	if a.gate.Processor != nil {
		data, err := a.gate.Processor.Marshal(msg)
		if err != nil {
			return err
		}
		return a.conn.WriteMsg(data...)
	}
	return nil
}

// LocalAddr 本地地址
func (a *Agent) LocalAddr() net.Addr {
	return a.conn.LocalAddr()
}

// RemoteAddr 远程地址
func (a *Agent) RemoteAddr() net.Addr {
	return a.conn.RemoteAddr()
}

// Close 关闭连接
func (a *Agent) Close() {
	a.conn.Close()
}

// Destroy 销毁连接
func (a *Agent) Destroy() {
	a.conn.Destroy()
}

// UserData 获取用户数据
func (a *Agent) UserData() interface{} {
	return a.userData
}

// SetUserData 设置用户数据
func (a *Agent) SetUserData(data interface{}) {
	a.userData = data
}

// BindActor 绑定Actor
func (a *Agent) BindActor(pid *actor.PID) {
	a.actorPID = pid
}

// GetActor 获取绑定的Actor
func (a *Agent) GetActor() *actor.PID {
	return a.actorPID
}
