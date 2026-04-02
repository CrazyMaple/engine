package network

import (
	"crypto/tls"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"engine/log"
)

// WSServer WebSocket 服务器
type WSServer struct {
	Addr            string
	MaxConnNum      int
	PendingWriteNum int
	MaxMsgLen       uint32
	HTTPTimeout     time.Duration
	CertFile        string
	KeyFile         string
	PingInterval    time.Duration // Ping 发送间隔（0 表示禁用心跳）
	PongTimeout     time.Duration // Pong 等待超时（默认 PingInterval * 3/2）
	NewAgent        func(*WSConn) Agent
	ln              net.Listener
	handler         *WSHandler
}

// WSHandler WebSocket HTTP 处理器
type WSHandler struct {
	maxConnNum      int
	pendingWriteNum int
	maxMsgLen       uint32
	pingInterval    time.Duration
	pongTimeout     time.Duration
	newAgent        func(*WSConn) Agent
	upgrader        websocket.Upgrader
	conns           WebsocketConnSet
	mutexConns      sync.Mutex
	wg              sync.WaitGroup
}

func (handler *WSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	conn, err := handler.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Debug("ws upgrade error: %v", err)
		return
	}
	conn.SetReadLimit(int64(handler.maxMsgLen))

	handler.wg.Add(1)
	defer handler.wg.Done()

	handler.mutexConns.Lock()
	if handler.conns == nil {
		handler.mutexConns.Unlock()
		conn.Close()
		return
	}
	if len(handler.conns) >= handler.maxConnNum {
		handler.mutexConns.Unlock()
		conn.Close()
		log.Debug("too many ws connections")
		return
	}
	handler.conns[conn] = struct{}{}
	handler.mutexConns.Unlock()

	wsConn := newWSConn(conn, handler.pendingWriteNum, handler.maxMsgLen)

	// 配置 Ping/Pong 心跳保活
	if handler.pingInterval > 0 {
		pongTimeout := handler.pongTimeout
		if pongTimeout <= 0 {
			pongTimeout = handler.pingInterval * 3 / 2
		}
		conn.SetReadDeadline(time.Now().Add(pongTimeout))
		conn.SetPongHandler(func(string) error {
			conn.SetReadDeadline(time.Now().Add(pongTimeout))
			return nil
		})
		wsConn.startPing(handler.pingInterval)
	}

	agent := handler.newAgent(wsConn)
	agent.Run()

	// cleanup
	wsConn.Close()
	handler.mutexConns.Lock()
	delete(handler.conns, conn)
	handler.mutexConns.Unlock()
	agent.OnClose()
}

// Start 启动 WebSocket 服务器
func (server *WSServer) Start() {
	ln, err := net.Listen("tcp", server.Addr)
	if err != nil {
		panic(err)
	}

	if server.MaxConnNum <= 0 {
		server.MaxConnNum = 100
	}
	if server.PendingWriteNum <= 0 {
		server.PendingWriteNum = 100
	}
	if server.MaxMsgLen <= 0 {
		server.MaxMsgLen = 4096
	}
	if server.HTTPTimeout <= 0 {
		server.HTTPTimeout = 10 * time.Second
	}
	if server.NewAgent == nil {
		panic("NewAgent must not be nil")
	}

	if server.CertFile != "" || server.KeyFile != "" {
		tlsConfig := &tls.Config{}
		tlsConfig.NextProtos = []string{"http/1.1"}

		tlsConfig.Certificates = make([]tls.Certificate, 1)
		tlsConfig.Certificates[0], err = tls.LoadX509KeyPair(server.CertFile, server.KeyFile)
		if err != nil {
			panic(err)
		}

		ln = tls.NewListener(ln, tlsConfig)
	}

	server.ln = ln
	server.handler = &WSHandler{
		maxConnNum:      server.MaxConnNum,
		pendingWriteNum: server.PendingWriteNum,
		maxMsgLen:       server.MaxMsgLen,
		pingInterval:    server.PingInterval,
		pongTimeout:     server.PongTimeout,
		newAgent:        server.NewAgent,
		conns:           make(WebsocketConnSet),
		upgrader: websocket.Upgrader{
			HandshakeTimeout: server.HTTPTimeout,
			CheckOrigin:      func(_ *http.Request) bool { return true },
		},
	}

	httpServer := &http.Server{
		Addr:           server.Addr,
		Handler:        server.handler,
		ReadTimeout:    server.HTTPTimeout,
		WriteTimeout:   server.HTTPTimeout,
		MaxHeaderBytes: 1024,
	}

	go httpServer.Serve(ln)
	log.Info("WSServer listening on %s", server.Addr)
}

// Close 关闭 WebSocket 服务器
func (server *WSServer) Close() {
	server.ln.Close()

	server.handler.mutexConns.Lock()
	for conn := range server.handler.conns {
		conn.Close()
	}
	server.handler.conns = nil
	server.handler.mutexConns.Unlock()

	server.handler.wg.Wait()
}
