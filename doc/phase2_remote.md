# Phase 2: 轻量级分布式通信

## 概述

Phase 2 实现了多节点部署和跨节点消息通信功能，使Actor可以在不同节点之间透明地发送消息。

## 核心功能

### 1. Remote模块
- 基于TCP的节点间通信
- 自定义消息序列化（JSON）
- 连接池管理和自动重连
- 位置透明的消息路由

### 2. 主要组件

#### Remote
远程通信管理器，负责：
- 启动TCP服务器监听远程连接
- 管理EndpointManager
- 路由消息到本地或远程Actor

#### EndpointManager
端点管理器，负责：
- 管理到远程节点的连接
- 自动创建和维护Endpoint
- 连接池管理

#### Endpoint
远程端点，负责：
- 维护到单个远程节点的TCP连接
- 自动重连
- 消息发送队列

#### RemoteProcess
远程进程代理，实现Process接口：
- 将消息转发到远程节点
- 支持用户消息和系统消息

### 3. PID扩展
PID现在支持远程寻址：
- `Address`字段：空表示本地，非空表示远程节点地址
- `IsLocal()`方法：判断是否为本地Actor
- 位置透明：发送消息时自动路由到本地或远程

## 使用示例

### 启动服务器节点

```go
// 创建Actor系统
system := actor.NewActorSystem()
system.Address = "127.0.0.1:8001"

// 启动远程通信
r := remote.NewRemote(system, system.Address)
r.Start()

// 设置远程进程代理
remoteProc := remote.NewRemoteProcess(r)
system.ProcessRegistry.SetRemoteProcess(remoteProc)

// 创建Actor
props := actor.PropsFromProducer(func() actor.Actor {
    return &MyActor{}
})
pid := system.Root.SpawnNamed(props, "myactor")
```

### 启动客户端节点并发送消息

```go
// 创建Actor系统
system := actor.NewActorSystem()
system.Address = "127.0.0.1:8002"

// 启动远程通信
r := remote.NewRemote(system, system.Address)
r.Start()

// 设置远程进程代理
remoteProc := remote.NewRemoteProcess(r)
system.ProcessRegistry.SetRemoteProcess(remoteProc)

// 创建远程Actor的PID
remotePID := actor.NewPID("127.0.0.1:8001", "myactor")

// 发送消息（位置透明）
system.Root.Send(remotePID, &MyMessage{})
```

## 运行示例

示例程序演示了两个节点之间的Ping-Pong通信：

### 启动服务器
```bash
go run example/remote_example.go server
```

### 启动客户端
```bash
go run example/remote_example.go client
```

客户端会向服务器发送10次Ping消息，服务器回复Pong消息。

## 架构特点

1. **轻量级**：不依赖gRPC，使用自研TCP协议
2. **自动重连**：连接断开后自动重连
3. **位置透明**：代码不区分本地/远程Actor
4. **消息队列**：每个Endpoint有独立的发送队列
5. **并发安全**：使用读写锁保护共享状态

## 消息格式

```go
type RemoteMessage struct {
    Target  *actor.PID  // 目标Actor
    Sender  *actor.PID  // 发送者
    Message interface{} // 消息内容
    Type    MessageType // 消息类型（用户/系统）
}
```

消息使用JSON序列化，格式：
```
| 4字节长度 | JSON数据 |
```

## 下一步（Phase 3）

- 集群管理（Gossip协议）
- 虚拟Actor（Grain）
- 路由器（Broadcast/ConsistentHash）
- PubSub发布订阅
