package federation

import (
	"time"

	"engine/actor"
	"engine/log"
	"engine/remote"
)

// Gateway 跨集群消息网关
type Gateway struct {
	config     *FederationConfig
	registry   *ClusterRegistry
	remote     *remote.Remote
	system     *actor.ActorSystem
	gatewayPID *actor.PID
	stopChan   chan struct{}
	started    bool
}

// NewGateway 创建跨集群网关
func NewGateway(system *actor.ActorSystem, r *remote.Remote, cfg *FederationConfig) *Gateway {
	cfg.defaults()
	return &Gateway{
		config:   cfg,
		registry: NewClusterRegistry(),
		remote:   r,
		system:   system,
		stopChan: make(chan struct{}),
	}
}

// Start 启动网关
func (g *Gateway) Start() error {
	if g.started {
		return nil
	}

	// 注册联邦消息类型到远程类型注册表
	remote.RegisterType(&FederatedMessage{})
	remote.RegisterType(&FederatedPing{})
	remote.RegisterType(&FederatedPong{})
	remote.RegisterType(&FederatedRegister{})

	// 创建网关 Actor
	props := actor.PropsFromProducer(func() actor.Actor {
		return &gatewayActor{gateway: g}
	})
	g.gatewayPID = g.system.Root.SpawnNamed(props, "federation/gateway")

	// 注册联邦进程到 ProcessRegistry
	router := NewFederatedRouter(g)
	g.system.ProcessRegistry.SetFederatedProcess(router)

	// 向所有已知对端发送注册消息
	for clusterID, addr := range g.config.PeerClusters {
		g.registry.Register(clusterID, addr, nil)
		g.sendRegistration(addr)
	}

	// 启动心跳循环
	go g.heartbeatLoop()

	g.started = true
	log.Info("Federation gateway started: cluster=%s, address=%s, peers=%d",
		g.config.LocalClusterID, g.config.GatewayAddress, len(g.config.PeerClusters))
	return nil
}

// Stop 停止网关
func (g *Gateway) Stop() {
	if !g.started {
		return
	}
	close(g.stopChan)
	if g.gatewayPID != nil {
		g.system.Root.Stop(g.gatewayPID)
	}
	g.started = false
	log.Info("Federation gateway stopped")
}

// Registry 返回集群注册表
func (g *Gateway) Registry() *ClusterRegistry {
	return g.registry
}

// Send 向联邦 PID 发送消息
func (g *Gateway) Send(target *actor.PID, sender *actor.PID, message interface{}) {
	clusterID, actorPath, err := ParseFederatedPID(target.Address)
	if err != nil {
		log.Error("Federation: invalid target: %v", err)
		return
	}

	entry, ok := g.registry.Lookup(clusterID)
	if !ok {
		log.Error("Federation: unknown cluster %s", clusterID)
		return
	}

	// 查找消息类型名
	typeName, _ := remote.DefaultTypeRegistry().GetTypeName(message)

	fedMsg := &FederatedMessage{
		SourceCluster: g.config.LocalClusterID,
		TargetCluster: clusterID,
		TargetActor:   actorPath,
		Sender:        sender,
		Payload:       message,
		TypeName:      typeName,
	}

	// 通过 remote 发送到对端网关 actor
	gatewayPID := actor.NewPID(entry.GatewayAddress, "federation/gateway")
	g.remote.Send(gatewayPID, g.gatewayPID, fedMsg, 0)
}

func (g *Gateway) sendRegistration(addr string) {
	target := actor.NewPID(addr, "federation/gateway")
	regMsg := &FederatedRegister{
		ClusterID:      g.config.LocalClusterID,
		GatewayAddress: g.config.GatewayAddress,
	}
	g.remote.Send(target, g.gatewayPID, regMsg, 0)
}

func (g *Gateway) heartbeatLoop() {
	ticker := time.NewTicker(g.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-g.stopChan:
			return
		case <-ticker.C:
			for _, entry := range g.registry.All() {
				target := actor.NewPID(entry.GatewayAddress, "federation/gateway")
				ping := &FederatedPing{
					ClusterID: g.config.LocalClusterID,
					Timestamp: time.Now().UnixMilli(),
				}
				g.remote.Send(target, g.gatewayPID, ping, 0)
			}
		}
	}
}

// gatewayActor 处理跨集群消息的 Actor
type gatewayActor struct {
	gateway *Gateway
}

func (a *gatewayActor) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		log.Debug("Federation gateway actor started")

	case *FederatedMessage:
		a.handleFederatedMessage(msg)

	case *FederatedPing:
		// 回复 Pong
		pong := &FederatedPong{
			ClusterID: a.gateway.config.LocalClusterID,
			Timestamp: msg.Timestamp,
		}
		if ctx.Sender() != nil {
			ctx.Respond(pong)
		}
		a.gateway.registry.UpdateStatus(msg.ClusterID, "alive")

	case *FederatedPong:
		a.gateway.registry.UpdateStatus(msg.ClusterID, "alive")

	case *FederatedRegister:
		a.gateway.registry.Register(msg.ClusterID, msg.GatewayAddress, msg.Kinds)
		log.Info("Federation: registered cluster %s at %s", msg.ClusterID, msg.GatewayAddress)
	}
}

func (a *gatewayActor) handleFederatedMessage(msg *FederatedMessage) {
	// 在本地集群中查找目标 Actor
	localTarget := &actor.PID{Id: msg.TargetActor}
	proc, ok := a.gateway.system.ProcessRegistry.Get(localTarget)
	if !ok {
		log.Error("Federation: target actor not found locally: %s", msg.TargetActor)
		return
	}

	// 投递消息
	envelope := actor.WrapEnvelope(msg.Payload, msg.Sender)
	proc.SendUserMessage(localTarget, envelope)
}
