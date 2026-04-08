package remote

import (
	"sync"
	"time"

	"engine/actor"
	"engine/log"
	"engine/network"
)

const (
	maxBatchSize   = 64              // 每批最多消息数
	batchFlushTime = time.Millisecond // 批量刷新间隔
)

// Endpoint 远程端点
type Endpoint struct {
	address       string
	conn          *network.TCPConn
	client        *network.TCPClient
	sendChan      chan *RemoteMessage
	stopChan      chan struct{}
	connected     bool
	mu            sync.RWMutex
	signer        *MessageSigner     // 可选的消息签名器
	tlsCfg        *network.TLSConfig // 可选的 TLS 配置
	codec         *RemoteCodec       // 编解码器
	healthChk     *healthChecker     // 可选的健康检查器
	retryQ        *retryQueue        // 可选的消息重发队列
}

// NewEndpoint 创建远程端点
func NewEndpoint(address string) *Endpoint {
	return &Endpoint{
		address:  address,
		sendChan: make(chan *RemoteMessage, 1000),
		stopChan: make(chan struct{}),
	}
}

// Start 启动端点
func (ep *Endpoint) Start() {
	// 创建TCP客户端（在 Start 时创建以确保 signer/tlsCfg 已设置）
	ep.client = &network.TCPClient{
		Addr:            ep.address,
		ConnNum:         1,
		ConnectInterval: 3 * time.Second,
		PendingWriteNum: 100,
		AutoReconnect:   true,
		LenMsgLen:       4,
		MaxMsgLen:       1024 * 1024,
		TLSCfg:          ep.tlsCfg,
		NewAgent: func(conn *network.TCPConn) network.Agent {
			return &endpointAgent{
				endpoint: ep,
				conn:     conn,
			}
		},
	}
	ep.client.Start()
	go ep.sendLoop()
}

// Stop 停止端点
func (ep *Endpoint) Stop() {
	close(ep.stopChan)
	if ep.healthChk != nil {
		ep.healthChk.Stop()
	}
	if ep.client != nil {
		ep.client.Close()
	}
}

// Send 发送消息
func (ep *Endpoint) Send(msg *RemoteMessage) {
	select {
	case ep.sendChan <- msg:
	default:
		log.Error("Endpoint send channel full, message dropped")
	}
}

// sendLoop 批量发送循环，积累消息后一次性写入以减少 syscall
func (ep *Endpoint) sendLoop() {
	batch := make([]*RemoteMessage, 0, maxBatchSize)
	ticker := time.NewTicker(batchFlushTime)
	defer ticker.Stop()

	// 重发队列定时 drain（每秒一次）
	var retryTicker *time.Ticker
	if ep.retryQ != nil {
		retryTicker = time.NewTicker(time.Second)
		defer retryTicker.Stop()
	}

	for {
		select {
		case msg := <-ep.sendChan:
			batch = append(batch, msg)
			// 尝试非阻塞地继续读取更多消息
			for len(batch) < maxBatchSize {
				select {
				case m := <-ep.sendChan:
					batch = append(batch, m)
				default:
					goto flush
				}
			}
		flush:
			ep.sendBatch(batch)
			batch = batch[:0]
		case <-ticker.C:
			if len(batch) > 0 {
				ep.sendBatch(batch)
				batch = batch[:0]
			}
		case <-ep.stopChan:
			// 发送剩余消息
			if len(batch) > 0 {
				ep.sendBatch(batch)
			}
			return
		}

		// 非阻塞检查 retry queue
		if retryTicker != nil {
			select {
			case <-retryTicker.C:
				ep.retryQ.Drain(func(msg *RemoteMessage) error {
					ep.sendMessage(msg)
					return nil
				})
			default:
			}
		}
	}
}

// sendBatch 批量发送消息
func (ep *Endpoint) sendBatch(batch []*RemoteMessage) {
	if len(batch) == 0 {
		return
	}

	ep.mu.RLock()
	conn := ep.conn
	connected := ep.connected
	ep.mu.RUnlock()

	if !connected || conn == nil {
		if ep.retryQ != nil {
			for _, msg := range batch {
				ep.retryQ.Add(msg)
			}
			log.Debug("Endpoint not connected, %d messages queued for retry", len(batch))
		} else {
			log.Debug("Endpoint not connected, %d messages dropped", len(batch))
		}
		return
	}

	// 单条消息直接发送，避免批量包装开销
	if len(batch) == 1 {
		ep.sendMessage(batch[0])
		return
	}

	// 多条消息使用批量包装
	batchMsg := &RemoteMessageBatch{Messages: batch}
	buf := actor.AcquireBuffer()
	defer actor.ReleaseBuffer(buf)

	data, err := ep.codec.MarshalEnvelope(batchMsg)
	if err != nil {
		log.Error("Marshal batch error: %v", err)
		return
	}

	// 如果启用签名，追加 HMAC 签名
	if ep.signer != nil {
		sig := ep.signer.Sign(data)
		data = append(data, sig...)
	}

	if err := conn.WriteMsg(data); err != nil {
		log.Error("Write batch error: %v", err)
	}
}

