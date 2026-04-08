package remote

import (
	"sync"

	"engine/actor"
	engerr "engine/errors"
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
	// Codec 可选的编解码器，nil 使用 JSON 兜底
	Codec         *RemoteCodec
	// HealthCheck 可选的连接健康检查配置
	HealthCheck   HealthCheckConfig
	// RetryQueue 可选的消息重发队列配置
	RetryQueue    RetryQueueConfig
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

	// 初始化默认 Codec
	if r.Codec == nil {
		r.Codec = DefaultRemoteCodec()
	}

	// 传递签名器、TLS 配置和 Codec 到端点管理器
	r.endpointMgr.signer = r.Signer
	r.endpointMgr.tlsCfg = r.TLSCfg
	r.endpointMgr.codec = r.Codec

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

	// 自动注册远程进程代理，使远程 PID 可通过 ProcessRegistry 路由
	r.system.ProcessRegistry.SetRemoteProcess(NewRemoteProcess(r))

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

	// 确保 sender PID 携带本地地址，使远端可以路由响应回来
	if sender != nil && sender.Address == "" {
		sender = &actor.PID{
			Address: r.address,
			Id:      sender.Id,
		}
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
				log.Error("%v", &engerr.AuthError{Reason: "message too short for signature"})
				continue
			}
			payload := data[:len(data)-hmacSize]
			sig := data[len(data)-hmacSize:]
			if !a.remote.Signer.Verify(payload, sig) {
				log.Error("%v", &engerr.AuthError{Reason: "HMAC signature mismatch"})
				continue
			}
			data = payload
		}

		// 使用 Codec 反序列化远程消息（自动区分批量/单条）
		isBatch, batchMsg, singleMsg, unmarshalErr := a.remote.Codec.UnmarshalEnvelope(data)
		if unmarshalErr != nil {
			log.Error("Unmarshal message error: %v", unmarshalErr)
			continue
		}

		if isBatch {
			for _, msg := range batchMsg.Messages {
				a.resolveAndRoute(msg)
			}
		} else {
			a.resolveAndRoute(singleMsg)
		}
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
			rawBytes, _ := a.remote.Codec.MarshalPayload(rawMsg)
			if typed, err := a.remote.Codec.UnmarshalPayload(msg.TypeName, rawBytes, defaultTypeRegistry); err == nil {
				msg.Message = typed
			}
		}
	}
	a.routeMessage(msg)
}

func (a *remoteAgent) routeMessage(msg *RemoteMessage) {
	// 远程消息的 Target PID 可能携带远程地址，需要转为本地 PID 查找
	localTarget := &actor.PID{Id: msg.Target.Id}
	process, ok := a.remote.system.ProcessRegistry.Get(localTarget)
	if !ok {
		log.Error("Target actor not found: %s", msg.Target.Id)
		return
	}

	switch msg.Type {
	case MessageTypeUser:
		// 使用信封携带 sender 信息，使目标 Actor 可以通过 ctx.Respond() 回复
		process.SendUserMessage(localTarget, actor.WrapEnvelope(msg.Message, msg.Sender))
	case MessageTypeSystem:
		process.SendSystemMessage(localTarget, msg.Message)
	}
}
