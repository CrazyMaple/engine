//go:build ignore

package main

import (
	"fmt"
	"os"
	"time"

	"engine/actor"
	"engine/log"
	"engine/remote"
)

// PingMessage Ping消息
type PingMessage struct {
	Count int
}

// PongMessage Pong消息
type PongMessage struct {
	Count int
}

// PingActor Ping Actor
type PingActor struct {
	pongPID *actor.PID
	count   int
}

func (a *PingActor) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		log.Info("PingActor started")
		// 发送第一个ping
		ctx.Send(a.pongPID, &PingMessage{Count: a.count})
		a.count++

	case *PongMessage:
		log.Info("Received Pong: %d", msg.Count)
		if a.count < 10 {
			time.Sleep(1 * time.Second)
			ctx.Send(a.pongPID, &PingMessage{Count: a.count})
			a.count++
		} else {
			log.Info("Ping completed")
			ctx.StopActor(ctx.Self())
		}
	}
}

// PongActor Pong Actor
type PongActor struct{}

func (a *PongActor) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		log.Info("PongActor started")

	case *PingMessage:
		log.Info("Received Ping: %d", msg.Count)
		// 回复pong
		if ctx.Sender() != nil {
			ctx.Send(ctx.Sender(), &PongMessage{Count: msg.Count})
		}
	}
}

func main() {
	// 从命令行参数获取模式
	if len(os.Args) < 2 {
		fmt.Println("Usage: remote_example <server|client>")
		return
	}

	mode := os.Args[1]

	if mode == "server" {
		runServer()
	} else if mode == "client" {
		runClient()
	} else {
		fmt.Println("Invalid mode. Use 'server' or 'client'")
	}
}

func runServer() {
	log.Info("Starting server node...")

	// 创建Actor系统
	system := actor.NewActorSystem()
	system.Address = "127.0.0.1:8001"

	// 启动远程通信
	r := remote.NewRemote(system, system.Address)
	r.Start()

	// 设置远程进程代理
	remoteProc := remote.NewRemoteProcess(r)
	system.ProcessRegistry.SetRemoteProcess(remoteProc)

	// 创建PongActor
	props := actor.PropsFromProducer(func() actor.Actor {
		return &PongActor{}
	})
	pongPID := system.Root.SpawnNamed(props, "pong")
	log.Info("PongActor spawned: %s", pongPID.String())

	// 保持运行
	select {}
}

func runClient() {
	log.Info("Starting client node...")

	// 创建Actor系统
	system := actor.NewActorSystem()
	system.Address = "127.0.0.1:8002"

	// 启动远程通信
	r := remote.NewRemote(system, system.Address)
	r.Start()

	// 设置远程进程代理
	remoteProc := remote.NewRemoteProcess(r)
	system.ProcessRegistry.SetRemoteProcess(remoteProc)

	// 等待服务器启动
	time.Sleep(2 * time.Second)

	// 创建远程PongActor的PID
	pongPID := actor.NewPID("127.0.0.1:8001", "pong")

	// 创建PingActor
	props := actor.PropsFromProducer(func() actor.Actor {
		return &PingActor{
			pongPID: pongPID,
			count:   0,
		}
	})
	system.Root.Spawn(props)

	// 保持运行
	time.Sleep(30 * time.Second)
	log.Info("Client shutting down")
}
