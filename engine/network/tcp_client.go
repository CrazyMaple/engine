package network

import (
	"net"
	"sync"
	"time"
)

// TCPClient TCP客户端
type TCPClient struct {
	Addr            string        // 服务器地址
	ConnNum         int           // 连接数
	ConnectInterval time.Duration // 重连间隔
	PendingWriteNum int           // 写缓冲大小
	AutoReconnect   bool          // 是否自动重连
	TLSCfg          *TLSConfig    // TLS 配置（为 nil 则使用明文 TCP）
	NewAgent        func(*TCPConn) Agent

	sync.Mutex
	conns       ConnSet
	wg          sync.WaitGroup
	closeFlag   bool

	// 消息解析器配置
	LenMsgLen    int
	MinMsgLen    uint32
	MaxMsgLen    uint32
	LittleEndian bool
	msgParser    *MsgParser
}

// Start 启动客户端
func (client *TCPClient) Start() {
	client.init()

	for i := 0; i < client.ConnNum; i++ {
		client.wg.Add(1)
		go client.connect()
	}
}

func (client *TCPClient) init() {
	client.Lock()
	defer client.Unlock()

	if client.ConnNum <= 0 {
		client.ConnNum = 1
	}
	if client.ConnectInterval <= 0 {
		client.ConnectInterval = 3 * time.Second
	}
	if client.PendingWriteNum <= 0 {
		client.PendingWriteNum = 100
	}
	if client.NewAgent == nil {
		panic("NewAgent must not be nil")
	}

	client.conns = make(ConnSet)
	client.closeFlag = false

	// 创建消息解析器
	msgParser := NewMsgParser()
	msgParser.SetMsgLen(client.LenMsgLen, client.MinMsgLen, client.MaxMsgLen)
	msgParser.SetByteOrder(client.LittleEndian)
	client.msgParser = msgParser
}

func (client *TCPClient) dial() net.Conn {
	for {
		var conn net.Conn
		var err error
		if client.TLSCfg != nil {
			conn, err = tlsDial(client.Addr, client.TLSCfg)
		} else {
			conn, err = net.Dial("tcp", client.Addr)
		}
		if err == nil || client.closeFlag {
			return conn
		}

		time.Sleep(client.ConnectInterval)
	}
}

func (client *TCPClient) connect() {
	defer client.wg.Done()

reconnect:
	conn := client.dial()
	if conn == nil {
		return
	}

	client.Lock()
	if client.closeFlag {
		client.Unlock()
		conn.Close()
		return
	}
	client.conns[conn] = struct{}{}
	client.Unlock()

	tcpConn := newTCPConn(conn, client.PendingWriteNum, client.msgParser)
	agent := client.NewAgent(tcpConn)
	agent.Run()

	// 清理
	tcpConn.Close()
	client.Lock()
	delete(client.conns, conn)
	client.Unlock()
	agent.OnClose()

	// 自动重连
	if client.AutoReconnect {
		time.Sleep(client.ConnectInterval)
		goto reconnect
	}
}

// Close 关闭客户端
func (client *TCPClient) Close() {
	client.Lock()
	client.closeFlag = true
	for conn := range client.conns {
		conn.Close()
	}
	client.conns = nil
	client.Unlock()

	client.wg.Wait()
}