func (ep *Endpoint) sendMessage(msg *RemoteMessage) {
	ep.mu.RLock()
	conn := ep.conn
	connected := ep.connected
	ep.mu.RUnlock()

	if !connected || conn == nil {
		log.Debug("Endpoint not connected, message queued")
		return
	}

	// 序列化消息
	data, err := ep.codec.MarshalEnvelope(msg)
	if err != nil {
		log.Error("Marshal message error: %v", err)
		return
	}

	// 如果启用签名，追加 HMAC 签名（32 字节 SHA256）
	if ep.signer != nil {
		sig := ep.signer.Sign(data)
		data = append(data, sig...)
	}

	// 发送消息
	if err := conn.WriteMsg(data); err != nil {
		log.Error("Write message error: %v", err)
	}
}

func (ep *Endpoint) setConn(conn *network.TCPConn) {
	ep.mu.Lock()
	ep.conn = conn
	ep.connected = true
	ep.mu.Unlock()
}

func (ep *Endpoint) clearConn() {
	ep.mu.Lock()
	ep.conn = nil
	ep.connected = false
	ep.mu.Unlock()
}

// endpointAgent 端点代理
type endpointAgent struct {
	endpoint *Endpoint
	conn     *network.TCPConn
}

func (a *endpointAgent) Run() {
	a.endpoint.setConn(a.conn)
	log.Info("Connected to remote endpoint: %s", a.endpoint.address)

	// 启动健康检查器（如已配置）
	if a.endpoint.healthChk != nil {
		a.endpoint.healthChk.Start()
	}

	// 保持连接，接收消息
	for {
		data, err := a.conn.ReadMsg()
		if err != nil {
			break
		}
		// 处理 Pong 响应
		if a.endpoint.healthChk != nil && a.endpoint.codec != nil {
			_, _, single, uerr := a.endpoint.codec.UnmarshalEnvelope(data)
			if uerr == nil && single != nil {
				if _, ok := single.Message.(*PongMessage); ok {
					a.endpoint.healthChk.OnPong()
					continue
				}
			}
		}
	}
}

func (a *endpointAgent) OnClose() {
	a.endpoint.clearConn()
	log.Info("Disconnected from remote endpoint: %s", a.endpoint.address)
}

// EndpointManager 端点管理器
type EndpointManager struct {
	system    *actor.ActorSystem
	endpoints map[string]*Endpoint
	mu        sync.RWMutex
	signer    *MessageSigner     // 可选的消息签名器
	tlsCfg    *network.TLSConfig // 可选的 TLS 配置
	codec     *RemoteCodec       // 编解码器
}

// NewEndpointManager 创建端点管理器
func NewEndpointManager(system *actor.ActorSystem) *EndpointManager {
	return &EndpointManager{
		system:    system,
		endpoints: make(map[string]*Endpoint),
	}
}

// GetEndpoint 获取或创建端点
func (em *EndpointManager) GetEndpoint(address string) *Endpoint {
	em.mu.RLock()
	ep, ok := em.endpoints[address]
	em.mu.RUnlock()

	if ok {
		return ep
	}

	// 创建新端点
	em.mu.Lock()
	defer em.mu.Unlock()

	// 双重检查
	if ep, ok := em.endpoints[address]; ok {
		return ep
	}

	ep = NewEndpoint(address)
	ep.signer = em.signer
	ep.tlsCfg = em.tlsCfg
	ep.codec = em.codec
	ep.Start()
	em.endpoints[address] = ep

	log.Info("Created endpoint for: %s", address)
	return ep
}

// ConnectionCount 返回当前远程连接数
func (em *EndpointManager) ConnectionCount() int {
	em.mu.RLock()
	n := len(em.endpoints)
	em.mu.RUnlock()
	return n
}

// Stop 停止所有端点
func (em *EndpointManager) Stop() {
	em.mu.Lock()
	defer em.mu.Unlock()

	for _, ep := range em.endpoints {
		ep.Stop()
	}
	em.endpoints = make(map[string]*Endpoint)
}
