package main

import (
	"engine/actor"
	"engine/codec"
	"engine/console"
	"engine/gate"
	"engine/log"
	"os"
	"os/signal"
	"syscall"
)

// 消息定义
type LoginRequest struct {
	Type     string `json:"type"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Type    string `json:"type"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// PlayerActor 玩家Actor
type PlayerActor struct {
	username string
	agent    *gate.Agent
}

func (p *PlayerActor) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		log.Info("PlayerActor started: %s", p.username)

	case *LoginRequest:
		log.Info("Player login: %s", msg.Username)
		p.username = msg.Username

		response := &LoginResponse{
			Type:    "LoginResponse",
			Success: true,
			Message: "登录成功",
		}
		p.agent.WriteMsg(response)

	case *actor.Stopping:
		log.Info("PlayerActor stopping: %s", p.username)

	case *actor.Stopped:
		log.Info("PlayerActor stopped: %s", p.username)
	}
}

func main() {
	// 创建Actor系统
	system := actor.NewActorSystem()

	// 创建消息处理器
	jsonCodec := codec.NewJSONCodec()
	jsonCodec.Register(&LoginRequest{})
	jsonCodec.Register(&LoginResponse{})

	processor := codec.NewSimpleProcessor(jsonCodec)
	processor.Register(&LoginRequest{}, func(msg *LoginRequest, agent *gate.Agent) {
		handleLogin(system, msg, agent)
	})

	// 创建网关
	g := gate.NewGate(system)
	g.TCPAddr = ":8888"
	g.Processor = processor
	g.Start()

	// 创建控制台
	c := console.NewConsole(9999)
	c.Start()

	log.Info("游戏服务器启动成功")
	log.Info("TCP端口: 8888")
	log.Info("控制台端口: 9999")

	// 等待退出信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Info("服务器关闭中...")
	g.Close()
	c.Close()
}

func handleLogin(system *actor.ActorSystem, msg *LoginRequest, agent *gate.Agent) {
	// 为每个玩家创建Actor
	props := actor.PropsFromProducer(func() actor.Actor {
		return &PlayerActor{
			agent: agent,
		}
	})

	pid := system.Root.Spawn(props)
	agent.BindActor(pid)

	// 发送登录消息到PlayerActor
	system.Root.Send(pid, msg)
}

