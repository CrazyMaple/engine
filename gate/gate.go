package gate

import (
	"engine/actor"
	"engine/network"
	"net"
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

	// WebSocket配置（预留）
	WSAddr   string
	CertFile string
	KeyFile  string

	tcpServer *network.TCPServer
	system    *actor.ActorSystem
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
}

// Close 关闭网关
func (g *Gate) Close() {
	if g.tcpServer != nil {
		g.tcpServer.Close()
	}
}

func (g *Gate) newAgent(conn *network.TCPConn) network.Agent {
	agent := &Agent{
		conn:      conn,
		gate:      g,
		system:    g.system,
		closeChan: make(chan struct{}),
	}
	return agent
}

// Agent 玩家代理
type Agent struct {
	conn      network.Conn
	gate      *Gate
	system    *actor.ActorSystem
	actorPID  *actor.PID
	userData  interface{}
	closeChan chan struct{}
}

// Run 运行代理
func (a *Agent) Run() {
	for {
		data, err := a.conn.ReadMsg()
		if err != nil {
			break
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

// OnClose 连接关闭回调
func (a *Agent) OnClose() {
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
