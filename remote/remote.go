package remote

import (
	"encoding/json"
	"sync"

	"engine/actor"
	"engine/log"
	"engine/network"
)

// Remote 远程通信管理器
type Remote struct {
	system        *actor.ActorSystem
	address       string // 本地地址 "host:port"
	endpointMgr   *EndpointManager
	server        *network.TCPServer
	started       bool
	mu            sync.RWMutex
	// Signer 可选的消息签名器，启用后远程消息将进行 HMAC 签名/验签
	Signer        *MessageSigner
	// TLSCfg 可选的 TLS 配置，启用后远程通信使用 TLS 加密
	TLSCfg        *network.TLSConfig
}

// NewRemote 创建远程通信管理器
func NewRemote(system *actor.ActorSystem, address string) *Remote {
	return &Remote{
		system:      system,
		address:     address,
		endpointMgr: NewEndpointManager(system),
	}
}

// Start 启动远程通信
func (r *Remote) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return
	}

	// 传递签名器和 TLS 配置到端点管理器
	r.endpointMgr.signer = r.Signer
	r.endpointMgr.tlsCfg = r.TLSCfg

	// 启动TCP服务器监听远程连接
	r.server = &network.TCPServer{
		Addr:            r.address,
		MaxConnNum:      1000,
		PendingWriteNum: 100,
		LenMsgLen:       4,
		MaxMsgLen:       1024 * 1024, // 1MB
		TLSCfg:          r.TLSCfg,
		NewAgent: func(conn *network.TCPConn) network.Agent {
			return &remoteAgent{
				conn:   conn,
				remote: r,
			}
		},
	}
	r.server.Start()

	r.started = true
	log.Info("Remote started on %s", r.address)
}

// Stop 停止远程通信
func (r *Remote) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.started {
		return
	}

	if r.server != nil {
		r.server.Close()
	}
	r.endpointMgr.Stop()

	r.started = false
	log.Info("Remote stopped")
}

// Send 发送消息到远程Actor
func (r *Remote) Send(target *actor.PID, sender *actor.PID, message interface{}, msgType MessageType) {
	endpoint := r.endpointMgr.GetEndpoint(target.Address)
	if endpoint == nil {
		log.Error("Endpoint not found: %s", target.Address)
		return
	}

	// 查找类型名称用于远端反序列化
	typeName, _ := defaultTypeRegistry.GetTypeName(message)

	remoteMsg := &RemoteMessage{
		Target:   target,
		Sender:   sender,
		Message:  message,
		Type:     msgType,
		TypeName: typeName,
	}

	endpoint.Send(remoteMsg)
}

// GetAddress 获取本地地址
func (r *Remote) GetAddress() string {
	return r.address
}

const hmacSize = 32 // SHA256 HMAC 签名长度

// remoteAgent 处理远程连接的Agent
type remoteAgent struct {
	conn   *network.TCPConn
	remote *Remote
}

func (a *remoteAgent) Run() {
	for {
		data, err := a.conn.ReadMsg()
		if err != nil {
			log.Debug("Read message error: %v", err)
			break
		}

		// 如果启用签名验证，先验证并剥离签名
		if a.remote.Signer != nil {
			if len(data) < hmacSize {
				log.Error("Message too short for signature verification")
				continue
			}
			payload := data[:len(data)-hmacSize]
			sig := data[len(data)-hmacSize:]
			if !a.remote.Signer.Verify(payload, sig) {
				log.Error("Message signature verification failed")
				continue
			}
			data = payload
		}

		// 尝试解析为批量消息
		var batchMsg RemoteMessageBatch
		if err := json.Unmarshal(data, &batchMsg); err == nil && len(batchMsg.Messages) > 0 {
			for _, msg := range batchMsg.Messages {
				a.resolveAndRoute(msg)
			}
			continue
		}

		// 解析为单条消息
		var remoteMsg RemoteMessage
		if err := json.Unmarshal(data, &remoteMsg); err != nil {
			log.Error("Unmarshal message error: %v", err)
			continue
		}

		a.resolveAndRoute(&remoteMsg)
	}
}

func (a *remoteAgent) OnClose() {
	log.Debug("Remote connection closed")
}

// resolveAndRoute 解析消息类型并路由
func (a *remoteAgent) resolveAndRoute(msg *RemoteMessage) {
	// 如果有类型名称，尝试类型化反序列化
	if msg.TypeName != "" {
		if rawMsg, ok := msg.Message.(map[string]interface{}); ok {
			rawBytes, _ := json.Marshal(rawMsg)
			if typed, err := defaultTypeRegistry.Deserialize(msg.TypeName, rawBytes); err == nil {
				msg.Message = typed
			}
		}
	}
	a.routeMessage(msg)
}

func (a *remoteAgent) routeMessage(msg *RemoteMessage) {
	process, ok := a.remote.system.ProcessRegistry.Get(msg.Target)
	if !ok {
		log.Error("Target actor not found: %s", msg.Target.Id)
		return
	}

	switch msg.Type {
	case MessageTypeUser:
		process.SendUserMessage(msg.Target, msg.Message)
	case MessageTypeSystem:
		process.SendSystemMessage(msg.Target, msg.Message)
	}
}
